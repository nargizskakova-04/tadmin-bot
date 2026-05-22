package strategy

import (
	"testing"

	"admin-bot/internal/domain"
)

// Each PiscineStrategy is verified end-to-end: its Type, the rule matrix
// for SupportsMessage, the TemplateKey mapping, and TemplateVars output.

type stratCtor func() PiscineStrategy

func allStrategies() map[domain.PiscineType]stratCtor {
	return map[domain.PiscineType]stratCtor{
		domain.PiscineGo: func() PiscineStrategy { return NewGoStrategy() },
		domain.PiscineJS: func() PiscineStrategy { return NewJSStrategy() },
		domain.PiscineAI: func() PiscineStrategy { return NewAIStrategy() },
	}
}

func TestStrategy_Type(t *testing.T) {
	for p, ctor := range allStrategies() {
		if got := ctor().Type(); got != p {
			t.Errorf("Type() = %q, want %q", got, p)
		}
	}
}

// TestSupportsMessage_Matrix is the master rule table.
// Indexed [piscine][messageType] -> []bool of length TotalWeeks(piscine),
// where weekN (1-indexed) maps to index N-1.
func TestSupportsMessage_Matrix(t *testing.T) {
	// Go and JS run 4 weeks (week 4 is final). AI runs 3 weeks (week 3 is final).
	type row struct {
		msg  domain.MessageType
		want []bool // per-week
	}

	goJS := []row{
		{domain.MsgFAQ, []bool{true, false, false, false}},
		{domain.MsgExamAnnouncement, []bool{true, true, true, false}},
		{domain.MsgHackathon, []bool{false, false, true, false}},
		{domain.MsgDefenseReminder, []bool{true, true, true, false}},
		{domain.MsgStudentMessage, []bool{true, true, true, false}},
		{domain.MsgFinalExam, []bool{false, false, false, true}},
	}
	ai := []row{
		{domain.MsgFAQ, []bool{true, false, false}},
		{domain.MsgExamAnnouncement, []bool{true, true, false}},
		{domain.MsgHackathon, []bool{false, false, false}}, // AI has no hackathon
		{domain.MsgDefenseReminder, []bool{true, true, false}},
		{domain.MsgStudentMessage, []bool{true, true, false}},
		{domain.MsgFinalExam, []bool{false, false, true}},
	}

	matrix := map[domain.PiscineType][]row{
		domain.PiscineGo: goJS,
		domain.PiscineJS: goJS,
		domain.PiscineAI: ai,
	}

	for p, ctor := range allStrategies() {
		s := ctor()
		for _, r := range matrix[p] {
			for week0, want := range r.want {
				week := week0 + 1
				got := s.SupportsMessage(r.msg, week)
				if got != want {
					t.Errorf("%s.SupportsMessage(%q, week=%d) = %v, want %v",
						p, r.msg, week, got, want)
				}
			}
		}
	}
}

func TestSupportsMessage_UnknownMessageType(t *testing.T) {
	s := NewGoStrategy()
	if s.SupportsMessage(domain.MessageType("nope"), 1) {
		t.Error("unknown message type should not be supported")
	}
}

func TestTemplateKey_MatchesMessageTypeString(t *testing.T) {
	// TemplateKey is just the string form of the message type; a regression here
	// would silently make the template loader miss files.
	s := NewGoStrategy()
	cases := []domain.MessageType{
		domain.MsgFAQ,
		domain.MsgExamAnnouncement,
		domain.MsgHackathon,
		domain.MsgDefenseReminder,
		domain.MsgStudentMessage,
		domain.MsgFinalExam,
	}
	for _, m := range cases {
		if got := s.TemplateKey(m); got != string(m) {
			t.Errorf("TemplateKey(%q) = %q, want %q", m, got, string(m))
		}
	}
}

func TestTemplateVars_IncludesCommonKeys(t *testing.T) {
	s := NewGoStrategy()
	info := &domain.RaidInfo{
		RaidName:   "quad",
		TeamsCount: 9,
	}
	vars := s.TemplateVars(domain.MsgExamAnnouncement, info, nil)

	if vars["RAID_NAME"] != "quad" {
		t.Errorf("RAID_NAME=%q, want %q", vars["RAID_NAME"], "quad")
	}
	if vars["TEAMS_COUNT"] != "9" {
		t.Errorf("TEAMS_COUNT=%q, want %q", vars["TEAMS_COUNT"], "9")
	}
}

func TestTemplateVars_ExtraOverlay(t *testing.T) {
	s := NewGoStrategy()
	info := &domain.RaidInfo{RaidName: "x", TeamsCount: 3}
	extra := map[string]string{
		"SHEET_URL": "https://example.com/sheet",
		"RAID_NAME": "OVERRIDDEN", // extras win over common vars
	}
	vars := s.TemplateVars(domain.MsgStudentMessage, info, extra)

	if vars["SHEET_URL"] != "https://example.com/sheet" {
		t.Errorf("SHEET_URL missing: %v", vars)
	}
	if vars["RAID_NAME"] != "OVERRIDDEN" {
		t.Errorf("extras should overlay common vars, got RAID_NAME=%q", vars["RAID_NAME"])
	}
}

func TestTemplateVars_NilExtraIsOK(t *testing.T) {
	s := NewAIStrategy()
	info := &domain.RaidInfo{RaidName: "backtesting-sp500", TeamsCount: 4}
	if vars := s.TemplateVars(domain.MsgFAQ, info, nil); vars["RAID_NAME"] != "backtesting-sp500" {
		t.Errorf("nil extras path failed: %v", vars)
	}
}

// Compile-time check that each constructor satisfies the interface.
var (
	_ PiscineStrategy = (*GoStrategy)(nil)
	_ PiscineStrategy = (*JSStrategy)(nil)
	_ PiscineStrategy = (*AIStrategy)(nil)
)
