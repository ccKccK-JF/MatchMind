package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/config"
	"github.com/ccKccK-JF/MatchMind/internal/platform/logging"
	"github.com/ccKccK-JF/MatchMind/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	logging.Configure()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, config.String("POSTGRES_DSN", "postgres://matchmind:matchmind@localhost:5432/matchmind?sslmode=disable"))
	if err != nil {
		slog.Error("create PostgreSQL pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		slog.Error("ping PostgreSQL", "error", err)
		os.Exit(1)
	}
	if err := migrations.Apply(ctx, pool); err != nil {
		slog.Error("apply PostgreSQL migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("PostgreSQL migrations applied")
}
