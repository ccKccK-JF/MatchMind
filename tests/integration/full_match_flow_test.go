package integration

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	simulationv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/simulation/v1"
	matchmakingapp "github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	matchmakingplayergateway "github.com/ccKccK-JF/MatchMind/internal/matchmaking/gateway/playergrpc"
	matchmakingmemory "github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/memory"
	matchmakinggrpc "github.com/ccKccK-JF/MatchMind/internal/matchmaking/transport/grpc"
	playerapp "github.com/ccKccK-JF/MatchMind/internal/player/application"
	playermemory "github.com/ccKccK-JF/MatchMind/internal/player/repository/memory"
	playergrpc "github.com/ccKccK-JF/MatchMind/internal/player/transport/grpc"
	"github.com/ccKccK-JF/MatchMind/internal/rating/elo"
	simulationapp "github.com/ccKccK-JF/MatchMind/internal/simulation/application"
	simulationdomain "github.com/ccKccK-JF/MatchMind/internal/simulation/domain"
	simulationmatchgateway "github.com/ccKccK-JF/MatchMind/internal/simulation/gateway/matchmakinggrpc"
	simulationplayergateway "github.com/ccKccK-JF/MatchMind/internal/simulation/gateway/playergrpc"
	simulationmemory "github.com/ccKccK-JF/MatchMind/internal/simulation/repository/memory"
	simulationgrpc "github.com/ccKccK-JF/MatchMind/internal/simulation/transport/grpc"
	"google.golang.org/grpc"
)

func TestCompleteThreeServiceMatchFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	now := time.Date(2026, time.August, 1, 15, 0, 0, 0, time.UTC)

	playerRepository := playermemory.NewRepository()
	playerService := playerapp.NewService(playerRepository, func() time.Time { return now })
	calculator, err := elo.NewCalculator(32)
	if err != nil {
		t.Fatal(err)
	}
	ratingService := playerapp.NewRatingService(playerRepository, calculator, func() time.Time { return now })
	playerConnection := startBufconnServer(t, func(server *grpc.Server) {
		playerv1.RegisterPlayerServiceServer(server, playergrpc.NewServer(playerService, ratingService))
	})
	playerClient := playerv1.NewPlayerServiceClient(playerConnection)

	ticketStore := matchmakingmemory.NewTicketStore()
	matchStore := matchmakingmemory.NewMatchStore()
	ticketService := matchmakingapp.NewTicketService(
		ticketStore,
		matchmakingplayergateway.NewClient(playerClient),
		nil,
		func() time.Time { return now },
	)
	matchService := matchmakingapp.NewMatchService(matchStore, ticketStore, func() time.Time { return now })
	matchmakingConnection := startBufconnServer(t, func(server *grpc.Server) {
		matchmakingv1.RegisterMatchmakingServiceServer(server, matchmakinggrpc.NewServer(ticketService, matchService))
	})
	matchmakingClient := matchmakingv1.NewMatchmakingServiceClient(matchmakingConnection)

	roles := []playerv1.Role{
		playerv1.Role_ROLE_VANGUARD,
		playerv1.Role_ROLE_ROAMER,
		playerv1.Role_ROLE_CORE,
		playerv1.Role_ROLE_RANGED,
		playerv1.Role_ROLE_SUPPORT,
	}
	for index := range 10 {
		playerID := fmt.Sprintf("player-%02d", index)
		_, err := playerClient.CreatePlayer(ctx, &playerv1.CreatePlayerRequest{
			Id: playerID, Name: playerID, InitialRating: 1500 + float64(index%2)*10,
			PreferredRoles: []playerv1.Role{roles[index%5]}, HomeRegion: "hongkong",
			RegionLatencyMs: map[string]int32{"hongkong": 30}, BehaviorScore: 95,
		})
		if err != nil {
			t.Fatalf("CreatePlayer(%s) error = %v", playerID, err)
		}
		_, err = matchmakingClient.CreateTicket(ctx, &matchmakingv1.CreateTicketRequest{
			PlayerId: playerID, Mode: "ranked_5v5", ClientVersion: "1.0.0",
			PreferredRoles:  []playerv1.Role{roles[index%5]},
			RegionLatencyMs: map[string]int32{"hongkong": 30},
			IdempotencyKey:  fmt.Sprintf("create-%02d", index),
		})
		if err != nil {
			t.Fatalf("CreateTicket(%s) error = %v", playerID, err)
		}
	}

	workerIDs := []string{"reservation-1", "match-1"}
	workerIDIndex := 0
	worker, err := matchmakingapp.NewWorker(
		ticketStore,
		matchStore,
		matchmakingapp.NewLocalAllocator(func() (string, error) { return "connection-token", nil }),
		domain.DefaultPolicy(),
		func() (string, error) {
			id := workerIDs[workerIDIndex]
			workerIDIndex++
			return id, nil
		},
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	match, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatalf("match worker error = %v", err)
	}
	if match.State() != domain.MatchStateReady {
		t.Fatalf("match state = %s, want READY", match.State())
	}

	simulationService := simulationapp.NewService(
		simulationmatchgateway.NewClient(matchmakingClient),
		simulationplayergateway.NewClient(playerClient),
		simulationmemory.NewResultStore(),
		simulationdomain.NewSimulator(),
	)
	simulationConnection := startBufconnServer(t, func(server *grpc.Server) {
		simulationv1.RegisterSimulationServiceServer(server, simulationgrpc.NewServer(simulationService))
	})
	simulationClient := simulationv1.NewSimulationServiceClient(simulationConnection)
	firstResult, err := simulationClient.SimulateMatch(ctx, &simulationv1.SimulateMatchRequest{
		MatchId: match.ID(), RandomSeed: 42,
	})
	if err != nil {
		t.Fatalf("SimulateMatch() error = %v", err)
	}
	secondResult, err := simulationClient.SimulateMatch(ctx, &simulationv1.SimulateMatchRequest{
		MatchId: match.ID(), RandomSeed: 999,
	})
	if err != nil {
		t.Fatalf("idempotent SimulateMatch() error = %v", err)
	}
	if !reflect.DeepEqual(firstResult, secondResult) {
		t.Fatalf("duplicate simulation changed result:\n%+v\n%+v", firstResult, secondResult)
	}

	finished, err := matchmakingClient.GetMatch(ctx, &matchmakingv1.GetMatchRequest{MatchId: match.ID()})
	if err != nil {
		t.Fatal(err)
	}
	if finished.GetMatch().GetState() != matchmakingv1.MatchState_MATCH_STATE_FINISHED || finished.GetMatch().GetResult() == nil {
		t.Fatalf("finished match = %+v", finished.GetMatch())
	}
	firstPlayerID := finished.GetMatch().GetTeamA().GetPlayerIds()[0]
	history, err := playerClient.GetRatingHistory(ctx, &playerv1.GetRatingHistoryRequest{PlayerId: firstPlayerID})
	if err != nil {
		t.Fatal(err)
	}
	if len(history.GetChanges()) != 1 || history.GetChanges()[0].GetMatchId() != match.ID() {
		t.Fatalf("rating history = %+v", history.GetChanges())
	}
	_, err = matchmakingClient.CreateTicket(ctx, &matchmakingv1.CreateTicketRequest{
		PlayerId: firstPlayerID, Mode: "ranked_5v5", ClientVersion: "1.0.0",
		PreferredRoles:  []playerv1.Role{roles[0]},
		RegionLatencyMs: map[string]int32{"hongkong": 30},
		IdempotencyKey:  "create-after-finished-match",
	})
	if err != nil {
		t.Fatalf("CreateTicket() after finished Match error = %v", err)
	}
}
