package integration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
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
	matches := matchpostgres.NewMatchStore(pool)
	match := newPersistentMatch(t, now)
	if err := matches.Create(ctx, match); err != nil {
		t.Fatal(err)
	}
	if err := match.StartAllocation(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := matches.Update(ctx, match); err != nil {
		t.Fatal(err)
	}
	stale := match.Clone()
	if err := match.MarkReady("game:7001", "connection-token", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := matches.AssignReservedTickets(ctx, "reservation-1", match, now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, ticketID := range ticketIDs {
		ticket, err := tickets.Get(ctx, ticketID)
		if err != nil || ticket.State() != matchdomain.TicketStateAssigned || ticket.MatchID() != "match-1" {
			t.Fatalf("assigned ticket = %#v, %v", ticket, err)
		}
	}
	if err := stale.Fail(now.Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := matches.Update(ctx, stale); !errors.Is(err, application.ErrMatchRevisionConflict) {
		t.Fatalf("stale Match Update() error = %v", err)
	}
	restored, err := matches.Get(ctx, match.ID())
	if err != nil || restored.State() != matchdomain.MatchStateReady || restored.Revision() != 3 {
		t.Fatalf("restored Match = %#v, %v", restored, err)
	}
	if err := restored.Start(now.Add(4 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := matches.Update(ctx, restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Complete(matchdomain.MatchResult{
		WinningTeam: matchdomain.WinningTeamA, RandomSeed: 42, DurationSeconds: 900,
		ScoreA: 15, ScoreB: 10, MaxAdvantage: 1200, ActualQualityScore: 88,
	}, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := matches.CompleteMatchAndReleaseTickets(ctx, restored, now.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	history, err := matches.ListFinished(ctx, application.MatchHistoryFilter{
		PolicyVersion: "v1", Mode: "ranked_5v5", ServerRegion: "hongkong",
		From: now.Add(-time.Second), To: now.Add(time.Second), Limit: 10,
	})
	if err != nil || len(history) != 1 || history[0].ID() != restored.ID() {
		t.Fatalf("finished Match history = %#v, %v", history, err)
	}
	next := newPersistentTicket(t, "ticket-next", "player-00", roles[0], now.Add(6*time.Second))
	if _, err := tickets.CreateQueued(ctx, next, "create-next"); err != nil {
		t.Fatalf("create Ticket after Match completion: %v", err)
	}
}

func newPersistentMatch(t *testing.T, now time.Time) *matchdomain.Match {
	t.Helper()
	team := func(prefix string) matchdomain.MatchTeam {
		players := make([]matchdomain.MatchPlayer, 0, 5)
		roles := []matchdomain.Role{
			matchdomain.RoleVanguard, matchdomain.RoleRoamer, matchdomain.RoleCore,
			matchdomain.RoleRanged, matchdomain.RoleSupport,
		}
		for index := range 5 {
			players = append(players, matchdomain.MatchPlayer{
				PlayerID: fmt.Sprintf("player-%02d", index),
				TicketID: fmt.Sprintf("ticket-%02d", index),
				Role:     roles[index], Rating: 1500,
			})
			if prefix == "b" {
				players[index].PlayerID = fmt.Sprintf("player-%02d", index+5)
				players[index].TicketID = fmt.Sprintf("ticket-%02d", index+5)
			}
		}
		return matchdomain.MatchTeam{ID: "team-" + prefix, Players: players, AverageRating: 1500}
	}
	match, err := matchdomain.NewMatch(matchdomain.NewMatchParams{
		ID: "match-1", Mode: "ranked_5v5", TeamA: team("a"), TeamB: team("b"),
		ServerRegion: "hongkong", PolicyVersion: "v1", CreatedAt: now,
		Quality: matchdomain.MatchQuality{
			TotalScore: 90, SkillScore: 100, RoleScore: 100, LatencyScore: 80,
			PartyScore: 100, WaitScore: 90, PredictedWinRateA: .5, PredictedWinRateB: .5,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return match
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
