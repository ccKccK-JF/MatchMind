package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/player/application"
	"github.com/ccKccK-JF/MatchMind/internal/player/domain"
	"github.com/ccKccK-JF/MatchMind/internal/player/repository/memory"
)

func TestServiceCreateAndGetPlayer(t *testing.T) {
	createdAt := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	service := application.NewService(memory.NewRepository(), func() time.Time { return createdAt })
	command := application.CreatePlayerCommand{
		ID:             "player-1001",
		Name:           "Nova",
		InitialRating:  1500,
		PreferredRoles: []domain.Role{domain.RoleCore, domain.RoleSupport},
		HomeRegion:     "hongkong",
		RegionLatency:  map[string]int{"hongkong": 30},
		BehaviorScore:  95,
	}

	created, err := service.CreatePlayer(context.Background(), command)
	if err != nil {
		t.Fatalf("CreatePlayer() error = %v", err)
	}
	if !created.CreatedAt().Equal(createdAt) {
		t.Fatalf("CreatedAt() = %v, want %v", created.CreatedAt(), createdAt)
	}

	got, err := service.GetPlayer(context.Background(), command.ID)
	if err != nil {
		t.Fatalf("GetPlayer() error = %v", err)
	}
	if got.ID() != command.ID || got.Rating() != command.InitialRating {
		t.Fatalf("GetPlayer() returned unexpected player: id=%q rating=%v", got.ID(), got.Rating())
	}

	_, err = service.CreatePlayer(context.Background(), command)
	if !errors.Is(err, application.ErrPlayerAlreadyExists) {
		t.Fatalf("duplicate CreatePlayer() error = %v, want ErrPlayerAlreadyExists", err)
	}
}

func TestServiceGetMissingPlayer(t *testing.T) {
	service := application.NewService(memory.NewRepository(), nil)
	_, err := service.GetPlayer(context.Background(), "missing")
	if !errors.Is(err, application.ErrPlayerNotFound) {
		t.Fatalf("GetPlayer() error = %v, want ErrPlayerNotFound", err)
	}
}

func TestUpdateRegionLatencyPersistsValidatedMeasurements(t *testing.T) {
	repository := memory.NewRepository()
	service := application.NewService(repository, func() time.Time {
		return time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	})
	_, err := service.CreatePlayer(context.Background(), application.CreatePlayerCommand{
		ID: "player-1", Name: "Nova", InitialRating: 1500,
		PreferredRoles: []domain.Role{domain.RoleCore}, HomeRegion: "hongkong",
		RegionLatency: map[string]int{"hongkong": 30}, BehaviorScore: 95,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateRegionLatency(context.Background(), application.UpdateRegionLatencyCommand{
		PlayerID: " player-1 ", Latency: map[string]int{"HongKong": 22, "tokyo": 65},
	})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := service.GetPlayer(context.Background(), "player-1")
	if err != nil {
		t.Fatal(err)
	}
	if updated.RegionLatency()["hongkong"] != 22 || stored.RegionLatency()["tokyo"] != 65 {
		t.Fatalf("updated/stored latency = %#v/%#v", updated.RegionLatency(), stored.RegionLatency())
	}
}
