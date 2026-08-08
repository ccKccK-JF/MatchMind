package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/player/application"
	"github.com/ccKccK-JF/MatchMind/internal/player/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

var _ application.Repository = (*Repository)(nil)
var _ application.RatingRepository = (*Repository)(nil)

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, player *domain.Player) error {
	latencies, err := json.Marshal(player.RegionLatency())
	if err != nil {
		return fmt.Errorf("marshal player latency: %w", err)
	}
	proficiency, err := json.Marshal(player.HeroProficiency())
	if err != nil {
		return fmt.Errorf("marshal player hero proficiency: %w", err)
	}
	_, err = r.pool.Exec(ctx, `
		INSERT INTO players (
			id, name, rating, rating_deviation, rating_volatility, preferred_roles,
			home_region, region_latency, behavior_score, hero_proficiency, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$11)
	`,
		player.ID(), player.Name(), player.Rating(), player.RatingDeviation(), player.RatingVolatility(),
		rolesToStrings(player.PreferredRoles()), player.HomeRegion(), latencies,
		player.BehaviorScore(), proficiency, player.CreatedAt(),
	)
	if uniqueViolation(err) {
		return application.ErrPlayerAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("insert player: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, playerID string) (*domain.Player, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, rating, rating_deviation, rating_volatility,
		       banned, COALESCE(ban_reason, ''), banned_at, COALESCE(banned_by, ''), preferred_roles,
		       home_region, region_latency, behavior_score, hero_proficiency, created_at
		FROM players
		WHERE id = $1
	`, strings.TrimSpace(playerID))
	player, err := scanPlayer(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, application.ErrPlayerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select player: %w", err)
	}
	return player, nil
}

func (r *Repository) GetBanStates(ctx context.Context, playerIDs []string) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, banned FROM players WHERE id = ANY($1)`, playerIDs)
	if err != nil {
		return nil, fmt.Errorf("select player ban states: %w", err)
	}
	defer rows.Close()
	result := make(map[string]bool, len(playerIDs))
	for rows.Next() {
		var playerID string
		var banned bool
		if err := rows.Scan(&playerID, &banned); err != nil {
			return nil, fmt.Errorf("scan player ban state: %w", err)
		}
		result[playerID] = banned
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate player ban states: %w", err)
	}
	return result, nil
}

func (r *Repository) UpdateRegionLatency(
	ctx context.Context,
	playerID string,
	latencies map[string]int,
	updatedAt time.Time,
) (*domain.Player, error) {
	playerID = strings.TrimSpace(playerID)
	current, err := r.GetByID(ctx, playerID)
	if err != nil {
		return nil, err
	}
	updated, err := current.WithRegionLatency(latencies)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(updated.RegionLatency())
	if err != nil {
		return nil, fmt.Errorf("marshal player latency update: %w", err)
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE players SET region_latency = $1, updated_at = $2 WHERE id = $3
	`, encoded, updatedAt.UTC(), playerID)
	if err != nil {
		return nil, fmt.Errorf("update player latency: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, application.ErrPlayerNotFound
	}
	return r.GetByID(ctx, playerID)
}

func (r *Repository) SetBanState(
	ctx context.Context,
	playerID string,
	banned bool,
	reason, operatorID string,
	changedAt time.Time,
) (*domain.Player, error) {
	playerID = strings.TrimSpace(playerID)
	current, err := r.GetByID(ctx, playerID)
	if err != nil {
		return nil, err
	}
	updated, err := current.WithBanState(banned, reason, operatorID, changedAt)
	if err != nil {
		return nil, err
	}
	var banReason, bannedBy any
	var bannedAt any
	if updated.Banned() {
		banReason = updated.BanReason()
		bannedAt = updated.BannedAt()
		bannedBy = updated.BannedBy()
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE players
		SET banned = $1, ban_reason = $2, banned_at = $3, banned_by = $4, updated_at = $5
		WHERE id = $6
	`, updated.Banned(), banReason, bannedAt, bannedBy, changedAt.UTC(), playerID)
	if err != nil {
		return nil, fmt.Errorf("update player ban state: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return nil, application.ErrPlayerNotFound
	}
	return r.GetByID(ctx, playerID)
}

func (r *Repository) ApplyRatingChanges(
	ctx context.Context,
	matchID string,
	changes []*domain.RatingChange,
) ([]*domain.RatingChange, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" || len(changes) == 0 {
		return nil, application.ErrRatingConflict
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return nil, fmt.Errorf("begin rating transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Serialize retries for the same Match before the idempotency read.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, matchID); err != nil {
		return nil, fmt.Errorf("lock rating match: %w", err)
	}
	existing, err := queryChanges(ctx, tx, `
		SELECT player_id, match_id, rating_before, rating_after,
		       deviation_before, deviation_after, volatility_before, volatility_after,
		       rating_system, reason, created_at
		FROM rating_changes
		WHERE match_id = $1
		ORDER BY sequence
	`, matchID)
	if err != nil {
		return nil, fmt.Errorf("query existing rating changes: %w", err)
	}
	if len(existing) > 0 {
		return existing, nil
	}

	seen := make(map[string]struct{}, len(changes))
	committedChanges := cloneChanges(changes)
	for sequence, change := range changes {
		if change == nil || change.MatchID() != matchID {
			return nil, application.ErrRatingConflict
		}
		if _, duplicate := seen[change.PlayerID()]; duplicate {
			return nil, application.ErrRatingConflict
		}
		seen[change.PlayerID()] = struct{}{}
		if !change.HasUncertaintyState() {
			var deviation, volatility float64
			if err := tx.QueryRow(ctx, `
				SELECT rating_deviation, rating_volatility
				FROM players WHERE id = $1 AND rating = $2
				FOR UPDATE
			`, change.PlayerID(), change.Before()).Scan(&deviation, &volatility); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, fmt.Errorf("%w: player %s rating changed", application.ErrRatingConflict, change.PlayerID())
				}
				return nil, fmt.Errorf("load player uncertainty: %w", err)
			}
			before := domain.RatingState{Rating: change.Before(), Deviation: deviation, Volatility: volatility}
			after := before
			after.Rating = change.After()
			change, err = domain.NewRatingChangeWithState(domain.NewRatingChangeParams{
				PlayerID: change.PlayerID(), MatchID: change.MatchID(), Before: before, After: after,
				System: change.System(), Reason: change.Reason(), CreatedAt: change.CreatedAt(),
			})
			if err != nil {
				return nil, err
			}
			committedChanges[sequence] = change
		}
		var tag pgconn.CommandTag
		if change.HasUncertaintyState() {
			tag, err = tx.Exec(ctx, `
				UPDATE players
				SET rating = $1, rating_deviation = $2, rating_volatility = $3, updated_at = $4
				WHERE id = $5 AND rating = $6 AND rating_deviation = $7 AND rating_volatility = $8
			`, change.After(), change.DeviationAfter(), change.VolatilityAfter(), change.CreatedAt(),
				change.PlayerID(), change.Before(), change.DeviationBefore(), change.VolatilityBefore())
		} else {
			tag, err = tx.Exec(ctx, `
				UPDATE players
				SET rating = $1, updated_at = $2
				WHERE id = $3 AND rating = $4
			`, change.After(), change.CreatedAt(), change.PlayerID(), change.Before())
		}
		if err != nil {
			return nil, fmt.Errorf("update player rating: %w", err)
		}
		if tag.RowsAffected() != 1 {
			return nil, fmt.Errorf("%w: player %s rating changed", application.ErrRatingConflict, change.PlayerID())
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO rating_changes (
				match_id, player_id, sequence, rating_before, rating_after,
				deviation_before, deviation_after, volatility_before, volatility_after,
				rating_system, reason, created_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		`, matchID, change.PlayerID(), sequence, change.Before(), change.After(),
			change.DeviationBefore(), change.DeviationAfter(), change.VolatilityBefore(), change.VolatilityAfter(),
			change.System(), change.Reason(), change.CreatedAt()); err != nil {
			return nil, fmt.Errorf("insert rating change: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit rating transaction: %w", err)
	}
	return cloneChanges(committedChanges), nil
}

func (r *Repository) RatingHistory(ctx context.Context, playerID string) ([]*domain.RatingChange, error) {
	playerID = strings.TrimSpace(playerID)
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM players WHERE id = $1)`, playerID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check player history owner: %w", err)
	}
	if !exists {
		return nil, application.ErrPlayerNotFound
	}
	changes, err := queryChanges(ctx, r.pool, `
		SELECT player_id, match_id, rating_before, rating_after,
		       deviation_before, deviation_after, volatility_before, volatility_after,
		       rating_system, reason, created_at
		FROM rating_changes
		WHERE player_id = $1
		ORDER BY created_at, match_id
	`, playerID)
	if err != nil {
		return nil, fmt.Errorf("query rating history: %w", err)
	}
	return changes, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func scanPlayer(row rowScanner) (*domain.Player, error) {
	var snapshot domain.PlayerSnapshot
	var roles []string
	var latencyJSON []byte
	var proficiencyJSON []byte
	var bannedAt *time.Time
	if err := row.Scan(
		&snapshot.ID, &snapshot.Name, &snapshot.Rating, &snapshot.RatingDeviation, &snapshot.RatingVolatility,
		&snapshot.Banned, &snapshot.BanReason, &bannedAt, &snapshot.BannedBy,
		&roles, &snapshot.HomeRegion, &latencyJSON, &snapshot.BehaviorScore, &proficiencyJSON, &snapshot.CreatedAt,
	); err != nil {
		return nil, err
	}
	if bannedAt != nil {
		snapshot.BannedAt = *bannedAt
	}
	parsedRoles, err := stringsToRoles(roles)
	if err != nil {
		return nil, err
	}
	snapshot.PreferredRoles = parsedRoles
	if err := json.Unmarshal(latencyJSON, &snapshot.RegionLatency); err != nil {
		return nil, fmt.Errorf("decode player latency: %w", err)
	}
	if err := json.Unmarshal(proficiencyJSON, &snapshot.HeroProficiency); err != nil {
		return nil, fmt.Errorf("decode player hero proficiency: %w", err)
	}
	return domain.RestorePlayer(snapshot)
}

func queryChanges(ctx context.Context, source queryer, query string, argument any) ([]*domain.RatingChange, error) {
	rows, err := source.Query(ctx, query, argument)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*domain.RatingChange
	for rows.Next() {
		var playerID, matchID, reason, system string
		var before, after, deviationBefore, deviationAfter, volatilityBefore, volatilityAfter float64
		var createdAt time.Time
		if err := rows.Scan(
			&playerID, &matchID, &before, &after,
			&deviationBefore, &deviationAfter, &volatilityBefore, &volatilityAfter,
			&system, &reason, &createdAt,
		); err != nil {
			return nil, err
		}
		change, err := domain.NewRatingChangeWithState(domain.NewRatingChangeParams{
			PlayerID: playerID, MatchID: matchID,
			Before: domain.RatingState{Rating: before, Deviation: deviationBefore, Volatility: volatilityBefore},
			After:  domain.RatingState{Rating: after, Deviation: deviationAfter, Volatility: volatilityAfter},
			System: domain.RatingSystem(system), Reason: reason, CreatedAt: createdAt,
		})
		if err != nil {
			return nil, err
		}
		result = append(result, change)
	}
	return result, rows.Err()
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
			return nil, fmt.Errorf("unknown stored player role %q", value)
		}
	}
	return roles, nil
}

func uniqueViolation(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}

func cloneChanges(changes []*domain.RatingChange) []*domain.RatingChange {
	result := make([]*domain.RatingChange, 0, len(changes))
	for _, change := range changes {
		result = append(result, change.Clone())
	}
	return result
}
