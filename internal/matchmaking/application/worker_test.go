package application_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/memory"
)

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
				domain.DefaultPolicy(), idGenerator, func() time.Time { return now },
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
			RegionLatency:  map[string]int{"hongkong": 30},
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
