package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	"github.com/ccKccK-JF/MatchMind/internal/config"
	"github.com/ccKccK-JF/MatchMind/internal/platform/grpcserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/httpserver"
	"github.com/ccKccK-JF/MatchMind/internal/platform/logging"
	platformmetrics "github.com/ccKccK-JF/MatchMind/internal/platform/metrics"
	"github.com/ccKccK-JF/MatchMind/internal/player/application"
	"github.com/ccKccK-JF/MatchMind/internal/player/repository/memory"
	playerpostgres "github.com/ccKccK-JF/MatchMind/internal/player/repository/postgres"
	playergrpc "github.com/ccKccK-JF/MatchMind/internal/player/transport/grpc"
	"github.com/ccKccK-JF/MatchMind/internal/rating/elo"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
)

type playerRepository interface {
	application.Repository
	application.RatingRepository
}

func main() {
	logging.Configure()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var repository playerRepository
	switch backend := strings.ToLower(config.String("PLAYER_STORAGE_BACKEND", "memory")); backend {
	case "memory":
		repository = memory.NewRepository()
	case "postgres":
		connectContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		pool, err := pgxpool.New(connectContext, config.String(
			"POSTGRES_DSN",
			"postgres://matchmind:matchmind@localhost:5432/matchmind?sslmode=disable",
		))
		if err == nil {
			err = pool.Ping(connectContext)
		}
		cancel()
		if err != nil {
			slog.Error("connect player PostgreSQL repository", "error", err)
			os.Exit(1)
		}
		defer pool.Close()
		repository = playerpostgres.NewRepository(pool)
	default:
		slog.Error("unsupported player storage backend", "backend", backend)
		os.Exit(1)
	}
	service := application.NewService(repository, nil)
	kFactor, err := config.Float64("PLAYER_ELO_K_FACTOR", 32)
	if err != nil {
		slog.Error("invalid player service configuration", "error", err)
		os.Exit(1)
	}
	calculator, err := elo.NewCalculator(kFactor)
	if err != nil {
		slog.Error("invalid Elo K factor", "k_factor", kFactor, "error", err)
		os.Exit(1)
	}
	ratingService := application.NewRatingService(repository, calculator, nil)
	transport := playergrpc.NewServer(service, ratingService)

	address := config.String("PLAYER_GRPC_ADDRESS", ":50051")
	registry := platformmetrics.NewRegistry()
	errCh := make(chan error, 2)
	go func() {
		errCh <- grpcserver.Run(ctx, "matchmind.player.v1.PlayerService", address, func(server *grpc.Server) {
			playerv1.RegisterPlayerServiceServer(server, transport)
		})
	}()
	go func() {
		httpAddress := config.String("PLAYER_HTTP_ADDRESS", ":8081")
		errCh <- httpserver.Run(ctx, "matchmind-player-operations", httpAddress, httpserver.NewHandler(nil, registry, nil))
	}()
	err = <-errCh
	stop()
	if err != nil {
		slog.Error("player service stopped with error", "error", err)
		os.Exit(1)
	}
}
