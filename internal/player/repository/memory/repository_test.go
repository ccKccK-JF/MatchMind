package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/player/application"
	"github.com/ccKccK-JF/MatchMind/internal/player/domain"
)

func TestRepositoryCreateAndGet(t *testing.T) {
	repository := NewRepository()
	player := testPlayer(t, "player-1001")

	if err := repository.Create(context.Background(), player); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := repository.Create(context.Background(), player); !errors.Is(err, application.ErrPlayerAlreadyExists) {
		t.Fatalf("second Create() error = %v, want ErrPlayerAlreadyExists", err)
	}

	got, err := repository.GetByID(context.Background(), player.ID())
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if got.ID() != player.ID() || got.Name() != player.Name() {
		t.Fatalf("GetByID() = %q/%q, want %q/%q", got.ID(), got.Name(), player.ID(), player.Name())
	}
}

func TestRepositoryGetMissingPlayer(t *testing.T) {
	_, err := NewRepository().GetByID(context.Background(), "missing")
	if !errors.Is(err, application.ErrPlayerNotFound) {
		t.Fatalf("GetByID() error = %v, want ErrPlayerNotFound", err)
	}
}

func TestRepositoryConcurrentDuplicateCreate(t *testing.T) {
	repository := NewRepository()
	player := testPlayer(t, "player-1001")

	const goroutines = 100
	var successful atomic.Int32
	var duplicates atomic.Int32
	var waitGroup sync.WaitGroup
	waitGroup.Add(goroutines)

	for range goroutines {
		go func() {
			defer waitGroup.Done()
			err := repository.Create(context.Background(), player)
			switch {
			case err == nil:
				successful.Add(1)
			case errors.Is(err, application.ErrPlayerAlreadyExists):
				duplicates.Add(1)
			default:
				t.Errorf("Create() unexpected error = %v", err)
			}
		}()
	}
	waitGroup.Wait()

	if successful.Load() != 1 {
		t.Fatalf("successful creates = %d, want 1", successful.Load())
	}
	if duplicates.Load() != goroutines-1 {
		t.Fatalf("duplicate creates = %d, want %d", duplicates.Load(), goroutines-1)
	}
}

func testPlayer(t *testing.T, id string) *domain.Player {
	t.Helper()
	player, err := domain.NewPlayer(domain.NewPlayerParams{
		ID:             id,
		Name:           "Nova",
		InitialRating:  1500,
		PreferredRoles: []domain.Role{domain.RoleCore},
		HomeRegion:     "hongkong",
		RegionLatency:  map[string]int{"hongkong": 30},
		BehaviorScore:  90,
		CreatedAt:      time.Now(),
	})
	if err != nil {
		t.Fatalf("NewPlayer() error = %v", err)
	}
	return player
}
