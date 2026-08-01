package playergrpc

import (
	"context"

	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	simulationdomain "github.com/ccKccK-JF/MatchMind/internal/simulation/domain"
)

type Client struct {
	client playerv1.PlayerServiceClient
}

func NewClient(client playerv1.PlayerServiceClient) *Client {
	return &Client{client: client}
}

func (c *Client) ApplyMatchResult(
	ctx context.Context,
	matchID string,
	teamAPlayerIDs, teamBPlayerIDs []string,
	winningTeam simulationdomain.WinningTeam,
) error {
	outcome := playerv1.MatchOutcome_MATCH_OUTCOME_UNSPECIFIED
	if winningTeam == simulationdomain.WinningTeamA {
		outcome = playerv1.MatchOutcome_MATCH_OUTCOME_TEAM_A_WIN
	} else if winningTeam == simulationdomain.WinningTeamB {
		outcome = playerv1.MatchOutcome_MATCH_OUTCOME_TEAM_B_WIN
	}
	_, err := c.client.ApplyMatchResult(ctx, &playerv1.ApplyMatchResultRequest{
		MatchId: matchID, TeamAPlayerIds: append([]string(nil), teamAPlayerIDs...),
		TeamBPlayerIds: append([]string(nil), teamBPlayerIDs...), Outcome: outcome, Reason: "ranked_match",
	})
	return err
}
