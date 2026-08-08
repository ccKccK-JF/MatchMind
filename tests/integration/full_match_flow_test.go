package integration

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	agentv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/agent/v1"
	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	simulationv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/simulation/v1"
	agentapp "github.com/ccKccK-JF/MatchMind/internal/agent/application"
	agentgateway "github.com/ccKccK-JF/MatchMind/internal/agent/gateway/matchmakinggrpc"
	agentmemory "github.com/ccKccK-JF/MatchMind/internal/agent/repository/memory"
	agentgrpc "github.com/ccKccK-JF/MatchMind/internal/agent/transport/grpc"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCompleteServiceMatchAndAgentAnalysisFlow(t *testing.T) {
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
	playerGateway := matchmakingplayergateway.NewClient(playerClient)
	ticketService := matchmakingapp.NewTicketService(
		ticketStore,
		playerGateway,
		nil,
		func() time.Time { return now },
	)
	matchService := matchmakingapp.NewMatchService(matchStore, ticketStore, func() time.Time { return now })
	analysisService, err := matchmakingapp.NewAnalysisService(
		matchStore, ticketStore, []domain.MatchPolicy{domain.DefaultPolicy(), domain.BeamPolicy()},
	)
	if err != nil {
		t.Fatal(err)
	}
	policyManager, err := matchmakingapp.NewPolicyManager(
		[]domain.MatchPolicy{domain.DefaultPolicy(), domain.BeamPolicy()}, domain.DefaultPolicy().Version,
	)
	if err != nil {
		t.Fatal(err)
	}
	operationsService, err := matchmakingapp.NewPolicyOperationsService(ticketStore, policyManager, "integration-agent-token", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	matchmakingTransport := matchmakinggrpc.NewServer(ticketService, matchService, analysisService)
	matchmakingTransport.SetPolicyOperations(operationsService)
	matchmakingConnection := startBufconnServer(t, func(server *grpc.Server) {
		matchmakingv1.RegisterMatchmakingServiceServer(server, matchmakingTransport)
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
		latency := map[string]int32{"hongkong": 30}
		if index == 0 {
			updated, updateErr := playerClient.UpdateRegionLatency(ctx, &playerv1.UpdateRegionLatencyRequest{
				PlayerId: playerID, RegionLatencyMs: map[string]int32{"hongkong": 25, "tokyo": 70},
			})
			if updateErr != nil || updated.GetPlayer().GetRegionLatencyMs()["hongkong"] != 25 {
				t.Fatalf("UpdateRegionLatency(%s) = %+v, %v", playerID, updated, updateErr)
			}
			latency = nil
		}
		_, err = matchmakingClient.CreateTicket(ctx, &matchmakingv1.CreateTicketRequest{
			PlayerId: playerID, Mode: "ranked_5v5", ClientVersion: "1.0.0",
			PreferredRoles:  []playerv1.Role{roles[index%5]},
			RegionLatencyMs: latency,
			IdempotencyKey:  fmt.Sprintf("create-%02d", index),
		})
		if err != nil {
			t.Fatalf("CreateTicket(%s) error = %v", playerID, err)
		}
	}
	activeBeforeMatch, err := matchmakingClient.GetActiveTicketForPlayer(ctx, &matchmakingv1.GetActiveTicketForPlayerRequest{PlayerId: "player-00"})
	if err != nil || !activeBeforeMatch.GetFound() || activeBeforeMatch.GetTicket().GetRegionLatencyMs()["hongkong"] != 25 {
		t.Fatalf("active Ticket before Match = %+v, %v", activeBeforeMatch, err)
	}

	workerIDs := []string{"reservation-1", "match-1"}
	workerIDIndex := 0
	worker, err := matchmakingapp.NewWorker(
		ticketStore,
		matchStore,
		matchmakingapp.NewLocalAllocator(func() (string, error) { return "connection-token", nil }),
		playerGateway,
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
	_, err = playerClient.SetPlayerBan(ctx, &playerv1.SetPlayerBanRequest{
		PlayerId: "player-00", Banned: true, Reason: "integration moderation", OperatorId: "admin-1",
	})
	if err != nil {
		t.Fatalf("SetPlayerBan() error = %v", err)
	}
	_, err = matchmakingClient.CreateTicket(ctx, &matchmakingv1.CreateTicketRequest{
		PlayerId: "player-00", Mode: "ranked_5v5", ClientVersion: "1.0.0",
		PreferredRoles: []playerv1.Role{playerv1.Role_ROLE_VANGUARD}, IdempotencyKey: "banned-retry-00",
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("banned CreateTicket() code = %v, want PermissionDenied", status.Code(err))
	}
	if _, err := worker.RunOnce(ctx); !errors.Is(err, matchmakingapp.ErrNoMatchAvailable) {
		t.Fatalf("worker matched a newly banned queued player: %v", err)
	}
	cancelled, err := matchmakingClient.GetTicket(ctx, &matchmakingv1.GetTicketRequest{TicketId: activeBeforeMatch.GetTicket().GetId()})
	if err != nil || cancelled.GetTicket().GetState() != matchmakingv1.TicketState_TICKET_STATE_CANCELLED {
		t.Fatalf("banned player's Ticket = %+v, %v", cancelled.GetTicket(), err)
	}
	_, err = playerClient.SetPlayerBan(ctx, &playerv1.SetPlayerBanRequest{
		PlayerId: "player-00", Banned: false, OperatorId: "admin-1",
	})
	if err != nil {
		t.Fatalf("unban player error = %v", err)
	}
	_, err = matchmakingClient.CreateTicket(ctx, &matchmakingv1.CreateTicketRequest{
		PlayerId: "player-00", Mode: "ranked_5v5", ClientVersion: "1.0.0",
		PreferredRoles: []playerv1.Role{playerv1.Role_ROLE_VANGUARD}, IdempotencyKey: "requeue-00",
	})
	if err != nil {
		t.Fatalf("requeue unbanned player error = %v", err)
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
	activeAfterMatch, err := matchmakingClient.GetActiveTicketForPlayer(ctx, &matchmakingv1.GetActiveTicketForPlayerRequest{PlayerId: firstPlayerID})
	if err != nil || activeAfterMatch.GetFound() {
		t.Fatalf("active Ticket after finished Match = %+v, %v", activeAfterMatch, err)
	}
	qualityAnalysis, err := matchmakingClient.AnalyzeMatchQuality(ctx, &matchmakingv1.AnalyzeMatchQualityRequest{
		Mode: "ranked_5v5", ServerRegion: "hongkong", Limit: 10,
	})
	if err != nil {
		t.Fatalf("AnalyzeMatchQuality() error = %v", err)
	}
	if len(qualityAnalysis.GetObservations()) != 1 || len(qualityAnalysis.GetSummaries()) != 1 ||
		qualityAnalysis.GetObservations()[0].GetActualQuality() != firstResult.GetActualQualityScore() {
		t.Fatalf("quality analysis = %+v", qualityAnalysis)
	}
	replay, err := matchmakingClient.ReplayHistoricalMatch(ctx, &matchmakingv1.ReplayHistoricalMatchRequest{
		MatchId: match.ID(), PolicyVersions: []string{"v1-greedy", "v2-beam"},
	})
	if err != nil {
		t.Fatalf("ReplayHistoricalMatch() error = %v", err)
	}
	if replay.GetTicketCount() != 10 || len(replay.GetOutcomes()) != 2 ||
		!replay.GetOutcomes()[0].GetMatched() || !replay.GetOutcomes()[1].GetMatched() {
		t.Fatalf("historical replay = %+v", replay)
	}
	agentService, err := agentapp.NewService(
		agentmemory.NewRepository(),
		agentgateway.NewClient(matchmakingClient, "integration-agent-token"),
		"integration-advisor", "rules-v1", "prompt-v1", "v1-greedy", nil,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatal(err)
	}
	agentConnection := startBufconnServer(t, func(server *grpc.Server) {
		agentv1.RegisterAgentServiceServer(server, agentgrpc.NewServer(agentService))
	})
	agentClient := agentv1.NewAgentServiceClient(agentConnection)
	agentResult, err := agentClient.RunAnalysis(ctx, &agentv1.RunAnalysisRequest{
		RequestedBy: "integration-analyst", BasePolicyVersion: "v1-greedy",
		Mode: "ranked_5v5", ServerRegion: "hongkong", HistoricalLimit: 10,
	})
	if err != nil {
		t.Fatalf("Agent RunAnalysis() error = %v", err)
	}
	if agentResult.GetRun().GetStatus() != agentv1.RunStatus_RUN_STATUS_SUCCEEDED ||
		len(agentResult.GetRun().GetToolCalls()) != 3 || agentResult.GetProposal().GetRiskReport().GetPassed() {
		t.Fatalf("Agent result = %+v", agentResult)
	}
	audited, err := agentClient.GetRun(ctx, &agentv1.GetRunRequest{RunId: agentResult.GetRun().GetId()})
	if err != nil || audited.GetRun().GetOutputJson() == "" {
		t.Fatalf("Agent audit = %+v, %v", audited, err)
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
