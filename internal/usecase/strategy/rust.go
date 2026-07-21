package strategy

import "admin-bot/internal/domain"

// RustStrategy implements PiscineStrategy for Piscine RUST.
type RustStrategy struct {
	baseStrategy
}

func NewRustStrategy() *RustStrategy {
	return &RustStrategy{
		baseStrategy: baseStrategy{
			piscineType:  domain.PiscineRUST,
			hasHackathon: false, // confirmed: Rust has no hackathon
			totalWeeks:   4,     // confirmed: Rust runs 4 weeks (like Go/JS)
		},
	}
}
