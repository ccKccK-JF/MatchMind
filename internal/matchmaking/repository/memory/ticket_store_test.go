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

func TestTicketStoreOneHundredConcurrentPlayers(t *testing.T) {
	store := NewTicketStore()
	const players = 100
	var successful atomic.Int32
	var waitGroup sync.WaitGroup
	waitGroup.Add(players)

	for index := range players {
		go func() {
			defer waitGroup.Done()
			ticket := queuedTicket(
				t,
				fmt.Sprintf("ticket-%d", index),
				fmt.Sprintf("player-%d", index),
				time.Now(),
			)
			if _, err := store.CreateQueued(context.Background(), ticket, fmt.Sprintf("create-%d", index)); err != nil {
				t.Errorf("CreateQueued(%d) error = %v", index, err)
				return
			}
			successful.Add(1)
		}()
	}
	waitGroup.Wait()

	size, err := store.QueueSize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if successful.Load() != players || size != players {
		t.Fatalf("successful/queue size = %d/%d, want %d/%d", successful.Load(), size, players, players)
	}
}

func TestTicketStoreCreateAndCancelRaceRemainsConsistent(t *testing.T) {
	store := NewTicketStore()
	now := time.Now()
	first := queuedTicket(t, "ticket-1", "player-1", now)
	if _, err := store.CreateQueued(context.Background(), first, "create-1"); err != nil {
		t.Fatal(err)
	}

	const attempts = 100
	start := make(chan struct{})
	var waitGroup sync.WaitGroup
	waitGroup.Add(attempts)
	for index := range attempts {
		go func() {
			defer waitGroup.Done()
			<-start
			if index%2 == 0 {
				_, err := store.Cancel(context.Background(), first.ID(), "player-1", "cancel-1", now.Add(time.Second))
				if err != nil {
					t.Errorf("Cancel() error = %v", err)
				}
				return
			}
			second := queuedTicket(t, "ticket-2", "player-1", now.Add(time.Second))
			_, err := store.CreateQueued(context.Background(), second, "create-2")
			if err != nil && !errors.Is(err, application.ErrActiveTicketExists) {
				t.Errorf("CreateQueued() error = %v", err)
			}
		}()
	}
	close(start)
	waitGroup.Wait()

	firstStored, err := store.Get(context.Background(), first.ID())
	if err != nil {
		t.Fatal(err)
	}
	if firstStored.State() != domain.TicketStateCancelled {
		t.Fatalf("first ticket state = %s, want CANCELLED", firstStored.State())
	}
	size, err := store.QueueSize(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if size > 1 {
		t.Fatalf("active queue size = %d, want at most one ticket for player", size)
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

func TestTicketStoreReservationExpiryAndConfirmationRaceIsAtomic(t *testing.T) {
	for iteration := range 100 {
		store := NewTicketStore()
		now := time.Now()
		ids := make([]string, 0, 10)
		for index := range 10 {
			ticket := queuedTicket(
				t,
				fmt.Sprintf("ticket-%d-%d", iteration, index),
				fmt.Sprintf("player-%d-%d", iteration, index),
				now,
			)
			if _, err := store.CreateQueued(context.Background(), ticket, fmt.Sprintf("create-%d", index)); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, ticket.ID())
		}
		if _, err := store.ReserveAll(context.Background(), ids, "reservation", now.Add(time.Second), now); err != nil {
			t.Fatal(err)
		}

		start := make(chan struct{})
		results := make(chan error, 2)
		go func() {
			<-start
			_, err := store.AssignAll(context.Background(), "reservation", "match-1", now.Add(time.Second))
			results <- err
		}()
		go func() {
			<-start
			_, err := store.RecoverExpiredReservations(context.Background(), now.Add(time.Second))
			results <- err
		}()
		close(start)
		firstErr, secondErr := <-results, <-results
		if firstErr != nil && secondErr != nil {
			t.Fatalf("both competing operations failed: %v / %v", firstErr, secondErr)
		}

		var state domain.TicketState
		for _, ticketID := range ids {
			ticket, err := store.Get(context.Background(), ticketID)
			if err != nil {
				t.Fatal(err)
			}
			if state == "" {
				state = ticket.State()
			}
			if ticket.State() != state {
				t.Fatalf("reservation batch has mixed states: %s and %s", state, ticket.State())
			}
		}
		if state != domain.TicketStateQueued && state != domain.TicketStateAssigned {
			t.Fatalf("final reservation state = %s, want QUEUED or ASSIGNED", state)
		}
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
