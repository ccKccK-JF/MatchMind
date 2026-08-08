package playergrpc

import (
	"context"
	"errors"
	"testing"

	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakePlayerClient struct {
	playerv1.PlayerServiceClient
	get   func(context.Context, *playerv1.GetPlayerRequest) (*playerv1.GetPlayerResponse, error)
	check func(context.Context, *playerv1.CheckPlayersEligibilityRequest) (*playerv1.CheckPlayersEligibilityResponse, error)
}

func (f fakePlayerClient) GetPlayer(ctx context.Context, request *playerv1.GetPlayerRequest, _ ...grpc.CallOption) (*playerv1.GetPlayerResponse, error) {
	return f.get(ctx, request)
}

func (f fakePlayerClient) CheckPlayersEligibility(ctx context.Context, request *playerv1.CheckPlayersEligibilityRequest, _ ...grpc.CallOption) (*playerv1.CheckPlayersEligibilityResponse, error) {
	return f.check(ctx, request)
}

func TestClientMapsBanStateAndBatchEligibility(t *testing.T) {
	client := NewClient(fakePlayerClient{
		get: func(context.Context, *playerv1.GetPlayerRequest) (*playerv1.GetPlayerResponse, error) {
			return &playerv1.GetPlayerResponse{Player: &playerv1.Player{
				Id: "player-1", Rating: 1500, Banned: true, BehaviorScore: 97,
				HeroProficiency: map[string]float64{"starblade": 91},
			}}, nil
		},
		check: func(_ context.Context, request *playerv1.CheckPlayersEligibilityRequest) (*playerv1.CheckPlayersEligibilityResponse, error) {
			if len(request.GetPlayerIds()) != 3 {
				t.Fatalf("player IDs = %#v", request.GetPlayerIds())
			}
			return &playerv1.CheckPlayersEligibilityResponse{Players: []*playerv1.PlayerEligibility{
				{PlayerId: "player-1", Exists: true, Banned: true},
				{PlayerId: "player-2", Exists: true},
				{PlayerId: "missing", Exists: false},
			}}, nil
		},
	})
	player, err := client.GetPlayer(context.Background(), "player-1")
	if err != nil || !player.Banned || player.BehaviorScore != 97 || player.HeroProficiency["starblade"] != 91 {
		t.Fatalf("GetPlayer() = %+v, %v", player, err)
	}
	player.HeroProficiency["starblade"] = 0
	refetched, err := client.GetPlayer(context.Background(), "player-1")
	if err != nil || refetched.HeroProficiency["starblade"] != 91 {
		t.Fatalf("GetPlayer() returned shared hero map: %+v, %v", refetched, err)
	}
	eligible, err := client.CheckPlayersEligibility(context.Background(), []string{"player-1", "player-2", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if eligible["player-1"] || !eligible["player-2"] || eligible["missing"] {
		t.Fatalf("eligibility = %#v", eligible)
	}
}

func TestClientFailsClosedWhenEligibilityServiceFails(t *testing.T) {
	client := NewClient(fakePlayerClient{check: func(context.Context, *playerv1.CheckPlayersEligibilityRequest) (*playerv1.CheckPlayersEligibilityResponse, error) {
		return nil, status.Error(codes.Unavailable, "offline")
	}})
	_, err := client.CheckPlayersEligibility(context.Background(), []string{"player-1"})
	if !errors.Is(err, application.ErrPlayerServiceUnavailable) {
		t.Fatalf("CheckPlayersEligibility() error = %v", err)
	}
}
