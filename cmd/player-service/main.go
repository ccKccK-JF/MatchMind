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
	playergrpc "github.com/ccKccK-JF/MatchMind/internal/player/transport/grpc"
	"google.golang.org/grpc"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	address := config.String("PLAYER_GRPC_ADDRESS", ":50051")
	err := grpcserver.Run(ctx, "matchmind.player.v1.PlayerService", address, func(server *grpc.Server) {
		playerv1.RegisterPlayerServiceServer(server, playergrpc.NewServer())
	})
	if err != nil {
		slog.Error("player service stopped with error", "error", err)
		os.Exit(1)
	}
}
