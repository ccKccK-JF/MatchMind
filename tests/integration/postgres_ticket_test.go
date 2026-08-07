package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	matchdomain "github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	matchpostgres "github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/postgres"
	playerdomain "github.com/ccKccK-JF/MatchMind/internal/player/domain"
	playerpostgres "github.com/ccKccK-JF/MatchMind/internal/player/repository/postgres"
	"github.com/ccKccK-JF/MatchMind/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgreSQLTicketPersistenceAndAtomicReservation(t *testing.T) {
	dsn := os.Getenv("MATCHMIND_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set MATCHMIND_POSTGRES_TEST_DSN to run the real PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	schema := fmt.Sprintf("matchmind_test_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		_, _ = admin.Exec(cleanupContext, "DROP SCHEMA "+identifier+" CASCADE")
	})

	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}

	players := playerpostgres.NewRepository(pool)
	tickets := matchpostgres.NewTicketStore(pool)
	now := time.Now().UTC().Truncate(time.Microsecond)
	ticketIDs := make([]string, 0, 10)
	roles := []matchdomain.Role{
		matchdomain.RoleVanguard, matchdomain.RoleRoamer, matchdomain.RoleCore,
		matchdomain.RoleRanged, matchdomain.RoleSupport,
	}
	for index := range 10 {
		playerID := fmt.Sprintf("player-%02d", index)
		player, err := playerdomain.NewPlayer(playerdomain.NewPlayerParams{
			ID: playerID, Name: playerID, InitialRating: 1500,
			PreferredRoles: []playerdomain.Role{playerdomain.Role(roles[index%5])},
			HomeRegion:     "hongkong", RegionLatency: map[string]int{"hongkong": 30},
			BehaviorScore: 95, CreatedAt: now,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := players.Create(ctx, player); err != nil {
			t.Fatal(err)
		}
		ticket := newPersistentTicket(t, fmt.Sprintf("ticket-%02d", index), playerID, roles[index%5], now)
		created, err := tickets.CreateQueued(ctx, ticket, fmt.Sprintf("create-%02d", index))
		if err != nil {
			t.Fatal(err)
		}
		ticketIDs = append(ticketIDs, created.ID())
	}

	duplicate := newPersistentTicket(t, "different-id", "player-00", roles[0], now)
	idempotent, err := tickets.CreateQueued(ctx, duplicate, "create-00")
	if err != nil || idempotent.ID() != "ticket-00" {
		t.Fatalf("idempotent create = %v, %v", idempotent, err)
	}
	reserved, err := tickets.ReserveAll(ctx, ticketIDs, "reservation-1", now.Add(time.Minute), now)
	if err != nil || len(reserved) != 10 {
		t.Fatalf("ReserveAll() = %d tickets, %v", len(reserved), err)
	}
	assigned, err := tickets.AssignAll(ctx, "reservation-1", "match-1", now.Add(time.Second))
	if err != nil || len(assigned) != 10 {
		t.Fatalf("AssignAll() = %d tickets, %v", len(assigned), err)
	}
	for _, ticket := range assigned {
		if ticket.State() != matchdomain.TicketStateAssigned || ticket.MatchID() != "match-1" {
			t.Fatalf("assigned ticket = state %s, match %s", ticket.State(), ticket.MatchID())
		}
	}
}

func newPersistentTicket(
	t *testing.T,
	ticketID, playerID string,
	role matchdomain.Role,
	now time.Time,
) *matchdomain.MatchTicket {
	t.Helper()
	ticket, err := matchdomain.NewTicket(matchdomain.NewTicketParams{
		ID: ticketID, PlayerID: playerID, Mode: "ranked_5v5",
		ClientVersion: "1.0.0", Region: "hongkong", Rating: 1500,
		PreferredRoles: []matchdomain.Role{role},
		RegionLatency:  map[string]int{"hongkong": 30}, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ticket.Queue(now); err != nil {
		t.Fatal(err)
	}
	return ticket
}
