package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/jackc/pgx/v5/pgtype"
)

const ticketColumns = `
	id, player_id, party_id, mode, client_version, region, rating, behavior_score, hero_proficiency,
	preferred_roles, region_latency, state, created_at, updated_at,
	reservation_id, reservation_expires_at, match_id
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTicket(row rowScanner) (*domain.MatchTicket, error) {
	var snapshot domain.TicketSnapshot
	var roles []string
	var latencyJSON []byte
	var proficiencyJSON []byte
	var state string
	var reservationID, matchID pgtype.Text
	var reservationExpiresAt pgtype.Timestamptz
	if err := row.Scan(
		&snapshot.ID, &snapshot.PlayerID, &snapshot.PartyID, &snapshot.Mode,
		&snapshot.ClientVersion, &snapshot.Region, &snapshot.Rating,
		&snapshot.BehaviorScore, &proficiencyJSON,
		&roles, &latencyJSON, &state, &snapshot.CreatedAt, &snapshot.UpdatedAt,
		&reservationID, &reservationExpiresAt, &matchID,
	); err != nil {
		return nil, err
	}
	parsedRoles, err := stringsToRoles(roles)
	if err != nil {
		return nil, err
	}
	snapshot.PreferredRoles = parsedRoles
	if err := json.Unmarshal(proficiencyJSON, &snapshot.HeroProficiency); err != nil {
		return nil, fmt.Errorf("decode ticket hero proficiency: %w", err)
	}
	if err := json.Unmarshal(latencyJSON, &snapshot.RegionLatency); err != nil {
		return nil, fmt.Errorf("decode ticket latency: %w", err)
	}
	snapshot.State = domain.TicketState(state)
	if reservationID.Valid {
		snapshot.ReservationID = reservationID.String
	}
	if reservationExpiresAt.Valid {
		snapshot.ReservationExpiresAt = reservationExpiresAt.Time
	}
	if matchID.Valid {
		snapshot.MatchID = matchID.String
	}
	return domain.RestoreTicket(snapshot)
}

func rolesToStrings(roles []domain.Role) []string {
	result := make([]string, len(roles))
	for index, role := range roles {
		result[index] = string(role)
	}
	return result
}

func stringsToRoles(values []string) ([]domain.Role, error) {
	roles := make([]domain.Role, len(values))
	for index, value := range values {
		role := domain.Role(value)
		switch role {
		case domain.RoleVanguard, domain.RoleRoamer, domain.RoleCore, domain.RoleRanged, domain.RoleSupport:
			roles[index] = role
		default:
			return nil, fmt.Errorf("unknown stored ticket role %q", value)
		}
	}
	return roles, nil
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
