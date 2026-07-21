package strategy

import "admin-bot/internal/domain"

// AIStrategy implements PiscineStrategy for a Piscine AI stream. AI runs as
// three independent streams, so the constructor is parameterized with the
// concrete PiscineType.
type AIStrategy struct {
	baseStrategy
}

func NewAIStrategy(piscineType domain.PiscineType) *AIStrategy {
	return &AIStrategy{
		baseStrategy: baseStrategy{
			piscineType:  piscineType,
			hasHackathon: false,
			totalWeeks:   4,
		},
	}
}
