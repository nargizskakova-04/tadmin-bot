package strategy

import "admin-bot/internal/domain"

// GoStrategy implements PiscineStrategy for Piscine Go.
type GoStrategy struct {
	baseStrategy
}

func NewGoStrategy() *GoStrategy {
	return &GoStrategy{
		baseStrategy: baseStrategy{
			piscineType:  domain.PiscineGo,
			hasHackathon: true,
			totalWeeks:   4,
		},
	}
}

func (s *GoStrategy) Type() domain.PiscineType { return domain.PiscineGo }

func (s *GoStrategy) TemplateVars(msgType domain.MessageType, info *domain.RaidInfo, extra map[string]string) map[string]string {
	return buildCommonVars(info, extra)
}
