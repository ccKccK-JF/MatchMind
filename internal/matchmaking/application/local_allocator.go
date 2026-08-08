package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	platformid "github.com/ccKccK-JF/MatchMind/internal/platform/id"
)

type LocalAllocator struct {
	mu             sync.Mutex
	tokenGenerator platformid.Generator
	capacities     map[string]int
	allocations    map[string]localServerAllocation
}

type localServerAllocation struct {
	region     string
	allocation Allocation
}

var ErrInvalidServerCapacity = errors.New("invalid game server capacity")

func ParseRegionCapacities(value string) (map[string]int, error) {
	result := make(map[string]int)
	for _, entry := range strings.Split(value, ",") {
		parts := strings.Split(strings.TrimSpace(entry), "=")
		if len(parts) != 2 {
			return nil, ErrInvalidServerCapacity
		}
		region := strings.ToLower(strings.TrimSpace(parts[0]))
		capacity, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || region == "" || capacity < 0 {
			return nil, ErrInvalidServerCapacity
		}
		result[region] += capacity
	}
	if len(result) == 0 {
		return nil, ErrInvalidServerCapacity
	}
	return result, nil
}

func NewLocalAllocator(tokenGenerator platformid.Generator) *LocalAllocator {
	allocator, _ := NewLocalAllocatorWithCapacities(map[string]int{
		"hongkong":  100,
		"singapore": 100,
		"tokyo":     100,
	}, tokenGenerator)
	return allocator
}

func NewLocalAllocatorWithCapacities(capacities map[string]int, tokenGenerator platformid.Generator) (*LocalAllocator, error) {
	if tokenGenerator == nil {
		tokenGenerator = platformid.UUID
	}
	normalized := make(map[string]int, len(capacities))
	for region, capacity := range capacities {
		region = strings.ToLower(strings.TrimSpace(region))
		if region == "" || capacity < 0 {
			return nil, ErrInvalidServerCapacity
		}
		normalized[region] += capacity
	}
	if len(normalized) == 0 {
		return nil, ErrInvalidServerCapacity
	}
	return &LocalAllocator{
		tokenGenerator: tokenGenerator,
		capacities:     normalized,
		allocations:    make(map[string]localServerAllocation),
	}, nil
}

func (a *LocalAllocator) Capacities(ctx context.Context) ([]RegionCapacity, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	used := make(map[string]int, len(a.capacities))
	for _, allocation := range a.allocations {
		used[allocation.region]++
	}
	result := make([]RegionCapacity, 0, len(a.capacities))
	for region, capacity := range a.capacities {
		result = append(result, RegionCapacity{Region: region, AvailableServers: capacity - used[region]})
	}
	return result, nil
}

func (a *LocalAllocator) Allocate(ctx context.Context, matchID, region string) (Allocation, error) {
	if err := ctx.Err(); err != nil {
		return Allocation{}, err
	}
	matchID = strings.TrimSpace(matchID)
	region = strings.ToLower(strings.TrimSpace(region))
	if matchID == "" || region == "" {
		return Allocation{}, ErrNoServerCapacity
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if existing, ok := a.allocations[matchID]; ok {
		return existing.allocation, nil
	}
	used := 0
	for _, allocation := range a.allocations {
		if allocation.region == region {
			used++
		}
	}
	if a.capacities[region]-used <= 0 {
		return Allocation{}, ErrNoServerCapacity
	}
	token, err := a.tokenGenerator()
	if err != nil {
		return Allocation{}, err
	}
	allocation := Allocation{
		Address: fmt.Sprintf("%s.game.matchmind.local:7000", region),
		Token:   token,
	}
	a.allocations[matchID] = localServerAllocation{region: region, allocation: allocation}
	return allocation, nil
}

func (a *LocalAllocator) Release(ctx context.Context, matchID, _ string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	delete(a.allocations, strings.TrimSpace(matchID))
	a.mu.Unlock()
	return nil
}
