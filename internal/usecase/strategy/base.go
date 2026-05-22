package strategy

import (
	"fmt"

	"admin-bot/internal/domain"
)

// baseStrategy holds logic shared by all piscine strategies.
type baseStrategy struct {
	piscineType  domain.PiscineType
	hasHackathon bool
	totalWeeks   int
}

// Type returns the PiscineType this strategy handles.
func (b *baseStrategy) Type() domain.PiscineType { return b.piscineType }

// TemplateVars builds template variables for a given message type and raid info.
// The default implementation simply uses the common variables; subtypes can
// override if they need piscine-specific vars.
func (b *baseStrategy) TemplateVars(_ domain.MessageType, info *domain.RaidInfo, extra map[string]string) map[string]string {
	return buildCommonVars(info, extra)
}

// SupportsMessage checks whether a message type is applicable for the given week.
func (b *baseStrategy) SupportsMessage(msgType domain.MessageType, weekNumber int) bool {
	isFinal := weekNumber == b.totalWeeks

	switch msgType {
	case domain.MsgFAQ:
		return weekNumber == 1

	case domain.MsgExamAnnouncement:
		// Weeks 1 through (totalWeeks-1), i.e. non-final weeks.
		return !isFinal

	case domain.MsgHackathon:
		// Week 3, only for piscines with a hackathon (Go & JS).
		return b.hasHackathon && weekNumber == 3

	case domain.MsgDefenseReminder, domain.MsgStudentMessage:
		// Non-final weeks (raids have defense).
		return !isFinal

	case domain.MsgFinalExam:
		return isFinal

	default:
		return false
	}
}

// TemplateKey returns the template filename (without .txt extension) for a message type.
func (b *baseStrategy) TemplateKey(msgType domain.MessageType) string {
	return string(msgType)
}

// buildCommonVars creates template variables shared across message types.
func buildCommonVars(info *domain.RaidInfo, extra map[string]string) map[string]string {
	vars := map[string]string{
		"RAID_NAME":   info.RaidName,
		"TEAMS_COUNT": fmt.Sprintf("%d", info.TeamsCount),
	}
	for k, v := range extra {
		vars[k] = v
	}
	return vars
}
