package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	"github.com/ccKccK-JF/MatchMind/internal/config"
	"github.com/ccKccK-JF/MatchMind/internal/platform/grpcserver"
	"github.com/ccKccK-JF/MatchMind/internal/player/application"
	"github.com/ccKccK-JF/MatchMind/internal/player/repository/memory"
	playergrpc "github.com/ccKccK-JF/MatchMind/internal/player/transport/grpc"
	"github.com/ccKccK-JF/MatchMind/internal/rating/elo"
	"google.golang.org/grpc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	repository := memory.NewRepository()
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
	err = grpcserver.Run(ctx, "matchmind.player.v1.PlayerService", address, func(server *grpc.Server) {
		playerv1.RegisterPlayerServiceServer(server, transport)
	})
	if err != nil {
		slog.Error("player service stopped with error", "error", err)
		os.Exit(1)
	}
}
