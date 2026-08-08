package redis

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/memory"
	redisclient "github.com/redis/go-redis/v9"
)

func TestStoreRunsCompleteWorkerReservationFlow(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisclient.NewClient(&redisclient.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	queue := NewQueue(client, "worker")
	durable := memory.NewTicketStore()
	store := NewStore(durable, queue)
	ctx := context.Background()
	now := time.Now().UTC()
	roles := []domain.Role{
		domain.RoleVanguard, domain.RoleRoamer, domain.RoleCore,
		domain.RoleRanged, domain.RoleSupport,
	}
	for index := range 10 {
		createdAt := now.Add(time.Duration(index) * time.Millisecond)
		ticket, err := domain.NewTicket(domain.NewTicketParams{
			ID: fmt.Sprintf("ticket-%02d", index), PlayerID: fmt.Sprintf("player-%02d", index),
			Mode: "ranked_5v5", ClientVersion: "1.0.0", Region: "hongkong", Rating: 1500,
			PreferredRoles: []domain.Role{roles[index%5]}, RegionLatency: map[string]int{"hongkong": 30},
			CreatedAt: createdAt,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := ticket.Queue(createdAt); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateQueued(ctx, ticket, fmt.Sprintf("create-%02d", index)); err != nil {
			t.Fatal(err)
		}
	}
	ids := []string{"reservation-1", "match-1"}
	idIndex := 0
	workerNow := now.Add(20 * time.Millisecond)
	matchStore := memory.NewMatchStore()
	worker, err := application.NewWorker(
		store, matchStore, application.NewLocalAllocator(func() (string, error) { return "token", nil }),
		domain.DefaultPolicy(), func() (string, error) {
			id := ids[idIndex]
			idIndex++
			return id, nil
		}, func() time.Time { return workerNow },
	)
	if err != nil {
		t.Fatal(err)
	}
	match, err := worker.RunOnce(ctx)
	if err != nil || match.State() != domain.MatchStateReady {
		t.Fatalf("RunOnce() = %#v, %v", match, err)
	}
	if size, _ := store.QueueSize(ctx); size != 0 {
		t.Fatalf("Redis queue size after Match = %d", size)
	}
	for index := range 10 {
		ticket, err := durable.Get(ctx, fmt.Sprintf("ticket-%02d", index))
		if err != nil || ticket.State() != domain.TicketStateAssigned || ticket.MatchID() != match.ID() {
			t.Fatalf("durable assigned ticket = %#v, %v", ticket, err)
		}
	}
}

func TestStoreRebuildsFromDurableTicketsAndRollsBackRedisConflict(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisclient.NewClient(&redisclient.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	queue := NewQueue(client, "hybrid")
	durable := memory.NewTicketStore()
	store := NewStore(durable, queue)
	ctx := context.Background()
	now := time.Now().UTC()
	key := domain.PoolKey{Mode: "ranked_5v5", ClientVersion: "1.0.0", Region: "hongkong"}

	ticket := redisTestTicket(t, "durable-ticket", now)
	if _, err := store.CreateQueued(ctx, ticket, "create-durable"); err != nil {
		t.Fatal(err)
	}
	server.FlushAll()
	rebuild, err := store.Rebuild(ctx, now)
	if err != nil || rebuild.RestoredTickets != 1 {
		t.Fatalf("Rebuild() = %#v, %v", rebuild, err)
	}
	snapshot, err := store.QueueSnapshot(ctx, key, 10)
	if err != nil || len(snapshot) != 1 || snapshot[0].ID() != ticket.ID() {
		t.Fatalf("rebuilt snapshot = %#v, %v", snapshot, err)
	}
	if _, err := store.Cancel(ctx, ticket.ID(), ticket.PlayerID(), "cancel-durable", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	idempotent, err := store.CreateQueued(ctx, ticket, "create-durable")
	if err != nil || idempotent.State() != domain.TicketStateCancelled {
		t.Fatalf("terminal idempotent create = %#v, %v", idempotent, err)
	}
	if size, _ := store.QueueSize(ctx); size != 0 {
		t.Fatalf("terminal idempotent create requeued Ticket, size = %d", size)
	}

	stale := redisTestTicket(t, "redis-only-ticket", now.Add(time.Second))
	if err := queue.UpsertTicket(ctx, stale, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveAll(ctx, []string{stale.ID()}, "stale-reservation", now.Add(time.Minute), now); err == nil {
		t.Fatal("ReserveAll() for Redis-only ticket succeeded, want durable rejection")
	}
	queued, err := queue.QueueSnapshot(ctx, key, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range queued {
		found = found || candidate.ID() == stale.ID()
	}
	if !found {
		t.Fatal("Redis reservation was not rolled back after durable rejection")
	}
}

func TestStoreRebuildsUnexpiredDurableReservation(t *testing.T) {
	server := miniredis.RunT(t)
	client := redisclient.NewClient(&redisclient.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	queue := NewQueue(client, "reserved-rebuild")
	durable := memory.NewTicketStore()
	store := NewStore(durable, queue)
	ctx := context.Background()
	now := time.Now().UTC()
	ticket := redisTestTicket(t, "ticket-reserved", now)
	if _, err := store.CreateQueued(ctx, ticket, "create-reserved"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReserveAll(ctx, []string{ticket.ID()}, "reservation-1", now.Add(time.Minute), now); err != nil {
		t.Fatal(err)
	}
	server.FlushAll()
	rebuild, err := store.Rebuild(ctx, now.Add(time.Second))
	if err != nil || rebuild.RestoredTickets != 1 {
		t.Fatalf("Rebuild() = %#v, %v", rebuild, err)
	}
	if size, _ := store.QueueSize(ctx); size != 0 {
		t.Fatalf("reserved Ticket reentered queue during rebuild, size = %d", size)
	}
	if err := store.ReleaseAll(ctx, "reservation-1", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if size, _ := store.QueueSize(ctx); size != 1 {
		t.Fatalf("released rebuilt reservation queue size = %d", size)
	}
}
