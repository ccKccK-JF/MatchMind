package memory

import (
	"context"
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
