package strategy

import "admin-bot/internal/domain"

// AIStrategy implements PiscineStrategy for Piscine AI.
type AIStrategy struct {
	baseStrategy
}

func NewAIStrategy() *AIStrategy {
	return &AIStrategy{
		baseStrategy: baseStrategy{
			piscineType:  domain.PiscineAI,
			hasHackathon: false,
			totalWeeks:   3,
		},
	}
}

func (s *AIStrategy) Type() domain.PiscineType { return domain.PiscineAI }

func (s *AIStrategy) TemplateVars(msgType domain.MessageType, info *domain.RaidInfo, extra map[string]string) map[string]string {
	return buildCommonVars(info, extra)
}
