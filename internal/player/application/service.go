package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/player/domain"
)

var (
	ErrPlayerAlreadyExists = errors.New("player already exists")
	ErrPlayerNotFound      = errors.New("player not found")
)

type Repository interface {
	Create(ctx context.Context, player *domain.Player) error
	GetByID(ctx context.Context, playerID string) (*domain.Player, error)
	UpdateRegionLatency(ctx context.Context, playerID string, latencies map[string]int, updatedAt time.Time) (*domain.Player, error)
}

type Clock func() time.Time

func defaultClock() time.Time { return time.Now() }

type Service struct {
	repository Repository
	clock      Clock
}

type CreatePlayerCommand struct {
	ID             string
	Name           string
	InitialRating  float64
	PreferredRoles []domain.Role
	HomeRegion     string
	RegionLatency  map[string]int
	BehaviorScore  float64
}

type UpdateRegionLatencyCommand struct {
	PlayerID string
	Latency  map[string]int
}

func NewService(repository Repository, clock Clock) *Service {
	if clock == nil {
		clock = defaultClock
	}
	return &Service{repository: repository, clock: clock}
}

func (s *Service) CreatePlayer(ctx context.Context, command CreatePlayerCommand) (*domain.Player, error) {
	player, err := domain.NewPlayer(domain.NewPlayerParams{
		ID:             command.ID,
		Name:           command.Name,
		InitialRating:  command.InitialRating,
		PreferredRoles: command.PreferredRoles,
		HomeRegion:     command.HomeRegion,
		RegionLatency:  command.RegionLatency,
		BehaviorScore:  command.BehaviorScore,
		CreatedAt:      s.clock(),
	})
	if err != nil {
		return nil, err
	}

	if err := s.repository.Create(ctx, player); err != nil {
		return nil, err
	}
	return player.Clone(), nil
}

func (s *Service) GetPlayer(ctx context.Context, playerID string) (*domain.Player, error) {
	return s.repository.GetByID(ctx, playerID)
}

func (s *Service) UpdateRegionLatency(ctx context.Context, command UpdateRegionLatencyCommand) (*domain.Player, error) {
	command.PlayerID = strings.TrimSpace(command.PlayerID)
	if command.PlayerID == "" {
		return nil, domain.ErrInvalidPlayer
	}
	return s.repository.UpdateRegionLatency(ctx, command.PlayerID, command.Latency, s.clock())
}
