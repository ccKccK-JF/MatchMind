package integration

import (
	"context"
	"net"
	"testing"
	"time"

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	matchmakingapp "github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	playergateway "github.com/ccKccK-JF/MatchMind/internal/matchmaking/gateway/playergrpc"
	matchmakingmemory "github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/memory"
	matchmakinggrpc "github.com/ccKccK-JF/MatchMind/internal/matchmaking/transport/grpc"
	playerapp "github.com/ccKccK-JF/MatchMind/internal/player/application"
	playermemory "github.com/ccKccK-JF/MatchMind/internal/player/repository/memory"
	playergrpc "github.com/ccKccK-JF/MatchMind/internal/player/transport/grpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

func TestPlayerToMatchmakingGRPCFlow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	playerRepository := playermemory.NewRepository()
	playerApplication := playerapp.NewService(playerRepository, nil)
	playerConnection := startBufconnServer(t, func(server *grpc.Server) {
		playerv1.RegisterPlayerServiceServer(server, playergrpc.NewServer(playerApplication, nil))
	})
	playerClient := playerv1.NewPlayerServiceClient(playerConnection)
	_, err := playerClient.CreatePlayer(ctx, &playerv1.CreatePlayerRequest{
		Id:              "player-1001",
		Name:            "Nova",
		InitialRating:   1800,
		PreferredRoles:  []playerv1.Role{playerv1.Role_ROLE_CORE, playerv1.Role_ROLE_SUPPORT},
		HomeRegion:      "hongkong",
		RegionLatencyMs: map[string]int32{"hongkong": 32, "singapore": 48},
		BehaviorScore:   96,
	})
	if err != nil {
		t.Fatalf("CreatePlayer RPC error = %v", err)
	}

	ticketStore := matchmakingmemory.NewTicketStore()
	matchmakingApplication := matchmakingapp.NewTicketService(
		ticketStore,
		playergateway.NewClient(playerClient),
		func() (string, error) { return "ticket-1001", nil },
		func() time.Time { return time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC) },
	)
	matchmakingConnection := startBufconnServer(t, func(server *grpc.Server) {
		matchmakingv1.RegisterMatchmakingServiceServer(server, matchmakinggrpc.NewServer(matchmakingApplication, nil))
	})
	matchmakingClient := matchmakingv1.NewMatchmakingServiceClient(matchmakingConnection)
	created, err := matchmakingClient.CreateTicket(ctx, &matchmakingv1.CreateTicketRequest{
		PlayerId:       "player-1001",
		Mode:           "ranked_5v5",
		ClientVersion:  "1.0.0",
		PreferredRoles: []playerv1.Role{playerv1.Role_ROLE_CORE, playerv1.Role_ROLE_SUPPORT},
		RegionLatencyMs: map[string]int32{
			"hongkong":  32,
			"singapore": 48,
		},
		IdempotencyKey: "create-player-1001-001",
	})
	if err != nil {
		t.Fatalf("CreateTicket RPC error = %v", err)
	}
	if created.GetTicket().GetRating() != 1800 || created.GetTicket().GetRegion() != "hongkong" {
		t.Fatalf("created ticket = %+v", created.GetTicket())
	}

	cancelled, err := matchmakingClient.CancelTicket(ctx, &matchmakingv1.CancelTicketRequest{
		TicketId:       created.GetTicket().GetId(),
		PlayerId:       "player-1001",
		IdempotencyKey: "cancel-player-1001-001",
	})
	if err != nil {
		t.Fatalf("CancelTicket RPC error = %v", err)
	}
	if cancelled.GetTicket().GetState() != matchmakingv1.TicketState_TICKET_STATE_CANCELLED {
		t.Fatalf("cancelled state = %s", cancelled.GetTicket().GetState())
	}
}

func startBufconnServer(t *testing.T, register func(*grpc.Server)) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	register(server)
	errors := make(chan error, 1)
	go func() { errors <- server.Serve(listener) }()

	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
		server.Stop()
		if err := <-errors; err != nil && err != grpc.ErrServerStopped {
			t.Errorf("gRPC server error = %v", err)
		}
	})
	return connection
}
