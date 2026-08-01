package application

import (
	"context"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

type MatchRepository interface {
	Create(ctx context.Context, match *domain.Match) error
	Get(ctx context.Context, matchID string) (*domain.Match, error)
	Update(ctx context.Context, match *domain.Match) error
}

type MatchService struct {
	repository MatchRepository
	clock      func() time.Time
}

func NewMatchService(repository MatchRepository, clock func() time.Time) *MatchService {
	if clock == nil {
		clock = time.Now
	}
	return &MatchService{repository: repository, clock: clock}
}

func (s *MatchService) StartMatch(ctx context.Context, matchID string) (*domain.Match, error) {
	match, err := s.GetMatch(ctx, matchID)
	if err != nil {
		return nil, err
	}
	if match.State() == domain.MatchStateRunning {
		return match, nil
	}
	if err := match.Start(s.clock()); err != nil {
		return nil, err
	}
	if err := s.repository.Update(ctx, match); err != nil {
		return nil, err
	}
	return match.Clone(), nil
}

func (s *MatchService) CompleteMatch(ctx context.Context, matchID string, result domain.MatchResult) (*domain.Match, error) {
	match, err := s.GetMatch(ctx, matchID)
	if err != nil {
		return nil, err
	}
	if match.State() == domain.MatchStateFinished {
		return match, nil
	}
	if err := match.Complete(result, s.clock()); err != nil {
		return nil, err
	}
	if err := s.repository.Update(ctx, match); err != nil {
		return nil, err
	}
	return match.Clone(), nil
}

func (s *MatchService) GetMatch(ctx context.Context, matchID string) (*domain.Match, error) {
	return s.repository.Get(ctx, strings.TrimSpace(matchID))
}
