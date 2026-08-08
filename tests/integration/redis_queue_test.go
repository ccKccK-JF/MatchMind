package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	matchdomain "github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	matchredis "github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/redis"
	redisclient "github.com/redis/go-redis/v9"
)

func TestRedisLuaAtomicReservationAndRecovery(t *testing.T) {
	address := os.Getenv("MATCHMIND_REDIS_TEST_ADDRESS")
	if address == "" {
		t.Skip("set MATCHMIND_REDIS_TEST_ADDRESS to run the real Redis integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client := redisclient.NewClient(&redisclient.Options{Addr: address})
	defer client.Close()
	prefix := fmt.Sprintf("matchmind-test-%d", time.Now().UnixNano())
	queue := matchredis.NewQueue(client, prefix)
	if err := queue.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		pattern := "{" + prefix + "}:matchmaking:*"
		var cursor uint64
		for {
			keys, next, err := client.Scan(cleanupContext, cursor, pattern, 100).Result()
			if err != nil {
				return
			}
			if len(keys) > 0 {
				_ = client.Del(cleanupContext, keys...).Err()
			}
			cursor = next
			if cursor == 0 {
				return
			}
		}
	})

	now := time.Now().UTC()
	ids := make([]string, 0, 10)
	for index := range 10 {
		id := fmt.Sprintf("ticket-%02d", index)
		ticket := newRedisIntegrationTicket(t, id, now.Add(time.Duration(index)*time.Millisecond))
		if err := queue.UpsertTicket(ctx, ticket, now); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if _, err := queue.ReserveAll(ctx, ids, "reservation-1", now.Add(time.Second), now); err != nil {
		t.Fatal(err)
	}
	recovered, err := queue.RecoverExpiredReservations(ctx, now.Add(2*time.Second))
	if err != nil || recovered != 10 {
		t.Fatalf("Redis recovery = %d, %v", recovered, err)
	}
	if size, err := queue.QueueSize(ctx); err != nil || size != 10 {
		t.Fatalf("Redis queue size = %d, %v", size, err)
	}
}

func newRedisIntegrationTicket(t *testing.T, id string, now time.Time) *matchdomain.MatchTicket {
	t.Helper()
	ticket, err := matchdomain.NewTicket(matchdomain.NewTicketParams{
		ID: id, PlayerID: "player-" + id, Mode: "ranked_5v5", ClientVersion: "1.0.0",
		Region: "hongkong", Rating: 1500, PreferredRoles: []matchdomain.Role{matchdomain.RoleCore},
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
