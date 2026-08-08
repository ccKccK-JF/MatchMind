package application_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
)

func TestLocalAllocatorTracksAndReleasesCapacity(t *testing.T) {
	tokens := 0
	allocator, err := application.NewLocalAllocatorWithCapacities(map[string]int{"tokyo": 1}, func() (string, error) {
		tokens++
		return "token", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := allocator.Allocate(context.Background(), "match-1", "tokyo")
	if err != nil {
		t.Fatal(err)
	}
	retry, err := allocator.Allocate(context.Background(), "match-1", "tokyo")
	if err != nil || retry != first || tokens != 1 {
		t.Fatalf("idempotent retry = %+v, %v, tokens=%d", retry, err, tokens)
	}
	if _, err := allocator.Allocate(context.Background(), "match-2", "tokyo"); !errors.Is(err, application.ErrNoServerCapacity) {
		t.Fatalf("second allocation error = %v", err)
	}
	capacities, err := allocator.Capacities(context.Background())
	if err != nil || len(capacities) != 1 || capacities[0].AvailableServers != 0 {
		t.Fatalf("capacities after allocation = %+v, %v", capacities, err)
	}
	if err := allocator.Release(context.Background(), "match-1", "tokyo"); err != nil {
		t.Fatal(err)
	}
	capacities, err = allocator.Capacities(context.Background())
	if err != nil || capacities[0].AvailableServers != 1 {
		t.Fatalf("capacities after release = %+v, %v", capacities, err)
	}
}

func TestParseRegionCapacities(t *testing.T) {
	capacities, err := application.ParseRegionCapacities("HongKong=2, tokyo=3,hongkong=1")
	if err != nil || capacities["hongkong"] != 3 || capacities["tokyo"] != 3 {
		t.Fatalf("ParseRegionCapacities() = %+v, %v", capacities, err)
	}
	for _, invalid := range []string{"", "hongkong", "hongkong=-1", "=2", "hongkong=two"} {
		if _, err := application.ParseRegionCapacities(invalid); !errors.Is(err, application.ErrInvalidServerCapacity) {
			t.Fatalf("ParseRegionCapacities(%q) error = %v", invalid, err)
		}
	}
}
