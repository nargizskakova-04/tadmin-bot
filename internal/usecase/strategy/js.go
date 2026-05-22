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
