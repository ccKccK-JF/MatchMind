package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
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
	matchpostgres "github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/postgres"
	matchmakinggrpc "github.com/ccKccK-JF/MatchMind/internal/matchmaking/transport/grpc"
	"github.com/ccKccK-JF/MatchMind/internal/platform/grpcserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/httpserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/logging"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type ticketRepository interface {
	application.TicketStore
	application.MatchQueue
	application.AssignedTicketCompleter
}

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

	ticketBackend := strings.ToLower(config.String("MATCHMAKING_TICKET_STORAGE_BACKEND", "memory"))
	matchBackend := strings.ToLower(config.String("MATCHMAKING_MATCH_STORAGE_BACKEND", ticketBackend))
	var postgresPool *pgxpool.Pool
	if ticketBackend == "postgres" || matchBackend == "postgres" {
		connectContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		postgresPool, err = pgxpool.New(connectContext, config.String(
			"POSTGRES_DSN",
			"postgres://matchmind:matchmind@localhost:5432/matchmind?sslmode=disable",
		))
		if err == nil {
			err = postgresPool.Ping(connectContext)
		}
		cancel()
		if err != nil {
			slog.Error("connect matchmaking PostgreSQL repositories", "error", err)
			os.Exit(1)
		}
		defer postgresPool.Close()
	}

	var store ticketRepository
	switch ticketBackend {
	case "memory":
		store = memory.NewTicketStore()
	case "postgres":
		store = matchpostgres.NewTicketStore(postgresPool)
	default:
		slog.Error("unsupported matchmaking Ticket storage backend", "backend", ticketBackend)
		os.Exit(1)
	}
	var matchStore application.MatchRepository
	switch matchBackend {
	case "memory":
		matchStore = memory.NewMatchStore()
	case "postgres":
		matchStore = matchpostgres.NewMatchStore(postgresPool)
	default:
		slog.Error("unsupported matchmaking Match storage backend", "backend", matchBackend)
		os.Exit(1)
	}
	players := playergateway.NewClient(playerv1.NewPlayerServiceClient(playerConnection))
	service := application.NewTicketService(store, players, nil, nil)
	matchService := application.NewMatchService(matchStore, store, nil)
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
