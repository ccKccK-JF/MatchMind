package memory

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/player/application"
	"github.com/ccKccK-JF/MatchMind/internal/player/domain"
)

type Repository struct {
	mu                sync.RWMutex
	players           map[string]*domain.Player
	ratingHistory     map[string][]*domain.RatingChange
	ratedMatchHistory map[string][]*domain.RatingChange
}

func NewRepository() *Repository {
	return &Repository{
		players:           make(map[string]*domain.Player),
		ratingHistory:     make(map[string][]*domain.RatingChange),
		ratedMatchHistory: make(map[string][]*domain.RatingChange),
	}
}

func (r *Repository) Create(ctx context.Context, player *domain.Player) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.players[player.ID()]; exists {
		return application.ErrPlayerAlreadyExists
	}
	r.players[player.ID()] = player.Clone()
	return nil
}

func (r *Repository) GetByID(ctx context.Context, playerID string) (*domain.Player, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	playerID = strings.TrimSpace(playerID)
	r.mu.RLock()
	player, exists := r.players[playerID]
	r.mu.RUnlock()
	if !exists {
		return nil, application.ErrPlayerNotFound
	}
	return player.Clone(), nil
}

func (r *Repository) UpdateRegionLatency(
	ctx context.Context,
	playerID string,
	latencies map[string]int,
	_ time.Time,
) (*domain.Player, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	playerID = strings.TrimSpace(playerID)
	r.mu.Lock()
	defer r.mu.Unlock()
	player, exists := r.players[playerID]
	if !exists {
		return nil, application.ErrPlayerNotFound
	}
	updated, err := player.WithRegionLatency(latencies)
	if err != nil {
		return nil, err
	}
	r.players[playerID] = updated
	return updated.Clone(), nil
}

// ApplyRatingChanges validates and commits all player changes under one lock.
// Repeating a processed match ID returns the original result without applying
// the rating delta again.
func (r *Repository) ApplyRatingChanges(ctx context.Context, matchID string, changes []*domain.RatingChange) ([]*domain.RatingChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(matchID) == "" || len(changes) == 0 {
		return nil, application.ErrRatingConflict
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, exists := r.ratedMatchHistory[matchID]; exists {
		return cloneChanges(existing), nil
	}

	updatedPlayers := make(map[string]*domain.Player, len(changes))
	for _, change := range changes {
		if change == nil || change.MatchID() != matchID {
			return nil, application.ErrRatingConflict
		}
		if _, duplicate := updatedPlayers[change.PlayerID()]; duplicate {
			return nil, application.ErrRatingConflict
		}
		player, exists := r.players[change.PlayerID()]
		if !exists {
			return nil, application.ErrPlayerNotFound
		}
		if player.Rating() != change.Before() {
			return nil, fmt.Errorf("%w: player %s rating changed", application.ErrRatingConflict, change.PlayerID())
		}
		updated, err := player.WithRating(change.After())
		if err != nil {
			return nil, err
		}
		updatedPlayers[change.PlayerID()] = updated
	}

	for playerID, player := range updatedPlayers {
		r.players[playerID] = player
	}
	for _, change := range changes {
		r.ratingHistory[change.PlayerID()] = append(r.ratingHistory[change.PlayerID()], change.Clone())
	}
	r.ratedMatchHistory[matchID] = cloneChanges(changes)
	return cloneChanges(changes), nil
}

func (r *Repository) RatingHistory(ctx context.Context, playerID string) ([]*domain.RatingChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	playerID = strings.TrimSpace(playerID)

	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, exists := r.players[playerID]; !exists {
		return nil, application.ErrPlayerNotFound
	}
	return cloneChanges(r.ratingHistory[playerID]), nil
}

func cloneChanges(changes []*domain.RatingChange) []*domain.RatingChange {
	clones := make([]*domain.RatingChange, 0, len(changes))
	for _, change := range changes {
		clones = append(clones, change.Clone())
	}
	return clones
}
