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
	cron    *cron.Cron
	raidUC  *usecase.RaidUseCase
	sender  domain.BotSender
	chatIDs []int64
	logger  *slog.Logger

	// DefenseCallback is called when a defense reminder is sent.
	// It receives the chat ID, piscine type, rendered text, and schedule info
	// so the caller can attach inline keyboards, etc.
	DefenseCallback func(ctx context.Context, chatID int64, piscine domain.PiscineType, text string, schedule *usecase.DefenseSchedule)
}

// NewCronScheduler creates a scheduler with predefined cron jobs.
// timezone should be an IANA timezone name, e.g. "Asia/Almaty".
func NewCronScheduler(
	raidUC *usecase.RaidUseCase,
	sender domain.BotSender,
	chatIDs []int64,
	timezone string,
	logger *slog.Logger,
) *CronScheduler {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		logger.Error("invalid timezone, using UTC", "timezone", timezone, "err", err)
		loc = time.UTC
	}

	s := &CronScheduler{
		cron:    cron.New(cron.WithLocation(loc)),
		raidUC:  raidUC,
		sender:  sender,
		chatIDs: chatIDs,
		logger:  logger,
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

	// Sunday 15:00 — Defense reminder (admin) + Student message
	s.mustAdd("0 15 * * 0", func() {
		s.broadcastDefenseReminder()
		s.broadcastMessage(domain.MsgStudentMessage, nil)
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

		for _, chatID := range s.chatIDs {
			if err := s.sender.SendMessage(ctx, chatID, text); err != nil {
				s.logger.Error("send failed",
					"piscine", piscine,
					"type", msgType,
					"chat_id", chatID,
					"err", err,
				)
			} else {
				s.logger.Info("message sent",
					"piscine", piscine,
					"type", msgType,
					"chat_id", chatID,
				)
			}
		}
	}
}

// broadcastDefenseReminder sends defense table reminders with schedule info.
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

		for _, chatID := range s.chatIDs {
			if s.DefenseCallback != nil {
				// Let the callback handle sending (with inline keyboard).
				s.DefenseCallback(ctx, chatID, piscine, text, schedule)
			} else {
				// Fallback: send as plain text.
				if err := s.sender.SendMessage(ctx, chatID, text); err != nil {
					s.logger.Error("send defense reminder failed",
						"chat_id", chatID,
						"err", err,
					)
				}
			}
		}
	}
}
