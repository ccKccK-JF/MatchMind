package matchmakinggrpc

import (
	"context"
	"fmt"

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
	"github.com/ccKccK-JF/MatchMind/internal/simulation/application"
	simulationdomain "github.com/ccKccK-JF/MatchMind/internal/simulation/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Client struct {
	client matchmakingv1.MatchmakingServiceClient
}

func NewClient(client matchmakingv1.MatchmakingServiceClient) *Client {
	return &Client{client: client}
}

func (c *Client) GetMatch(ctx context.Context, matchID string) (application.MatchSnapshot, error) {
	response, err := c.client.GetMatch(ctx, &matchmakingv1.GetMatchRequest{MatchId: matchID})
	if err != nil {
		if status.Code(err) == codes.NotFound {
			return application.MatchSnapshot{}, application.ErrMatchNotFound
		}
		return application.MatchSnapshot{}, err
	}
	match := response.GetMatch()
	if match == nil {
		return application.MatchSnapshot{}, application.ErrMatchNotFound
	}
	snapshot := application.MatchSnapshot{
		ID: match.GetId(), Mode: match.GetMode(), State: matchState(match.GetState()),
		TeamAPlayerIDs:     append([]string(nil), match.GetTeamA().GetPlayerIds()...),
		TeamBPlayerIDs:     append([]string(nil), match.GetTeamB().GetPlayerIds()...),
		TeamAAverageRating: match.GetTeamA().GetAverageRating(),
		TeamBAverageRating: match.GetTeamB().GetAverageRating(),
		PredictedWinRateA:  match.GetPredictedWinRateA(), RoleScore: match.GetRoleScore(),
		LatencyScore: match.GetLatencyScore(), PartyScore: match.GetPartyScore(),
	}
	if match.GetResult() != nil {
		result, err := resultFromProto(match.GetId(), match.GetResult())
		if err != nil {
			return application.MatchSnapshot{}, err
		}
		snapshot.ExistingResult = &result
	}
	return snapshot, nil
}

func (c *Client) StartMatch(ctx context.Context, matchID string) error {
	_, err := c.client.StartMatch(ctx, &matchmakingv1.StartMatchRequest{MatchId: matchID})
	return err
}

func (c *Client) CompleteMatch(ctx context.Context, result simulationdomain.Result) error {
	winningTeam := matchmakingv1.WinningTeam_WINNING_TEAM_UNSPECIFIED
	if result.WinningTeam == simulationdomain.WinningTeamA {
		winningTeam = matchmakingv1.WinningTeam_WINNING_TEAM_A
	} else if result.WinningTeam == simulationdomain.WinningTeamB {
		winningTeam = matchmakingv1.WinningTeam_WINNING_TEAM_B
	}
	_, err := c.client.CompleteMatch(ctx, &matchmakingv1.CompleteMatchRequest{
		MatchId: result.MatchID,
		Result: &matchmakingv1.MatchResult{
			WinningTeam: winningTeam, RandomSeed: result.RandomSeed,
			DurationSeconds: int32(result.DurationSeconds), ScoreA: int32(result.ScoreA), ScoreB: int32(result.ScoreB),
			MaxAdvantage: result.MaxAdvantage, HasAfk: result.HasAFK, Surrendered: result.Surrendered,
			OneSided: result.OneSided, ActualQualityScore: result.ActualQualityScore,
		},
	})
	return err
}

func matchState(state matchmakingv1.MatchState) string {
	switch state {
	case matchmakingv1.MatchState_MATCH_STATE_READY:
		return "READY"
	case matchmakingv1.MatchState_MATCH_STATE_RUNNING:
		return "RUNNING"
	case matchmakingv1.MatchState_MATCH_STATE_FINISHED:
		return "FINISHED"
	case matchmakingv1.MatchState_MATCH_STATE_FAILED:
		return "FAILED"
	case matchmakingv1.MatchState_MATCH_STATE_ALLOCATING:
		return "ALLOCATING"
	default:
		return "CREATED"
	}
}

func resultFromProto(matchID string, result *matchmakingv1.MatchResult) (simulationdomain.Result, error) {
	winningTeam := simulationdomain.WinningTeam("")
	switch result.GetWinningTeam() {
	case matchmakingv1.WinningTeam_WINNING_TEAM_A:
		winningTeam = simulationdomain.WinningTeamA
	case matchmakingv1.WinningTeam_WINNING_TEAM_B:
		winningTeam = simulationdomain.WinningTeamB
	default:
		return simulationdomain.Result{}, fmt.Errorf("match %s has invalid winning team", matchID)
	}
	return simulationdomain.Result{
		MatchID: matchID, RandomSeed: result.GetRandomSeed(), WinningTeam: winningTeam,
		DurationSeconds: int(result.GetDurationSeconds()), ScoreA: int(result.GetScoreA()), ScoreB: int(result.GetScoreB()),
		MaxAdvantage: result.GetMaxAdvantage(), HasAFK: result.GetHasAfk(), Surrendered: result.GetSurrendered(),
		OneSided: result.GetOneSided(), ActualQualityScore: result.GetActualQualityScore(),
	}, nil
}
