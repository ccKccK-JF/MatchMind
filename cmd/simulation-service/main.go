package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	simulationv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/simulation/v1"
	"github.com/ccKccK-JF/MatchMind/internal/config"
	"github.com/ccKccK-JF/MatchMind/internal/platform/grpcserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/httpserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/logging"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
	"github.com/ccKccK-JF/MatchMind/internal/simulation/application"
	simulationdomain "github.com/ccKccK-JF/MatchMind/internal/simulation/domain"
	matchmakinggateway "github.com/ccKccK-JF/MatchMind/internal/simulation/gateway/matchmakinggrpc"
	playergateway "github.com/ccKccK-JF/MatchMind/internal/simulation/gateway/playergrpc"
	"github.com/ccKccK-JF/MatchMind/internal/simulation/repository/memory"
	simulationgrpc "github.com/ccKccK-JF/MatchMind/internal/simulation/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	logging.Configure()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	matchmakingConnection, err := grpc.NewClient(
		config.String("MATCHMAKING_GRPC_TARGET", "localhost:50052"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		slog.Error("create matchmaking service client", "error", err)
		os.Exit(1)
	}
	defer matchmakingConnection.Close()
	playerConnection, err := grpc.NewClient(
		config.String("PLAYER_GRPC_TARGET", "localhost:50051"),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		slog.Error("create player service client", "error", err)
		os.Exit(1)
	}
	defer playerConnection.Close()

	service := application.NewService(
		matchmakinggateway.NewClient(matchmakingv1.NewMatchmakingServiceClient(matchmakingConnection)),
		playergateway.NewClient(playerv1.NewPlayerServiceClient(playerConnection)),
		memory.NewResultStore(),
		simulationdomain.NewSimulator(),
	)
	transport := simulationgrpc.NewServer(service)

	address := config.String("SIMULATION_GRPC_ADDRESS", ":50053")
	registry := platformmetrics.NewRegistry()
	errCh := make(chan error, 2)
	go func() {
		errCh <- grpcserver.Run(ctx, "matchmind.simulation.v1.SimulationService", address, func(server *grpc.Server) {
			simulationv1.RegisterSimulationServiceServer(server, transport)
		})
	}()
	go func() {
		httpAddress := config.String("SIMULATION_HTTP_ADDRESS", ":8083")
		errCh <- httpserver.Run(ctx, "matchmind-simulation-operations", httpAddress, httpserver.NewHandler(nil, registry, nil))
	}()
	err = <-errCh
	stop()
	if err != nil {
		slog.Error("simulation service stopped with error", "error", err)
		os.Exit(1)
	}
}
