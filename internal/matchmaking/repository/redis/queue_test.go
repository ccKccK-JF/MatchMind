package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	redisclient "github.com/redis/go-redis/v9"
)

func TestQueueLuaReservationIsAtomicAndRecoverable(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisclient.NewClient(&redisclient.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	queue := NewQueue(client, "test")
	ctx := context.Background()
	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	key := domain.PoolKey{Mode: "ranked_5v5", ClientVersion: "1.0.0", Region: "hongkong"}

	ticketIDs := make([]string, 0, 10)
	for index := range 10 {
		ticket := redisTestTicket(t, fmt.Sprintf("ticket-%02d", index), now.Add(time.Duration(index)*time.Millisecond))
		if err := queue.UpsertTicket(ctx, ticket, now); err != nil {
			t.Fatal(err)
		}
		ticketIDs = append(ticketIDs, ticket.ID())
	}
	pools, err := queue.PoolKeys(ctx)
	if err != nil || len(pools) != 1 || pools[0] != key {
		t.Fatalf("PoolKeys() = %#v, %v", pools, err)
	}
	snapshot, err := queue.QueueSnapshot(ctx, key, 10)
	if err != nil || len(snapshot) != 10 || snapshot[0].ID() != "ticket-00" {
		t.Fatalf("QueueSnapshot() = %d tickets, %v", len(snapshot), err)
	}

	invalidBatch := append([]string(nil), ticketIDs[:9]...)
	invalidBatch = append(invalidBatch, "missing-ticket")
	if _, err := queue.ReserveAll(ctx, invalidBatch, "bad-reservation", now.Add(time.Minute), now); !errors.Is(err, application.ErrReservationConflict) {
		t.Fatalf("invalid ReserveAll() error = %v", err)
	}
	if size, _ := queue.QueueSize(ctx); size != 10 {
		t.Fatalf("queue size after failed atomic reservation = %d, want 10", size)
	}
	poolKey, err := client.HGet(ctx, queue.ticketPoolsKey(), ticketIDs[9]).Result()
	if err != nil {
		t.Fatal(err)
	}
	client.HDel(ctx, queue.ticketPoolsKey(), ticketIDs[9])
	if _, err := queue.ReserveAll(ctx, ticketIDs, "missing-metadata", now.Add(time.Minute), now); !errors.Is(err, application.ErrReservationConflict) {
		t.Fatalf("metadata-invalid ReserveAll() error = %v", err)
	}
	for _, ticketID := range ticketIDs {
		state, _ := client.HGet(ctx, queue.statesKey(), ticketID).Result()
		if state != string(domain.TicketStateQueued) {
			t.Fatalf("ticket %s mutated before failed metadata validation: %s", ticketID, state)
		}
	}
	client.HSet(ctx, queue.ticketPoolsKey(), ticketIDs[9], poolKey)

	reserved, err := queue.ReserveAll(ctx, ticketIDs, "reservation-1", now.Add(time.Second), now)
	if err != nil || len(reserved) != 10 {
		t.Fatalf("ReserveAll() = %d tickets, %v", len(reserved), err)
	}
	if size, _ := queue.QueueSize(ctx); size != 0 {
		t.Fatalf("queue size while reserved = %d, want 0", size)
	}
	if retried, err := queue.ReserveAll(ctx, ticketIDs, "reservation-1", now.Add(time.Second), now); err != nil || len(retried) != 10 {
		t.Fatalf("idempotent ReserveAll() = %d tickets, %v", len(retried), err)
	}
	if _, err := queue.ReserveAll(ctx, ticketIDs[:9], "reservation-1", now.Add(time.Second), now); !errors.Is(err, application.ErrReservationConflict) {
		t.Fatalf("mismatched idempotent ReserveAll() error = %v", err)
	}
	for _, ticketID := range ticketIDs {
		state, _ := client.HGet(ctx, queue.statesKey(), ticketID).Result()
		if state != string(domain.TicketStateReserved) {
			t.Fatalf("mismatched retry mutated ticket %s: %s", ticketID, state)
		}
	}
	if _, err := queue.ReserveAll(ctx, ticketIDs, "reservation-2", now.Add(time.Minute), now); !errors.Is(err, application.ErrReservationConflict) {
		t.Fatalf("competing ReserveAll() error = %v", err)
	}

	recovered, err := queue.RecoverExpiredReservations(ctx, now.Add(2*time.Second))
	if err != nil || recovered != 10 {
		t.Fatalf("RecoverExpiredReservations() = %d, %v", recovered, err)
	}
	if size, _ := queue.QueueSize(ctx); size != 10 {
		t.Fatalf("queue size after recovery = %d, want 10", size)
	}

	if _, err := queue.ReserveAll(ctx, ticketIDs, "reservation-3", now.Add(time.Minute), now); err != nil {
		t.Fatal(err)
	}
	client.Del(ctx, queue.reservationKey("reservation-3"))
	if err := queue.FinalizeAssignment(ctx, "reservation-3", "match-1", now); err != nil {
		t.Fatal(err)
	}
	for _, ticketID := range ticketIDs {
		state, _ := client.HGet(ctx, queue.statesKey(), ticketID).Result()
		if state != string(domain.TicketStateAssigned) {
			t.Fatalf("ticket %s was not finalized after reservation set expiry: %s", ticketID, state)
		}
	}
	if size, _ := queue.QueueSize(ctx); size != 0 {
		t.Fatalf("queue size after assignment = %d, want 0", size)
	}
}

func TestQueueUpsertRebuildsMissingEntry(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisclient.NewClient(&redisclient.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	queue := NewQueue(client, "rebuild")
	now := time.Now().UTC()
	ticket := redisTestTicket(t, "ticket-1", now)
	if err := queue.UpsertTicket(context.Background(), ticket, now); err != nil {
		t.Fatal(err)
	}
	server.FlushAll()
	if err := queue.UpsertTicket(context.Background(), ticket, now); err != nil {
		t.Fatal(err)
	}
	snapshot, err := queue.QueueSnapshot(context.Background(), domain.PoolKey{
		Mode: "ranked_5v5", ClientVersion: "1.0.0", Region: "hongkong",
	}, 10)
	if err != nil || len(snapshot) != 1 || snapshot[0].ID() != ticket.ID() {
		t.Fatalf("rebuilt QueueSnapshot() = %#v, %v", snapshot, err)
	}
}

func TestQueueConcurrentReservationsHaveOneWinner(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisclient.NewClient(&redisclient.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	queue := NewQueue(client, "concurrent")
	ctx := context.Background()
	now := time.Now().UTC()
	ids := make([]string, 0, 10)
	for index := range 10 {
		ticket := redisTestTicket(t, fmt.Sprintf("ticket-%02d", index), now)
		if err := queue.UpsertTicket(ctx, ticket, now); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, ticket.ID())
	}
	const workers = 20
	var successes atomic.Int32
	var conflicts atomic.Int32
	var waitGroup sync.WaitGroup
	waitGroup.Add(workers)
	for index := range workers {
		go func() {
			defer waitGroup.Done()
			_, err := queue.ReserveAll(ctx, ids, fmt.Sprintf("reservation-%02d", index), now.Add(time.Minute), now)
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, application.ErrReservationConflict):
				conflicts.Add(1)
			default:
				t.Errorf("ReserveAll() error = %v", err)
			}
		}()
	}
	waitGroup.Wait()
	if successes.Load() != 1 || conflicts.Load() != workers-1 {
		t.Fatalf("successes/conflicts = %d/%d, want 1/%d", successes.Load(), conflicts.Load(), workers-1)
	}
}

func redisTestTicket(t *testing.T, id string, now time.Time) *domain.MatchTicket {
	t.Helper()
	ticket, err := domain.NewTicket(domain.NewTicketParams{
		ID: id, PlayerID: "player-" + id, Mode: "ranked_5v5", ClientVersion: "1.0.0",
		Region: "hongkong", Rating: 1500, PreferredRoles: []domain.Role{domain.RoleCore},
		RegionLatency: map[string]int{"hongkong": 30}, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ticket.Queue(now); err != nil {
		t.Fatal(err)
	}
	return ticket
}
