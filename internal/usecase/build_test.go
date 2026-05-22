package usecase

import (
	"context"
	"strings"
	"testing"
	"time"

	"admin-bot/internal/domain"
	"admin-bot/internal/usecase/strategy"
)

// --- fake TemplateRenderer ---

type fakeTemplates struct {
	// templates is keyed by template key. The string is rendered via simple
	// {{NAME}} substitution against vars passed to Render.
	templates map[string]string
	// rendered records the (key, vars) of each call.
	rendered []renderedCall
	// err lets a test force a render failure.
	err error
}

type renderedCall struct {
	key  string
	vars map[string]string
}

func (f *fakeTemplates) Render(key string, vars map[string]string) (string, error) {
	f.rendered = append(f.rendered, renderedCall{key: key, vars: vars})
	if f.err != nil {
		return "", f.err
	}
	tmpl, ok := f.templates[key]
	if !ok {
		return "", domain.ErrTemplateNotFound
	}
	// Substitute {{NAME}} placeholders with values from vars.
	out := tmpl
	for k, v := range vars {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out, nil
}

// helper to construct a fully wired RaidUseCase.
func newUCWithDeps(edu domain.OneEduClient, tmpls domain.TemplateRenderer, strats ...strategy.PiscineStrategy) *RaidUseCase {
	if len(strats) == 0 {
		strats = []strategy.PiscineStrategy{
			strategy.NewGoStrategy(),
			strategy.NewJSStrategy(),
			strategy.NewAIStrategy(),
		}
	}
	return NewRaidUseCase(edu, tmpls, strats)
}

// helper to build a fake edu client with one active raid right now.
func eduWithActiveRaid(t *testing.T, raidName string, week, teams int) *fakeEduClient {
	t.Helper()
	now := time.Now()
	return &fakeEduClient{
		piscine: &domain.PiscineInfo{ID: 1},
		raids: []domain.RaidInfo{
			{
				RaidName:   raidName,
				WeekNumber: week,
				TeamsCount: teams,
				StartDate:  now.Add(-24 * time.Hour),
				EndDate:    now.Add(24 * time.Hour),
			},
		},
	}
}

// --- BuildMessage tests ---

func TestBuildMessage_RendersWithVars(t *testing.T) {
	edu := eduWithActiveRaid(t, "quad", 1, 9)
	tmpls := &fakeTemplates{templates: map[string]string{
		"exam_announcement": "Раид {{RAID_NAME}}, команд {{TEAMS_COUNT}}",
	}}
	uc := newUCWithDeps(edu, tmpls)

	got, err := uc.BuildMessage(context.Background(), domain.PiscineGo, domain.MsgExamAnnouncement, nil)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	want := "Раид quad, команд 9"
	if got != want {
		t.Errorf("BuildMessage = %q, want %q", got, want)
	}

	if len(tmpls.rendered) != 1 {
		t.Fatalf("expected 1 render call, got %d", len(tmpls.rendered))
	}
	call := tmpls.rendered[0]
	if call.key != "exam_announcement" {
		t.Errorf("template key = %q, want %q", call.key, "exam_announcement")
	}
}

func TestBuildMessage_ExtraVarsAreForwarded(t *testing.T) {
	edu := eduWithActiveRaid(t, "sortable", 2, 4)
	tmpls := &fakeTemplates{templates: map[string]string{
		"student_message": "Ссылка: {{SHEET_URL}} рейд {{RAID_NAME}}",
	}}
	uc := newUCWithDeps(edu, tmpls)

	got, err := uc.BuildMessage(context.Background(), domain.PiscineJS, domain.MsgStudentMessage,
		map[string]string{"SHEET_URL": "https://x/y"})
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	if !strings.Contains(got, "https://x/y") {
		t.Errorf("expected SHEET_URL in output: %q", got)
	}
	if !strings.Contains(got, "sortable") {
		t.Errorf("expected RAID_NAME in output: %q", got)
	}
}

func TestBuildMessage_RefusesUnsupportedMessageForWeek(t *testing.T) {
	// Piscine AI has no hackathon (HasHackathon == false). Hackathon should be
	// rejected for AI regardless of week number.
	edu := eduWithActiveRaid(t, "backtesting-sp500", 1, 3)
	tmpls := &fakeTemplates{templates: map[string]string{"hackathon": "should never render"}}
	uc := newUCWithDeps(edu, tmpls)

	_, err := uc.BuildMessage(context.Background(), domain.PiscineAI, domain.MsgHackathon, nil)
	if err == nil {
		t.Fatal("expected error for hackathon on AI piscine, got nil")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("error should explain unsupported: %v", err)
	}
	if len(tmpls.rendered) != 0 {
		t.Error("template should not have been rendered for unsupported message")
	}
}

func TestBuildMessage_UnknownPiscine(t *testing.T) {
	edu := eduWithActiveRaid(t, "quad", 1, 1)
	uc := newUCWithDeps(edu, &fakeTemplates{},
		strategy.NewGoStrategy()) // only Go registered
	_, err := uc.BuildMessage(context.Background(), domain.PiscineJS, domain.MsgFAQ, nil)
	if err == nil {
		t.Fatal("expected error for unregistered piscine")
	}
}

func TestBuildMessage_FinalWeekUsesStubRaidInfo(t *testing.T) {
	// All raids have ended -> final week, ActiveRaid == nil.
	now := time.Now()
	edu := &fakeEduClient{
		piscine: &domain.PiscineInfo{ID: 1},
		raids: []domain.RaidInfo{
			{RaidName: "quad", WeekNumber: 1, StartDate: now.Add(-30 * 24 * time.Hour), EndDate: now.Add(-25 * 24 * time.Hour)},
			{RaidName: "sudoku", WeekNumber: 2, StartDate: now.Add(-20 * 24 * time.Hour), EndDate: now.Add(-15 * 24 * time.Hour)},
			{RaidName: "quadchecker", WeekNumber: 3, StartDate: now.Add(-10 * 24 * time.Hour), EndDate: now.Add(-5 * 24 * time.Hour)},
		},
	}
	tmpls := &fakeTemplates{templates: map[string]string{
		"final_exam": "Final week, raid={{RAID_NAME}}, teams={{TEAMS_COUNT}}",
	}}
	uc := newUCWithDeps(edu, tmpls)

	got, err := uc.BuildMessage(context.Background(), domain.PiscineGo, domain.MsgFinalExam, nil)
	if err != nil {
		t.Fatalf("BuildMessage: %v", err)
	}
	// Stub RaidInfo has empty fields, which surface as empty substitutions.
	if !strings.Contains(got, "raid=") || !strings.Contains(got, "teams=0") {
		t.Errorf("expected empty raid name and teams=0 in stub render, got %q", got)
	}
}

func TestBuildMessage_TemplateRenderError(t *testing.T) {
	edu := eduWithActiveRaid(t, "quad", 1, 1)
	tmpls := &fakeTemplates{err: domain.ErrTemplateNotFound}
	uc := newUCWithDeps(edu, tmpls)

	_, err := uc.BuildMessage(context.Background(), domain.PiscineGo, domain.MsgFAQ, nil)
	if err == nil {
		t.Fatal("expected error when template render fails")
	}
}

// --- BuildDefenseReminder tests ---

func TestBuildDefenseReminder_PopulatesScheduleVars(t *testing.T) {
	edu := eduWithActiveRaid(t, "quad", 1, 9) // 9 teams -> 3 rows, no break
	tmpls := &fakeTemplates{templates: map[string]string{
		"defense_reminder": "rows={{ROWS}} slots={{TOTAL_SLOTS}} sched={{RECOMMENDED_SCHEDULE}} raid={{RAID_NAME}}",
	}}
	uc := newUCWithDeps(edu, tmpls)

	text, schedule, err := uc.BuildDefenseReminder(context.Background(), domain.PiscineGo)
	if err != nil {
		t.Fatalf("BuildDefenseReminder: %v", err)
	}
	if schedule == nil {
		t.Fatal("schedule is nil")
	}
	if schedule.Rows != 3 || schedule.TotalSlots != 9 {
		t.Errorf("schedule rows/slots = %d/%d, want 3/9", schedule.Rows, schedule.TotalSlots)
	}
	if !strings.Contains(text, "rows=3") || !strings.Contains(text, "slots=9") {
		t.Errorf("rendered text missing schedule vars: %q", text)
	}
	if !strings.Contains(text, "raid=quad") {
		t.Errorf("rendered text missing raid name: %q", text)
	}
}

func TestBuildDefenseReminder_NoActiveRaidReturnsError(t *testing.T) {
	// All raids ended -> final week -> ActiveRaid is nil -> defense reminder N/A.
	now := time.Now()
	edu := &fakeEduClient{
		piscine: &domain.PiscineInfo{ID: 1},
		raids: []domain.RaidInfo{
			{RaidName: "quad", WeekNumber: 1, StartDate: now.Add(-30 * 24 * time.Hour), EndDate: now.Add(-25 * 24 * time.Hour)},
			{RaidName: "sudoku", WeekNumber: 2, StartDate: now.Add(-20 * 24 * time.Hour), EndDate: now.Add(-15 * 24 * time.Hour)},
			{RaidName: "quadchecker", WeekNumber: 3, StartDate: now.Add(-10 * 24 * time.Hour), EndDate: now.Add(-5 * 24 * time.Hour)},
		},
	}
	uc := newUCWithDeps(edu, &fakeTemplates{})

	_, _, err := uc.BuildDefenseReminder(context.Background(), domain.PiscineGo)
	if err == nil {
		t.Fatal("expected error when no active raid, got nil")
	}
}

func TestGetStrategy(t *testing.T) {
	uc := newUCWithDeps(eduWithActiveRaid(t, "quad", 1, 1), &fakeTemplates{})

	s, ok := uc.GetStrategy(domain.PiscineGo)
	if !ok {
		t.Fatal("expected GoStrategy to be registered")
	}
	if s.Type() != domain.PiscineGo {
		t.Errorf("strategy Type=%q, want %q", s.Type(), domain.PiscineGo)
	}

	if _, ok := uc.GetStrategy(domain.PiscineType("nope")); ok {
		t.Error("expected ok=false for unknown piscine")
	}
}
