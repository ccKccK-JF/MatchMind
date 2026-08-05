package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	"github.com/ccKccK-JF/MatchMind/internal/config"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	playergateway "github.com/ccKccK-JF/MatchMind/internal/matchmaking/gateway/playergrpc"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/observability"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/memory"
	matchmakinggrpc "github.com/ccKccK-JF/MatchMind/internal/matchmaking/transport/grpc"
	"github.com/ccKccK-JF/MatchMind/internal/platform/grpcserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/httpserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/logging"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	logging.Configure()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	playerTarget := config.String("PLAYER_GRPC_TARGET", "localhost:50051")
	playerConnection, err := grpc.NewClient(playerTarget, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		slog.Error("create player service client", "error", err)
		os.Exit(1)
	}
	defer playerConnection.Close()

	store := memory.NewTicketStore()
	matchStore := memory.NewMatchStore()
	players := playergateway.NewClient(playerv1.NewPlayerServiceClient(playerConnection))
	service := application.NewTicketService(store, players, nil, nil)
	matchService := application.NewMatchService(matchStore, nil)
	policy := domain.DefaultPolicy()
	workerCount, err := config.Int("MATCHMAKING_WORKER_COUNT", 1)
	if err != nil {
		slog.Error("invalid matchmaking worker count", "error", err)
		os.Exit(1)
	}
	if workerCount < 0 {
		slog.Error("matchmaking worker count cannot be negative", "worker_count", workerCount)
		os.Exit(1)
	}
	registry := platformmetrics.NewRegistry()
	workerMetrics := observability.NewMatchmakingMetrics(registry)
	for workerIndex := range workerCount {
		worker, workerErr := application.NewWorker(
			store, matchStore, application.NewLocalAllocator(nil), policy, nil, nil,
		)
		if workerErr != nil {
			slog.Error("create matchmaking worker", "worker_index", workerIndex, "error", workerErr)
			os.Exit(1)
		}
		worker.SetMetrics(workerMetrics)
		go worker.Run(ctx, 250*time.Millisecond)
	}
	transport := matchmakinggrpc.NewServer(service, matchService)

	address := config.String("MATCHMAKING_GRPC_ADDRESS", ":50052")
	errCh := make(chan error, 2)
	go func() {
		errCh <- grpcserver.Run(ctx, "matchmind.matchmaking.v1.MatchmakingService", address, func(server *grpc.Server) {
			matchmakingv1.RegisterMatchmakingServiceServer(server, transport)
		})
	}()
	go func() {
		httpAddress := config.String("MATCHMAKING_HTTP_ADDRESS", ":8082")
		errCh <- httpserver.Run(ctx, "matchmind-matchmaking-operations", httpAddress, httpserver.NewHandler(nil, registry, nil))
	}()
	err = <-errCh
	stop()
	if err != nil {
		slog.Error("matchmaking service stopped with error", "error", err)
		os.Exit(1)
	}
}
