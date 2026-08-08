package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	agentv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/agent/v1"
	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	simulationv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/simulation/v1"
	apihttp "github.com/ccKccK-JF/MatchMind/internal/api/transport/http"
	"github.com/ccKccK-JF/MatchMind/internal/config"
	"github.com/ccKccK-JF/MatchMind/internal/platform/httpserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/logging"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type downstream struct {
	name   string
	health grpc_health_v1.HealthClient
}

func main() {
	logging.Configure()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	playerConnection := mustConnect(config.String("PLAYER_GRPC_TARGET", "localhost:50051"))
	defer playerConnection.Close()
	matchmakingConnection := mustConnect(config.String("MATCHMAKING_GRPC_TARGET", "localhost:50052"))
	defer matchmakingConnection.Close()
	simulationConnection := mustConnect(config.String("SIMULATION_GRPC_TARGET", "localhost:50053"))
	defer simulationConnection.Close()
	agentConnection := mustConnect(config.String("AGENT_GRPC_TARGET", "localhost:50054"))
	defer agentConnection.Close()

	registry := platformmetrics.NewRegistry()
	api := apihttp.NewServer(
		playerv1.NewPlayerServiceClient(playerConnection),
		matchmakingv1.NewMatchmakingServiceClient(matchmakingConnection),
		simulationv1.NewSimulationServiceClient(simulationConnection),
		apihttp.NewAPIMetrics(registry),
		agentv1.NewAgentServiceClient(agentConnection),
	)
	downstreams := []downstream{
		{name: "matchmind.player.v1.PlayerService", health: grpc_health_v1.NewHealthClient(playerConnection)},
		{name: "matchmind.matchmaking.v1.MatchmakingService", health: grpc_health_v1.NewHealthClient(matchmakingConnection)},
		{name: "matchmind.simulation.v1.SimulationService", health: grpc_health_v1.NewHealthClient(simulationConnection)},
		{name: "matchmind.agent.v1.AgentService", health: grpc_health_v1.NewHealthClient(agentConnection)},
	}
	handler := httpserver.NewHandler(api, registry, func(ctx context.Context) error {
		for _, service := range downstreams {
			result, err := service.health.Check(ctx, &grpc_health_v1.HealthCheckRequest{Service: service.name})
			if err != nil {
				return fmt.Errorf("%s health check: %w", service.name, err)
			}
			if result.Status != grpc_health_v1.HealthCheckResponse_SERVING {
				return fmt.Errorf("%s is %s", service.name, result.Status)
			}
		}
		return nil
	})

	address := config.String("API_HTTP_ADDRESS", ":8080")
	if err := httpserver.Run(ctx, "matchmind-api", address, handler); err != nil {
		slog.Error("API service stopped with error", "error", err)
		os.Exit(1)
	}
}

func mustConnect(target string) *grpc.ClientConn {
	connection, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("create downstream gRPC client", "target", target, "error", err)
		os.Exit(1)
	}
	return connection
}
