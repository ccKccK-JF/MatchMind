package playergrpc

import (
	"context"
	"net"
	"testing"
	"time"

	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	"github.com/ccKccK-JF/MatchMind/internal/player/application"
	"github.com/ccKccK-JF/MatchMind/internal/player/repository/memory"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestPlayerServiceGRPCFlow(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	grpcServer := grpc.NewServer()
	playerv1.RegisterPlayerServiceServer(
		grpcServer,
		NewServer(application.NewService(memory.NewRepository(), nil), nil),
	)

	serveErrors := make(chan error, 1)
	go func() {
		serveErrors <- grpcServer.Serve(listener)
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		if err := <-serveErrors; err != nil && err != grpc.ErrServerStopped {
			t.Errorf("gRPC server stopped with error: %v", err)
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	client := playerv1.NewPlayerServiceClient(connection)
	created, err := client.CreatePlayer(ctx, &playerv1.CreatePlayerRequest{
		Id:             "player-1001",
		Name:           "Nova",
		InitialRating:  1800,
		PreferredRoles: []playerv1.Role{playerv1.Role_ROLE_CORE, playerv1.Role_ROLE_SUPPORT},
		HomeRegion:     "hongkong",
		RegionLatencyMs: map[string]int32{
			"hongkong":  32,
			"singapore": 48,
		},
		BehaviorScore: 96,
	})
	if err != nil {
		t.Fatalf("CreatePlayer RPC error = %v", err)
	}

	got, err := client.GetPlayer(ctx, &playerv1.GetPlayerRequest{PlayerId: created.GetPlayer().GetId()})
	if err != nil {
		t.Fatalf("GetPlayer RPC error = %v", err)
	}
	if got.GetPlayer().GetName() != "Nova" || got.GetPlayer().GetRating() != 1800 {
		t.Fatalf("GetPlayer RPC returned unexpected player: %+v", got.GetPlayer())
	}
}
