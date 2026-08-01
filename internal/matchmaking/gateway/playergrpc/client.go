package playergrpc

import (
	"context"
	"fmt"

	playerv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/player/v1"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Client struct {
	client playerv1.PlayerServiceClient
}

func NewClient(client playerv1.PlayerServiceClient) *Client {
	return &Client{client: client}
}

func (c *Client) GetPlayer(ctx context.Context, playerID string) (application.PlayerSnapshot, error) {
	response, err := c.client.GetPlayer(ctx, &playerv1.GetPlayerRequest{PlayerId: playerID})
	if err != nil {
		switch status.Code(err) {
		case codes.NotFound:
			return application.PlayerSnapshot{}, application.ErrPlayerNotFound
		case codes.Canceled, codes.DeadlineExceeded:
			return application.PlayerSnapshot{}, err
		default:
			return application.PlayerSnapshot{}, fmt.Errorf("%w: %v", application.ErrPlayerServiceUnavailable, err)
		}
	}
	player := response.GetPlayer()
	if player == nil {
		return application.PlayerSnapshot{}, application.ErrPlayerNotFound
	}

	roles := make([]domain.Role, 0, len(player.GetPreferredRoles()))
	for _, role := range player.GetPreferredRoles() {
		converted, err := roleFromProto(role)
		if err != nil {
			return application.PlayerSnapshot{}, err
		}
		roles = append(roles, converted)
	}
	latency := make(map[string]int, len(player.GetRegionLatencyMs()))
	for region, milliseconds := range player.GetRegionLatencyMs() {
		latency[region] = int(milliseconds)
	}
	return application.PlayerSnapshot{
		ID:             player.GetId(),
		Rating:         player.GetRating(),
		PreferredRoles: roles,
		RegionLatency:  latency,
	}, nil
}

func roleFromProto(role playerv1.Role) (domain.Role, error) {
	switch role {
	case playerv1.Role_ROLE_VANGUARD:
		return domain.RoleVanguard, nil
	case playerv1.Role_ROLE_ROAMER:
		return domain.RoleRoamer, nil
	case playerv1.Role_ROLE_CORE:
		return domain.RoleCore, nil
	case playerv1.Role_ROLE_RANGED:
		return domain.RoleRanged, nil
	case playerv1.Role_ROLE_SUPPORT:
		return domain.RoleSupport, nil
	default:
		return "", fmt.Errorf("%w: player has unsupported role", application.ErrPlayerServiceUnavailable)
	}
}
