package memory

import (
	"context"
	"sync"

	"github.com/ccKccK-JF/MatchMind/internal/simulation/application"
	simulationdomain "github.com/ccKccK-JF/MatchMind/internal/simulation/domain"
)

type ResultStore struct {
	mu      sync.RWMutex
	results map[string]simulationdomain.Result
}

func NewResultStore() *ResultStore {
	return &ResultStore{results: make(map[string]simulationdomain.Result)}
}

func (s *ResultStore) Get(ctx context.Context, matchID string) (simulationdomain.Result, error) {
	if err := ctx.Err(); err != nil {
		return simulationdomain.Result{}, err
	}
	s.mu.RLock()
	result, exists := s.results[matchID]
	s.mu.RUnlock()
	if !exists {
		return simulationdomain.Result{}, application.ErrResultNotFound
	}
	return result, nil
}

func (s *ResultStore) Save(ctx context.Context, result simulationdomain.Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.results[result.MatchID]; !exists {
		s.results[result.MatchID] = result
	}
	return nil
}
