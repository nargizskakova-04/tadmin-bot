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
