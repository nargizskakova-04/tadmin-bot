package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/robfig/cron/v3"

	"admin-bot/internal/domain"
	"admin-bot/internal/usecase"
)

// CronScheduler sends announcements at specific days/times using cron expressions.
type CronScheduler struct {
	cron      *cron.Cron
	raidUC    *usecase.RaidUseCase
	sender    domain.BotSender
	chatIDs   []int64
	sheetURLs map[domain.PiscineType]map[int]string
	logger    *slog.Logger

	// DefenseCallback is called when a defense reminder is sent.
	// It receives the chat ID, piscine type, rendered text, and schedule info
	// so the caller can attach inline keyboards, etc.
	DefenseCallback func(ctx context.Context, chatID int64, piscine domain.PiscineType, text string, schedule *usecase.DefenseSchedule)
}

// NewCronScheduler creates a scheduler with predefined cron jobs.
// timezone should be an IANA timezone name, e.g. "Asia/Almaty".
//
// sheetURLs maps each (piscine, week) to a pre-configured Google Sheets URL
// that will be embedded in the Sunday student message. Missing entries simply
// produce an empty SHEET_URL substitution in the template — the message is
// still sent.
func NewCronScheduler(
	raidUC *usecase.RaidUseCase,
	sender domain.BotSender,
	chatIDs []int64,
	timezone string,
	sheetURLs map[domain.PiscineType]map[int]string,
	logger *slog.Logger,
) *CronScheduler {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		logger.Error("invalid timezone, using UTC", "timezone", timezone, "err", err)
		loc = time.UTC
	}

	s := &CronScheduler{
		cron:      cron.New(cron.WithLocation(loc)),
		raidUC:    raidUC,
		sender:    sender,
		chatIDs:   chatIDs,
		sheetURLs: sheetURLs,
		logger:    logger,
	}
	s.registerJobs()
	return s
}

func (s *CronScheduler) registerJobs() {
	// Monday 10:00 — FAQ (week 1 only)
	s.mustAdd("0 10 * * 1", func() {
		s.broadcastMessage(domain.MsgFAQ, nil)
	})

	// Thursday 14:30 — Exam announcement (weeks 1-3)
	s.mustAdd("30 14 * * 4", func() {
		s.broadcastMessage(domain.MsgExamAnnouncement, nil)
	})

	// Thursday 14:00 — Final Exam (last week)
	s.mustAdd("0 14 * * 4", func() {
		s.broadcastMessage(domain.MsgFinalExam, nil)
	})

	// Friday 10:00 — Hackathon (week 3, Go & JS only)
	s.mustAdd("0 10 * * 5", func() {
		s.broadcastMessage(domain.MsgHackathon, nil)
	})

	// Sunday 15:00 — Defense reminder (admin) + Student message (with SHEET_URL).
	// Both go out from broadcastDefenseReminder so the per-piscine SHEET_URL
	// can be threaded into the student template.
	s.mustAdd("0 15 * * 0", func() {
		s.broadcastDefenseReminder()
	})
}

func (s *CronScheduler) mustAdd(spec string, cmd func()) {
	_, err := s.cron.AddFunc(spec, cmd)
	if err != nil {
		s.logger.Error("failed to add cron job", "spec", spec, "err", err)
	}
}

// Start begins the cron scheduler.
func (s *CronScheduler) Start() {
	s.cron.Start()
	s.logger.Info("cron scheduler started", "jobs", len(s.cron.Entries()))
}

// Stop gracefully stops the cron scheduler.
func (s *CronScheduler) Stop() {
	ctx := s.cron.Stop()
	<-ctx.Done()
	s.logger.Info("cron scheduler stopped")
}

// broadcastMessage sends a message of the given type for all active piscines
// to all configured chat IDs.
func (s *CronScheduler) broadcastMessage(msgType domain.MessageType, extra map[string]string) {
	ctx := context.Background()

	for _, piscine := range domain.AllPiscines() {
		text, err := s.raidUC.BuildMessage(ctx, piscine, msgType, extra)
		if err != nil {
			// Not an error if the message type is simply not applicable this week.
			s.logger.Debug("skip message",
				"piscine", piscine,
				"type", msgType,
				"reason", err,
			)
			continue
		}
		s.sendToAll(ctx, piscine, msgType, text)
	}
}

// sendToAll fans a single text out to every configured chat, logging per send.
func (s *CronScheduler) sendToAll(ctx context.Context, piscine domain.PiscineType, msgType domain.MessageType, text string) {
	for _, chatID := range s.chatIDs {
		if err := s.sender.SendMessage(ctx, chatID, text); err != nil {
			s.logger.Error("send failed",
				"piscine", piscine,
				"type", msgType,
				"chat_id", chatID,
				"err", err,
			)
			continue
		}
		s.logger.Info("message sent",
			"piscine", piscine,
			"type", msgType,
			"chat_id", chatID,
		)
	}
}

// sheetURLFor returns the configured spreadsheet URL for (piscine, week) or ""
// if none is set. An empty URL is intentional — the student template tolerates
// an empty substitution and the message still goes out.
func (s *CronScheduler) sheetURLFor(piscine domain.PiscineType, week int) string {
	if m, ok := s.sheetURLs[piscine]; ok {
		return m[week]
	}
	return ""
}

// broadcastDefenseReminder sends defense table reminders to admins (with the
// "Update table" inline keyboard) and the matching student message (with the
// pre-configured SHEET_URL) to the same chats.
func (s *CronScheduler) broadcastDefenseReminder() {
	ctx := context.Background()

	for _, piscine := range domain.AllPiscines() {
		text, schedule, err := s.raidUC.BuildDefenseReminder(ctx, piscine)
		if err != nil {
			s.logger.Debug("skip defense reminder",
				"piscine", piscine,
				"reason", err,
			)
			continue
		}

		// Build the matching student message with SHEET_URL from env (may be empty).
		// We need the week number to look up the URL — DetectCurrentWeek is cheap
		// to call once more than to plumb through BuildDefenseReminder's signature.
		sheetURL := ""
		if weekInfo, err := s.raidUC.DetectCurrentWeek(ctx, piscine); err == nil && weekInfo != nil {
			sheetURL = s.sheetURLFor(piscine, weekInfo.WeekNumber)
			if sheetURL == "" {
				s.logger.Warn("no sheet URL configured for week",
					"piscine", piscine, "week", weekInfo.WeekNumber)
			}
		}

		studentText, studentErr := s.raidUC.BuildMessage(ctx, piscine, domain.MsgStudentMessage,
			map[string]string{"SHEET_URL": sheetURL})
		if studentErr != nil {
			s.logger.Debug("skip student message",
				"piscine", piscine,
				"reason", studentErr,
			)
		}

		for _, chatID := range s.chatIDs {
			// Admin reminder with inline keyboard (falls back to plain text).
			if s.DefenseCallback != nil {
				s.DefenseCallback(ctx, chatID, piscine, text, schedule)
			} else if err := s.sender.SendMessage(ctx, chatID, text); err != nil {
				s.logger.Error("send defense reminder failed",
					"chat_id", chatID,
					"err", err,
				)
			}

			// Student message into the same chats.
			if studentErr == nil {
				if err := s.sender.SendMessage(ctx, chatID, studentText); err != nil {
					s.logger.Error("send student message failed",
						"piscine", piscine,
						"chat_id", chatID,
						"err", err,
					)
				}
			}
		}
	}
}
