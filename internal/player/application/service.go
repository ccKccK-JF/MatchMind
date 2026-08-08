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
	GetBanStates(ctx context.Context, playerIDs []string) (map[string]bool, error)
	UpdateRegionLatency(ctx context.Context, playerID string, latencies map[string]int, updatedAt time.Time) (*domain.Player, error)
	SetBanState(ctx context.Context, playerID string, banned bool, reason, operatorID string, changedAt time.Time) (*domain.Player, error)
}

type Clock func() time.Time

func defaultClock() time.Time { return time.Now() }

type Service struct {
	repository Repository
	clock      Clock
}

type CreatePlayerCommand struct {
	ID              string
	Name            string
	InitialRating   float64
	PreferredRoles  []domain.Role
	HomeRegion      string
	RegionLatency   map[string]int
	BehaviorScore   float64
	HeroProficiency map[string]float64
}

type UpdateRegionLatencyCommand struct {
	PlayerID string
	Latency  map[string]int
}

type SetPlayerBanCommand struct {
	PlayerID   string
	Banned     bool
	Reason     string
	OperatorID string
}

func NewService(repository Repository, clock Clock) *Service {
	if clock == nil {
		clock = defaultClock
	}
	return &Service{repository: repository, clock: clock}
}

func (s *Service) CreatePlayer(ctx context.Context, command CreatePlayerCommand) (*domain.Player, error) {
	player, err := domain.NewPlayer(domain.NewPlayerParams{
		ID:              command.ID,
		Name:            command.Name,
		InitialRating:   command.InitialRating,
		PreferredRoles:  command.PreferredRoles,
		HomeRegion:      command.HomeRegion,
		RegionLatency:   command.RegionLatency,
		BehaviorScore:   command.BehaviorScore,
		HeroProficiency: command.HeroProficiency,
		CreatedAt:       s.clock(),
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

func (s *Service) SetPlayerBan(ctx context.Context, command SetPlayerBanCommand) (*domain.Player, error) {
	command.PlayerID = strings.TrimSpace(command.PlayerID)
	command.OperatorID = strings.TrimSpace(command.OperatorID)
	if command.PlayerID == "" || command.OperatorID == "" {
		return nil, domain.ErrInvalidPlayer
	}
	return s.repository.SetBanState(
		ctx, command.PlayerID, command.Banned, command.Reason, command.OperatorID, s.clock(),
	)
}

// GetPlayerBanStates returns one entry for every existing requested player.
// Missing IDs are deliberately omitted so callers can fail closed.
func (s *Service) GetPlayerBanStates(ctx context.Context, playerIDs []string) (map[string]bool, error) {
	if len(playerIDs) == 0 || len(playerIDs) > 1000 {
		return nil, domain.ErrInvalidPlayer
	}
	normalized := make([]string, 0, len(playerIDs))
	seen := make(map[string]struct{}, len(playerIDs))
	for _, playerID := range playerIDs {
		playerID = strings.TrimSpace(playerID)
		if playerID == "" {
			return nil, domain.ErrInvalidPlayer
		}
		if _, duplicate := seen[playerID]; duplicate {
			continue
		}
		seen[playerID] = struct{}{}
		normalized = append(normalized, playerID)
	}
	return s.repository.GetBanStates(ctx, normalized)
}
