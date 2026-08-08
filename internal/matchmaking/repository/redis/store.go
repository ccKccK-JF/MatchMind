package redis

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

type DurableStore interface {
	application.TicketStore
	application.MatchQueue
	application.AssignedTicketCompleter
	application.ActiveTicketReader
	ActiveTickets(ctx context.Context) ([]*domain.MatchTicket, error)
}

// Store combines Redis coordination with PostgreSQL durability. Redis wins
// the short-lived reservation race; PostgreSQL remains the source of truth.
type Store struct {
	durable DurableStore
	queue   *Queue
}

type RebuildResult struct {
	RecoveredReservations int
	RestoredTickets       int
}

func NewStore(durable DurableStore, queue *Queue) *Store {
	return &Store{durable: durable, queue: queue}
}

func (s *Store) Rebuild(ctx context.Context, now time.Time) (RebuildResult, error) {
	if err := s.queue.Ping(ctx); err != nil {
		return RebuildResult{}, err
	}
	recovered, err := s.durable.RecoverExpiredReservations(ctx, now)
	if err != nil {
		return RebuildResult{}, err
	}
	if _, err := s.queue.RecoverExpiredReservations(ctx, now); err != nil {
		return RebuildResult{}, err
	}
	tickets, err := s.durable.ActiveTickets(ctx)
	if err != nil {
		return RebuildResult{}, err
	}
	for _, ticket := range tickets {
		if err := s.queue.UpsertTicket(ctx, ticket, now); err != nil {
			return RebuildResult{}, err
		}
	}
	return RebuildResult{RecoveredReservations: recovered, RestoredTickets: len(tickets)}, nil
}

func (s *Store) CreateQueued(ctx context.Context, ticket *domain.MatchTicket, idempotencyKey string) (*domain.MatchTicket, error) {
	created, err := s.durable.CreateQueued(ctx, ticket, idempotencyKey)
	if err != nil {
		return nil, err
	}
	switch created.State() {
	case domain.TicketStateQueued, domain.TicketStateReserved:
		if err := s.queue.UpsertTicket(ctx, created, time.Now()); err != nil {
			return nil, err
		}
	default:
		// A very old idempotency key may resolve to an already terminal Ticket.
		// Return that original response without reintroducing it into the queue.
		if err := s.queue.RemoveTicket(ctx, created); err != nil {
			return nil, err
		}
	}
	return created, nil
}

func (s *Store) Get(ctx context.Context, ticketID string) (*domain.MatchTicket, error) {
	return s.durable.Get(ctx, ticketID)
}

func (s *Store) GetActiveByPlayer(ctx context.Context, playerID string) (*domain.MatchTicket, error) {
	return s.durable.GetActiveByPlayer(ctx, playerID)
}

func (s *Store) Cancel(
	ctx context.Context,
	ticketID, playerID, idempotencyKey string,
	now time.Time,
) (*domain.MatchTicket, error) {
	ticket, err := s.durable.Cancel(ctx, ticketID, playerID, idempotencyKey, now)
	if err != nil {
		return nil, err
	}
	if err := s.queue.RemoveTicket(ctx, ticket); err != nil {
		return nil, err
	}
	return ticket, nil
}

func (s *Store) PoolKeys(ctx context.Context) ([]domain.PoolKey, error) {
	return s.queue.PoolKeys(ctx)
}

func (s *Store) QueueSnapshot(ctx context.Context, key domain.PoolKey, limit int) ([]*domain.MatchTicket, error) {
	return s.queue.QueueSnapshot(ctx, key, limit)
}

func (s *Store) ReserveAll(
	ctx context.Context,
	ticketIDs []string,
	reservationID string,
	expiresAt, now time.Time,
) ([]*domain.MatchTicket, error) {
	if _, err := s.queue.ReserveAll(ctx, ticketIDs, reservationID, expiresAt, now); err != nil {
		return nil, err
	}
	reserved, err := s.durable.ReserveAll(ctx, ticketIDs, reservationID, expiresAt, now)
	if err != nil {
		if releaseErr := s.queue.ReleaseAll(ctx, reservationID, now); releaseErr != nil {
			slog.Error("rollback Redis reservation after PostgreSQL rejection", "error", releaseErr, "reservation_id", reservationID)
		}
		return nil, err
	}
	return reserved, nil
}

func (s *Store) ReleaseAll(ctx context.Context, reservationID string, now time.Time) error {
	if err := s.durable.ReleaseAll(ctx, reservationID, now); err != nil {
		return err
	}
	return s.queue.ReleaseAll(ctx, reservationID, now)
}

func (s *Store) AssignAll(
	ctx context.Context,
	reservationID, matchID string,
	now time.Time,
) ([]*domain.MatchTicket, error) {
	assigned, err := s.durable.AssignAll(ctx, reservationID, matchID, now)
	if err != nil {
		return nil, err
	}
	if err := s.queue.FinalizeAssignment(ctx, reservationID, matchID, now); err != nil {
		slog.Warn("PostgreSQL assignment committed but Redis finalization failed", "error", err, "match_id", matchID)
	}
	return assigned, nil
}

func (s *Store) FinalizeAssignment(ctx context.Context, reservationID, matchID string, now time.Time) error {
	return s.queue.FinalizeAssignment(ctx, reservationID, matchID, now)
}

func (s *Store) RecoverExpiredReservations(ctx context.Context, now time.Time) (int, error) {
	recovered, err := s.durable.RecoverExpiredReservations(ctx, now)
	if err != nil {
		return 0, err
	}
	if _, err := s.queue.RecoverExpiredReservations(ctx, now); err != nil {
		return recovered, err
	}
	return recovered, nil
}

func (s *Store) QueueSize(ctx context.Context) (int, error) {
	return s.queue.QueueSize(ctx)
}

func (s *Store) CompleteAssignedTickets(ctx context.Context, matchID string, now time.Time) error {
	if err := s.durable.CompleteAssignedTickets(ctx, matchID, now); err != nil {
		return fmt.Errorf("complete durable assigned tickets: %w", err)
	}
	return nil
}

var _ application.TicketStore = (*Store)(nil)
var _ application.MatchQueue = (*Store)(nil)
var _ application.AssignedTicketCompleter = (*Store)(nil)
