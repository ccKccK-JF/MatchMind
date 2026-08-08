package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

type MatchRepository interface {
	Create(ctx context.Context, match *domain.Match) error
	Get(ctx context.Context, matchID string) (*domain.Match, error)
	Update(ctx context.Context, match *domain.Match) error
}

type AssignedTicketCompleter interface {
	CompleteAssignedTickets(ctx context.Context, matchID string, now time.Time) error
}

type MatchCompletionCoordinator interface {
	CompleteMatchAndReleaseTickets(ctx context.Context, match *domain.Match, now time.Time) error
}

type MatchService struct {
	repository      MatchRepository
	ticketCompleter AssignedTicketCompleter
	serverReleaser  ServerReleaser
	clock           func() time.Time
}

func (s *MatchService) SetServerReleaser(releaser ServerReleaser) {
	s.serverReleaser = releaser
}

func NewMatchService(repository MatchRepository, ticketCompleter AssignedTicketCompleter, clock func() time.Time) *MatchService {
	if clock == nil {
		clock = time.Now
	}
	return &MatchService{repository: repository, ticketCompleter: ticketCompleter, clock: clock}
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
		if errors.Is(err, ErrMatchRevisionConflict) {
			current, getErr := s.GetMatch(ctx, matchID)
			if getErr == nil && current.State() == domain.MatchStateRunning {
				return current, nil
			}
		}
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
		if s.ticketCompleter != nil {
			if err := s.ticketCompleter.CompleteAssignedTickets(ctx, match.ID(), s.clock()); err != nil {
				return nil, err
			}
		}
		if err := s.releaseServer(ctx, match); err != nil {
			return nil, err
		}
		return match, nil
	}
	now := s.clock()
	if err := match.Complete(result, now); err != nil {
		return nil, err
	}
	if coordinator, ok := s.repository.(MatchCompletionCoordinator); ok {
		if err := coordinator.CompleteMatchAndReleaseTickets(ctx, match, now); err != nil {
			return s.resolveCompletionConflict(ctx, matchID, err)
		}
	} else {
		if err := s.repository.Update(ctx, match); err != nil {
			return s.resolveCompletionConflict(ctx, matchID, err)
		}
		if s.ticketCompleter != nil {
			if err := s.ticketCompleter.CompleteAssignedTickets(ctx, match.ID(), now); err != nil {
				return nil, err
			}
		}
	}
	if err := s.releaseServer(ctx, match); err != nil {
		return nil, err
	}
	return match.Clone(), nil
}

func (s *MatchService) releaseServer(ctx context.Context, match *domain.Match) error {
	if s.serverReleaser == nil {
		return nil
	}
	return s.serverReleaser.Release(ctx, match.ID(), match.ServerRegion())
}

func (s *MatchService) resolveCompletionConflict(ctx context.Context, matchID string, cause error) (*domain.Match, error) {
	if !errors.Is(cause, ErrMatchRevisionConflict) {
		return nil, cause
	}
	current, err := s.GetMatch(ctx, matchID)
	if err != nil || current.State() != domain.MatchStateFinished {
		return nil, cause
	}
	if s.ticketCompleter != nil {
		if err := s.ticketCompleter.CompleteAssignedTickets(ctx, current.ID(), s.clock()); err != nil {
			return nil, err
		}
	}
	if err := s.releaseServer(ctx, current); err != nil {
		return nil, err
	}
	return current, nil
}

func (s *MatchService) GetMatch(ctx context.Context, matchID string) (*domain.Match, error) {
	return s.repository.Get(ctx, strings.TrimSpace(matchID))
}
