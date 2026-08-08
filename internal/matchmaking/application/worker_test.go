package application_test

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
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/memory"
)

type eligibilityFunc func([]string) (map[string]bool, error)

func (f eligibilityFunc) CheckPlayersEligibility(_ context.Context, playerIDs []string) (map[string]bool, error) {
	return f(playerIDs)
}

var allPlayersEligible = eligibilityFunc(func(playerIDs []string) (map[string]bool, error) {
	result := make(map[string]bool, len(playerIDs))
	for _, playerID := range playerIDs {
		result[playerID] = true
	}
	return result, nil
})

func TestWorkerCreatesReadyMatchAndAssignsAllTickets(t *testing.T) {
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	ticketStore := memory.NewTicketStore()
	ticketIDs := populateMatchableTickets(t, ticketStore, now)
	matchStore := memory.NewMatchStore()
	ids := []string{"reservation-1", "match-1"}
	index := 0
	worker, err := application.NewWorker(
		ticketStore,
		matchStore,
		application.NewLocalAllocator(func() (string, error) { return "connection-token", nil }),
		allPlayersEligible,
		domain.DefaultPolicy(),
		func() (string, error) {
			id := ids[index]
			index++
			return id, nil
		},
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := application.NewPolicyManager(
		[]domain.MatchPolicy{domain.DefaultPolicy(), domain.BeamPolicy()}, domain.DefaultPolicy().Version,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.StartExperiment(application.PolicyExperiment{
		ID: "beam-test", ControlVersion: domain.DefaultPolicy().Version,
		TreatmentVersion: domain.BeamPolicy().Version, TreatmentBasisPoints: 10000,
		AssignmentSalt: "test", StartedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	worker.SetPolicySelector(manager)

	match, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if match.State() != domain.MatchStateReady || match.ServerAddress() == "" || match.ConnectionToken() == "" {
		t.Fatalf("match state/address/token = %s/%q/%q", match.State(), match.ServerAddress(), match.ConnectionToken())
	}
	if match.PolicyVersion() != domain.BeamPolicy().Version {
		t.Fatalf("match policy version = %s, want %s", match.PolicyVersion(), domain.BeamPolicy().Version)
	}
	if len(match.TeamA().Players) != 5 || len(match.TeamB().Players) != 5 {
		t.Fatalf("team sizes = %d/%d", len(match.TeamA().Players), len(match.TeamB().Players))
	}
	for _, ticketID := range ticketIDs {
		ticket, err := ticketStore.Get(context.Background(), ticketID)
		if err != nil {
			t.Fatal(err)
		}
		if ticket.State() != domain.TicketStateAssigned || ticket.MatchID() != match.ID() {
			t.Fatalf("ticket %s state/match = %s/%s", ticketID, ticket.State(), ticket.MatchID())
		}
	}
	stored, err := matchStore.Get(context.Background(), match.ID())
	if err != nil || stored.State() != domain.MatchStateReady {
		t.Fatalf("stored match = %v, %v", stored, err)
	}
}

func TestConcurrentWorkersCannotDuplicatePlayers(t *testing.T) {
	now := time.Now()
	ticketStore := memory.NewTicketStore()
	populateMatchableTickets(t, ticketStore, now)
	matchStore := memory.NewMatchStore()
	var idCounter atomic.Int64
	idGenerator := func() (string, error) {
		return fmt.Sprintf("id-%d", idCounter.Add(1)), nil
	}

	const workerCount = 10
	var successes atomic.Int32
	var waitGroup sync.WaitGroup
	waitGroup.Add(workerCount)
	for range workerCount {
		go func() {
			defer waitGroup.Done()
			worker, err := application.NewWorker(
				ticketStore, matchStore, application.NewLocalAllocator(idGenerator),
				allPlayersEligible, domain.DefaultPolicy(), idGenerator, func() time.Time { return now },
			)
			if err != nil {
				t.Errorf("NewWorker() error = %v", err)
				return
			}
			if _, err := worker.RunOnce(context.Background()); err == nil {
				successes.Add(1)
			}
		}()
	}
	waitGroup.Wait()
	if successes.Load() != 1 {
		t.Fatalf("successful workers = %d, want 1", successes.Load())
	}
}

func TestWorkerSelectsAnotherRegionWhenPoolRegionHasNoCapacity(t *testing.T) {
	now := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	ticketStore := memory.NewTicketStore()
	populateMatchableTickets(t, ticketStore, now)
	matchStore := memory.NewMatchStore()
	allocator, err := application.NewLocalAllocatorWithCapacities(map[string]int{
		"hongkong": 0,
		"tokyo":    2,
	}, func() (string, error) { return "token", nil })
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"reservation-1", "match-1"}
	worker, err := application.NewWorker(
		ticketStore, matchStore, allocator, allPlayersEligible, domain.DefaultPolicy(),
		func() (string, error) {
			id := ids[0]
			ids = ids[1:]
			return id, nil
		},
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	match, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if match.ServerRegion() != "tokyo" || match.ServerAddress() != "tokyo.game.matchmind.local:7000" {
		t.Fatalf("server region/address = %s/%s", match.ServerRegion(), match.ServerAddress())
	}
}

func TestWorkerRechecksEligibilityBeforeReservationAndCancelsNewlyBannedTicket(t *testing.T) {
	now := time.Date(2026, time.August, 8, 16, 0, 0, 0, time.UTC)
	ticketStore := memory.NewTicketStore()
	populateMatchableTickets(t, ticketStore, now)
	var checks atomic.Int32
	eligibility := eligibilityFunc(func(playerIDs []string) (map[string]bool, error) {
		result := make(map[string]bool, len(playerIDs))
		call := checks.Add(1)
		for _, playerID := range playerIDs {
			result[playerID] = call == 1 || playerID != "player-00"
		}
		return result, nil
	})
	ids := []string{"reservation-1", "match-1"}
	worker, err := application.NewWorker(
		ticketStore, memory.NewMatchStore(),
		application.NewLocalAllocator(func() (string, error) { return "token", nil }),
		eligibility, domain.DefaultPolicy(), func() (string, error) {
			id := ids[0]
			ids = ids[1:]
			return id, nil
		}, func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worker.RunOnce(context.Background()); !errors.Is(err, application.ErrNoMatchAvailable) {
		t.Fatalf("RunOnce() error = %v, want ErrNoMatchAvailable", err)
	}
	if checks.Load() != 2 {
		t.Fatalf("eligibility checks = %d, want initial and pre-reservation checks", checks.Load())
	}
	bannedTicket, err := ticketStore.Get(context.Background(), "ticket-00")
	if err != nil {
		t.Fatal(err)
	}
	if bannedTicket.State() != domain.TicketStateCancelled {
		t.Fatalf("banned ticket state = %s, want CANCELLED", bannedTicket.State())
	}
	for index := 1; index < 10; index++ {
		ticket, err := ticketStore.Get(context.Background(), fmt.Sprintf("ticket-%02d", index))
		if err != nil || ticket.State() != domain.TicketStateQueued {
			t.Fatalf("eligible ticket %02d = %v, %v", index, ticket, err)
		}
	}
}

func populateMatchableTickets(t *testing.T, store *memory.TicketStore, now time.Time) []string {
	t.Helper()
	roles := []domain.Role{domain.RoleVanguard, domain.RoleRoamer, domain.RoleCore, domain.RoleRanged, domain.RoleSupport}
	ticketIDs := make([]string, 0, 10)
	for index := range 10 {
		ticketID := fmt.Sprintf("ticket-%02d", index)
		ticket, err := domain.NewTicket(domain.NewTicketParams{
			ID:             ticketID,
			PlayerID:       fmt.Sprintf("player-%02d", index),
			Mode:           "ranked_5v5",
			ClientVersion:  "1.0.0",
			Region:         "hongkong",
			Rating:         1500 + float64(index%2)*10,
			PreferredRoles: []domain.Role{roles[index%5]},
			RegionLatency:  map[string]int{"hongkong": 30, "tokyo": 50, "singapore": 80},
			CreatedAt:      now.Add(time.Duration(index) * time.Millisecond),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := ticket.Queue(now); err != nil {
			t.Fatal(err)
		}
		if _, err := store.CreateQueued(context.Background(), ticket, fmt.Sprintf("create-%02d", index)); err != nil {
			t.Fatal(err)
		}
		ticketIDs = append(ticketIDs, ticketID)
	}
	return ticketIDs
}
