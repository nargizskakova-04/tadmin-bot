package strategy

import "admin-bot/internal/domain"

// JSStrategy implements PiscineStrategy for Piscine JS.
type JSStrategy struct {
	baseStrategy
}

func NewJSStrategy() *JSStrategy {
	return &JSStrategy{
		baseStrategy: baseStrategy{
			piscineType:  domain.PiscineJS,
			hasHackathon: true,
			totalWeeks:   4,
		},
	}
}

func (s *JSStrategy) Type() domain.PiscineType { return domain.PiscineJS }

func (s *JSStrategy) TemplateVars(msgType domain.MessageType, info *domain.RaidInfo, extra map[string]string) map[string]string {
	return buildCommonVars(info, extra)
}
	