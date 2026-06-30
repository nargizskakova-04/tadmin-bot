package usecase

import (
	"admin-bot/internal/domain"
	"context"
	"fmt"
)

type AstanaUpdatesUseCase struct {
	eduClient domain.OneEduClient
}

func NewAstanaUpdatesUseCase(eduClient domain.OneEduClient) *AstanaUpdatesUseCase {
	return &AstanaUpdatesUseCase{
		eduClient: eduClient,
	}
}

func (u *AstanaUpdatesUseCase) GetAstanaUpdates(ctx context.Context) (domain.AstanaUpdatesInfo, error) {
	astanaUpdate, err := u.eduClient.GetAstanaUpdates(ctx)
	if err != nil {
		return domain.AstanaUpdatesInfo{}, fmt.Errorf("get astana updates: %w", err)
	}
	return domain.AstanaUpdatesInfo{
		Total:     astanaUpdate.Total,
		Succeeded: astanaUpdate.Succeeded,
		Checkin:   astanaUpdate.Checkin,
		Piscinego: astanaUpdate.Piscinego,
	}, nil
}
