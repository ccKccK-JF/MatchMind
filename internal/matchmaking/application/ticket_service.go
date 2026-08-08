package application

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	platformid "github.com/ccKccK-JF/MatchMind/internal/platform/id"
)

var (
	ErrTicketNotFound           = errors.New("ticket not found")
	ErrActiveTicketExists       = errors.New("player already has an active ticket")
	ErrTicketForbidden          = errors.New("ticket does not belong to player")
	ErrIdempotencyKeyRequired   = errors.New("idempotency key is required")
	ErrPlayerNotFound           = errors.New("player not found")
	ErrPlayerBanned             = errors.New("player is banned")
	ErrPlayerServiceUnavailable = errors.New("player service unavailable")
	ErrReservationConflict      = errors.New("ticket reservation conflict")
	ErrMatchNotFound            = errors.New("match not found")
	ErrMatchAlreadyExists       = errors.New("match already exists")
	ErrMatchRevisionConflict    = errors.New("match revision conflict")
)

type PlayerSnapshot struct {
	ID             string
	Rating         float64
	Banned         bool
	PreferredRoles []domain.Role
	RegionLatency  map[string]int
}

type PlayerReader interface {
	GetPlayer(ctx context.Context, playerID string) (PlayerSnapshot, error)
}

type TicketStore interface {
	CreateQueued(ctx context.Context, ticket *domain.MatchTicket, idempotencyKey string) (*domain.MatchTicket, error)
	Get(ctx context.Context, ticketID string) (*domain.MatchTicket, error)
	Cancel(ctx context.Context, ticketID, playerID, idempotencyKey string, now time.Time) (*domain.MatchTicket, error)
}

type ActiveTicketReader interface {
	GetActiveByPlayer(ctx context.Context, playerID string) (*domain.MatchTicket, error)
}

type TicketService struct {
	store       TicketStore
	players     PlayerReader
	idGenerator platformid.Generator
	clock       func() time.Time
}

type CreateTicketCommand struct {
	PlayerID       string
	PartyID        string
	Mode           string
	ClientVersion  string
	PreferredRoles []domain.Role
	RegionLatency  map[string]int
	IdempotencyKey string
}

func NewTicketService(store TicketStore, players PlayerReader, idGenerator platformid.Generator, clock func() time.Time) *TicketService {
	if idGenerator == nil {
		idGenerator = platformid.UUID
	}
	if clock == nil {
		clock = time.Now
	}
	return &TicketService{store: store, players: players, idGenerator: idGenerator, clock: clock}
}

func (s *TicketService) CreateTicket(ctx context.Context, command CreateTicketCommand) (*domain.MatchTicket, error) {
	command.IdempotencyKey = strings.TrimSpace(command.IdempotencyKey)
	if command.IdempotencyKey == "" {
		return nil, ErrIdempotencyKeyRequired
	}

	player, err := s.players.GetPlayer(ctx, command.PlayerID)
	if err != nil {
		return nil, err
	}
	if player.Banned {
		return nil, ErrPlayerBanned
	}
	if len(command.PreferredRoles) == 0 {
		command.PreferredRoles = player.PreferredRoles
	}
	if len(command.RegionLatency) == 0 {
		command.RegionLatency = player.RegionLatency
	}
	region, err := bestRegion(command.RegionLatency)
	if err != nil {
		return nil, err
	}

	ticketID, err := s.idGenerator()
	if err != nil {
		return nil, fmt.Errorf("generate ticket id: %w", err)
	}
	now := s.clock()
	ticket, err := domain.NewTicket(domain.NewTicketParams{
		ID:             ticketID,
		PlayerID:       player.ID,
		PartyID:        command.PartyID,
		Mode:           command.Mode,
		ClientVersion:  command.ClientVersion,
		Region:         region,
		Rating:         player.Rating,
		PreferredRoles: command.PreferredRoles,
		RegionLatency:  command.RegionLatency,
		CreatedAt:      now,
	})
	if err != nil {
		return nil, err
	}
	if err := ticket.Queue(now); err != nil {
		return nil, err
	}
	return s.store.CreateQueued(ctx, ticket, command.IdempotencyKey)
}

func (s *TicketService) GetTicket(ctx context.Context, ticketID string) (*domain.MatchTicket, error) {
	return s.store.Get(ctx, strings.TrimSpace(ticketID))
}

func (s *TicketService) GetActiveTicketForPlayer(ctx context.Context, playerID string) (*domain.MatchTicket, error) {
	playerID = strings.TrimSpace(playerID)
	if playerID == "" {
		return nil, domain.ErrInvalidTicket
	}
	reader, ok := s.store.(ActiveTicketReader)
	if !ok {
		return nil, ErrTicketNotFound
	}
	return reader.GetActiveByPlayer(ctx, playerID)
}

func (s *TicketService) CancelTicket(ctx context.Context, ticketID, playerID, idempotencyKey string) (*domain.MatchTicket, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return nil, ErrIdempotencyKeyRequired
	}
	return s.store.Cancel(ctx, ticketID, playerID, idempotencyKey, s.clock())
}

func bestRegion(latencies map[string]int) (string, error) {
	if len(latencies) == 0 {
		return "", fmt.Errorf("%w: region latency is required", domain.ErrInvalidTicket)
	}
	regions := make([]string, 0, len(latencies))
	for region := range latencies {
		regions = append(regions, region)
	}
	sort.Strings(regions)
	best := regions[0]
	for _, region := range regions[1:] {
		if latencies[region] < latencies[best] {
			best = region
		}
	}
	return strings.ToLower(strings.TrimSpace(best)), nil
}
