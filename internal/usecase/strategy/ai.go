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
