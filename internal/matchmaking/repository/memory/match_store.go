package memory

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

type MatchStore struct {
	mu      sync.RWMutex
	matches map[string]*domain.Match
}

func NewMatchStore() *MatchStore {
	return &MatchStore{matches: make(map[string]*domain.Match)}
}

func (s *MatchStore) Create(ctx context.Context, match *domain.Match) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.matches[match.ID()]; exists {
		return application.ErrMatchAlreadyExists
	}
	s.matches[match.ID()] = match.Clone()
	return nil
}

func (s *MatchStore) Get(ctx context.Context, matchID string) (*domain.Match, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	match, exists := s.matches[strings.TrimSpace(matchID)]
	s.mu.RUnlock()
	if !exists {
		return nil, application.ErrMatchNotFound
	}
	return match.Clone(), nil
}

func (s *MatchStore) Update(ctx context.Context, match *domain.Match) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, exists := s.matches[match.ID()]
	if !exists {
		return application.ErrMatchNotFound
	}
	if match.Revision() != current.Revision()+1 {
		return application.ErrMatchRevisionConflict
	}
	s.matches[match.ID()] = match.Clone()
	return nil
}

func (s *MatchStore) ListFinished(
	ctx context.Context,
	filter application.MatchHistoryFilter,
) ([]*domain.Match, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	result := make([]*domain.Match, 0, len(s.matches))
	for _, match := range s.matches {
		if match.State() != domain.MatchStateFinished ||
			(filter.PolicyVersion != "" && match.PolicyVersion() != filter.PolicyVersion) ||
			(filter.Mode != "" && match.Mode() != filter.Mode) ||
			(filter.ServerRegion != "" && match.ServerRegion() != filter.ServerRegion) ||
			(!filter.From.IsZero() && match.CreatedAt().Before(filter.From)) ||
			(!filter.To.IsZero() && !match.CreatedAt().Before(filter.To)) {
			continue
		}
		result = append(result, match.Clone())
	}
	s.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt().Equal(result[j].CreatedAt()) {
			return result[i].ID() > result[j].ID()
		}
		return result[i].CreatedAt().After(result[j].CreatedAt())
	})
	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}
	return result, nil
}

var _ application.MatchHistoryReader = (*MatchStore)(nil)
