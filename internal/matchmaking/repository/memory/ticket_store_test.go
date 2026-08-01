package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

func TestTicketStoreOrdersQueueByCreationTime(t *testing.T) {
	store := NewTicketStore()
	now := time.Now()
	newer := queuedTicket(t, "ticket-2", "player-2", now.Add(time.Second))
	older := queuedTicket(t, "ticket-1", "player-1", now)
	if _, err := store.CreateQueued(context.Background(), newer, "create-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateQueued(context.Background(), older, "create-1"); err != nil {
		t.Fatal(err)
	}

	queue, err := store.QueueSnapshot(context.Background(), domain.PoolKey{
		Mode: "ranked_5v5", ClientVersion: "1.0.0", Region: "hongkong",
	}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(queue) != 2 || queue[0].ID() != older.ID() || queue[1].ID() != newer.ID() {
		t.Fatalf("queue order = %v, want [%s %s]", ticketIDs(queue), older.ID(), newer.ID())
	}
}

func TestTicketStoreConcurrentActiveTicketProtection(t *testing.T) {
	store := NewTicketStore()
	const attempts = 100
	var successful atomic.Int32
	var conflicts atomic.Int32
	var waitGroup sync.WaitGroup
	waitGroup.Add(attempts)

	for index := range attempts {
		go func() {
			defer waitGroup.Done()
			ticket := queuedTicket(t, fmt.Sprintf("ticket-%d", index), "player-1", time.Now())
			_, err := store.CreateQueued(context.Background(), ticket, fmt.Sprintf("key-%d", index))
			switch {
			case err == nil:
				successful.Add(1)
			case errors.Is(err, application.ErrActiveTicketExists):
				conflicts.Add(1)
			default:
				t.Errorf("CreateQueued() error = %v", err)
			}
		}()
	}
	waitGroup.Wait()
	if successful.Load() != 1 || conflicts.Load() != attempts-1 {
		t.Fatalf("successful/conflicts = %d/%d, want 1/%d", successful.Load(), conflicts.Load(), attempts-1)
	}
}

func TestTicketStoreRejectsWrongOwnerCancellation(t *testing.T) {
	store := NewTicketStore()
	ticket := queuedTicket(t, "ticket-1", "player-1", time.Now())
	_, _ = store.CreateQueued(context.Background(), ticket, "create-1")
	_, err := store.Cancel(context.Background(), ticket.ID(), "player-2", "cancel-1", time.Now())
	if !errors.Is(err, application.ErrTicketForbidden) {
		t.Fatalf("Cancel() error = %v, want ErrTicketForbidden", err)
	}
}

func TestTicketStoreReserveAssignAll(t *testing.T) {
	store := NewTicketStore()
	now := time.Now()
	ids := make([]string, 0, 10)
	for index := range 10 {
		ticket := queuedTicket(t, fmt.Sprintf("ticket-%d", index), fmt.Sprintf("player-%d", index), now)
		if _, err := store.CreateQueued(context.Background(), ticket, fmt.Sprintf("create-%d", index)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, ticket.ID())
	}

	reserved, err := store.ReserveAll(context.Background(), ids, "reservation-1", now.Add(time.Minute), now)
	if err != nil {
		t.Fatalf("ReserveAll() error = %v", err)
	}
	for _, ticket := range reserved {
		if ticket.State() != domain.TicketStateReserved || ticket.ReservationID() != "reservation-1" {
			t.Fatalf("reserved ticket state/id = %s/%s", ticket.State(), ticket.ReservationID())
		}
	}
	assigned, err := store.AssignAll(context.Background(), "reservation-1", "match-1", now.Add(time.Second))
	if err != nil {
		t.Fatalf("AssignAll() error = %v", err)
	}
	for _, ticket := range assigned {
		if ticket.State() != domain.TicketStateAssigned {
			t.Fatalf("assigned ticket state = %s", ticket.State())
		}
	}
}

func TestTicketStoreReservationIsAllOrNothing(t *testing.T) {
	store := NewTicketStore()
	now := time.Now()
	first := queuedTicket(t, "ticket-1", "player-1", now)
	second := queuedTicket(t, "ticket-2", "player-2", now)
	_, _ = store.CreateQueued(context.Background(), first, "create-1")
	_, _ = store.CreateQueued(context.Background(), second, "create-2")
	_, err := store.ReserveAll(context.Background(), []string{first.ID()}, "reservation-1", now.Add(time.Minute), now)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.ReserveAll(
		context.Background(), []string{first.ID(), second.ID()}, "reservation-2", now.Add(time.Minute), now,
	)
	if !errors.Is(err, application.ErrReservationConflict) {
		t.Fatalf("ReserveAll() error = %v, want ErrReservationConflict", err)
	}
	gotSecond, err := store.Get(context.Background(), second.ID())
	if err != nil {
		t.Fatal(err)
	}
	if gotSecond.State() != domain.TicketStateQueued {
		t.Fatalf("second ticket state = %s, want QUEUED after failed batch", gotSecond.State())
	}
}

func TestTicketStoreRecoversExpiredReservations(t *testing.T) {
	store := NewTicketStore()
	now := time.Now()
	ticket := queuedTicket(t, "ticket-1", "player-1", now)
	_, _ = store.CreateQueued(context.Background(), ticket, "create-1")
	_, err := store.ReserveAll(context.Background(), []string{ticket.ID()}, "reservation-1", now.Add(time.Second), now)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := store.RecoverExpiredReservations(context.Background(), now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	got, _ := store.Get(context.Background(), ticket.ID())
	if got.State() != domain.TicketStateQueued || got.ReservationID() != "" {
		t.Fatalf("recovered ticket state/id = %s/%q", got.State(), got.ReservationID())
	}
}

func queuedTicket(t *testing.T, ticketID, playerID string, createdAt time.Time) *domain.MatchTicket {
	t.Helper()
	ticket, err := domain.NewTicket(domain.NewTicketParams{
		ID:             ticketID,
		PlayerID:       playerID,
		Mode:           "ranked_5v5",
		ClientVersion:  "1.0.0",
		Region:         "hongkong",
		Rating:         1500,
		PreferredRoles: []domain.Role{domain.RoleCore},
		RegionLatency:  map[string]int{"hongkong": 30},
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ticket.Queue(createdAt); err != nil {
		t.Fatal(err)
	}
	return ticket
}

func ticketIDs(tickets []*domain.MatchTicket) []string {
	ids := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		ids = append(ids, ticket.ID())
	}
	return ids
}
