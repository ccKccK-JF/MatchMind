package playergrpc

import (
	"context"
	"fmt"
	"testing"
	"time"

	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	"github.com/ccKccK-JF/MatchMind/internal/player/application"
	"github.com/ccKccK-JF/MatchMind/internal/player/repository/memory"
	"github.com/ccKccK-JF/MatchMind/internal/rating/elo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestServerCreateAndGetPlayer(t *testing.T) {
	createdAt := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	server := NewServer(application.NewService(memory.NewRepository(), func() time.Time { return createdAt }), nil)
	request := &playerv1.CreatePlayerRequest{
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
	}

	created, err := server.CreatePlayer(context.Background(), request)
	if err != nil {
		t.Fatalf("CreatePlayer() error = %v", err)
	}
	if created.GetPlayer().GetId() != request.GetId() {
		t.Fatalf("created player id = %q, want %q", created.GetPlayer().GetId(), request.GetId())
	}
	if !created.GetPlayer().GetCreatedAt().AsTime().Equal(createdAt) {
		t.Fatalf("created time = %v, want %v", created.GetPlayer().GetCreatedAt().AsTime(), createdAt)
	}
	if created.GetPlayer().GetRatingVolatility() != 0.06 {
		t.Fatalf("rating volatility = %v, want 0.06", created.GetPlayer().GetRatingVolatility())
	}

	got, err := server.GetPlayer(context.Background(), &playerv1.GetPlayerRequest{PlayerId: request.GetId()})
	if err != nil {
		t.Fatalf("GetPlayer() error = %v", err)
	}
	if got.GetPlayer().GetName() != request.GetName() {
		t.Fatalf("player name = %q, want %q", got.GetPlayer().GetName(), request.GetName())
	}

	_, err = server.CreatePlayer(context.Background(), request)
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("duplicate CreatePlayer() code = %v, want AlreadyExists", status.Code(err))
	}
}

func TestServerValidationAndNotFoundCodes(t *testing.T) {
	server := NewServer(application.NewService(memory.NewRepository(), nil), nil)

	_, err := server.CreatePlayer(context.Background(), &playerv1.CreatePlayerRequest{
		Id:              "player-1001",
		Name:            "Nova",
		InitialRating:   1500,
		PreferredRoles:  []playerv1.Role{playerv1.Role_ROLE_UNSPECIFIED},
		HomeRegion:      "hongkong",
		RegionLatencyMs: map[string]int32{"hongkong": 30},
		BehaviorScore:   90,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid CreatePlayer() code = %v, want InvalidArgument", status.Code(err))
	}

	_, err = server.GetPlayer(context.Background(), &playerv1.GetPlayerRequest{PlayerId: "missing"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("missing GetPlayer() code = %v, want NotFound", status.Code(err))
	}
}

func TestServerUpdatesRegionLatency(t *testing.T) {
	service := application.NewService(memory.NewRepository(), nil)
	server := NewServer(service, nil)
	_, err := server.CreatePlayer(context.Background(), &playerv1.CreatePlayerRequest{
		Id: "player-1", Name: "Nova", InitialRating: 1500,
		PreferredRoles: []playerv1.Role{playerv1.Role_ROLE_CORE}, HomeRegion: "hongkong",
		RegionLatencyMs: map[string]int32{"hongkong": 30}, BehaviorScore: 95,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := server.UpdateRegionLatency(context.Background(), &playerv1.UpdateRegionLatencyRequest{
		PlayerId: "player-1", RegionLatencyMs: map[string]int32{"HongKong": 21, "tokyo": 68},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GetPlayer().GetRegionLatencyMs()["hongkong"] != 21 || updated.GetPlayer().GetRegionLatencyMs()["tokyo"] != 68 {
		t.Fatalf("updated latency = %#v", updated.GetPlayer().GetRegionLatencyMs())
	}
}

func TestServerApplyMatchResultAndGetHistory(t *testing.T) {
	ctx := context.Background()
	repository := memory.NewRepository()
	playerService := application.NewService(repository, nil)
	calculator, _ := elo.NewCalculator(32)
	ratingService := application.NewRatingService(repository, calculator, nil)
	server := NewServer(playerService, ratingService)

	teamA := make([]string, 0, 5)
	teamB := make([]string, 0, 5)
	for _, team := range []struct {
		prefix string
		rating float64
		ids    *[]string
	}{
		{prefix: "a", rating: 1600, ids: &teamA},
		{prefix: "b", rating: 1500, ids: &teamB},
	} {
		for index := 1; index <= 5; index++ {
			playerID := fmt.Sprintf("%s-%d", team.prefix, index)
			_, err := server.CreatePlayer(ctx, &playerv1.CreatePlayerRequest{
				Id:              playerID,
				Name:            playerID,
				InitialRating:   team.rating,
				PreferredRoles:  []playerv1.Role{playerv1.Role_ROLE_CORE},
				HomeRegion:      "hongkong",
				RegionLatencyMs: map[string]int32{"hongkong": 30},
				BehaviorScore:   90,
			})
			if err != nil {
				t.Fatalf("CreatePlayer(%s) error = %v", playerID, err)
			}
			*team.ids = append(*team.ids, playerID)
		}
	}

	response, err := server.ApplyMatchResult(ctx, &playerv1.ApplyMatchResultRequest{
		MatchId:        "match-1001",
		TeamAPlayerIds: teamA,
		TeamBPlayerIds: teamB,
		Outcome:        playerv1.MatchOutcome_MATCH_OUTCOME_TEAM_A_WIN,
		Reason:         "ranked_match",
	})
	if err != nil {
		t.Fatalf("ApplyMatchResult() error = %v", err)
	}
	if len(response.GetChanges()) != 10 {
		t.Fatalf("changes = %d, want 10", len(response.GetChanges()))
	}

	history, err := server.GetRatingHistory(ctx, &playerv1.GetRatingHistoryRequest{PlayerId: teamA[0]})
	if err != nil {
		t.Fatalf("GetRatingHistory() error = %v", err)
	}
	if len(history.GetChanges()) != 1 || history.GetChanges()[0].GetDelta() <= 0 {
		t.Fatalf("history = %+v, want one positive change", history.GetChanges())
	}
	if history.GetChanges()[0].GetRatingSystem() != playerv1.RatingSystem_RATING_SYSTEM_ELO ||
		history.GetChanges()[0].GetRatingDeviationAfter() <= 0 {
		t.Fatalf("history rating metadata = %+v", history.GetChanges()[0])
	}
}
