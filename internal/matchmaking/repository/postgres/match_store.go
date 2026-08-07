package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const matchColumns = `
	id, mode, team_a, team_b, state, server_region, server_address,
	connection_token, policy_version, quality, result, revision, created_at, updated_at
`

type MatchStore struct {
	pool *pgxpool.Pool
}

var _ application.MatchRepository = (*MatchStore)(nil)

func NewMatchStore(pool *pgxpool.Pool) *MatchStore {
	return &MatchStore{pool: pool}
}

func (s *MatchStore) Create(ctx context.Context, match *domain.Match) error {
	if match == nil {
		return domain.ErrInvalidMatch
	}
	snapshot := match.Snapshot()
	teamA, teamB, quality, result, err := encodeMatchSnapshot(snapshot)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO matches (
			id, mode, team_a, team_b, state, server_region, server_address,
			connection_token, policy_version, quality, result, revision, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
	`, snapshot.ID, snapshot.Mode, teamA, teamB, string(snapshot.State), snapshot.ServerRegion,
		nullableString(snapshot.ServerAddress), nullableString(snapshot.ConnectionToken),
		snapshot.PolicyVersion, quality, result, snapshot.Revision, snapshot.CreatedAt, snapshot.UpdatedAt)
	if postgresConstraint(err, "matches_pkey") {
		return application.ErrMatchAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("insert match: %w", err)
	}
	return nil
}

func (s *MatchStore) Get(ctx context.Context, matchID string) (*domain.Match, error) {
	match, err := scanMatch(s.pool.QueryRow(ctx, `SELECT `+matchColumns+` FROM matches WHERE id = $1`, strings.TrimSpace(matchID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, application.ErrMatchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select match: %w", err)
	}
	return match, nil
}

func (s *MatchStore) Update(ctx context.Context, match *domain.Match) error {
	if match == nil || match.Revision() < 2 {
		return domain.ErrInvalidMatch
	}
	snapshot := match.Snapshot()
	teamA, teamB, quality, result, err := encodeMatchSnapshot(snapshot)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE matches SET
			mode = $1, team_a = $2, team_b = $3, state = $4, server_region = $5,
			server_address = $6, connection_token = $7, policy_version = $8,
			quality = $9, result = $10, revision = $11, updated_at = $12
		WHERE id = $13 AND revision = $14
	`, snapshot.Mode, teamA, teamB, string(snapshot.State), snapshot.ServerRegion,
		nullableString(snapshot.ServerAddress), nullableString(snapshot.ConnectionToken),
		snapshot.PolicyVersion, quality, result, snapshot.Revision, snapshot.UpdatedAt,
		snapshot.ID, snapshot.Revision-1)
	if err != nil {
		return matchWriteError(err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM matches WHERE id = $1)`, snapshot.ID).Scan(&exists); err != nil {
		return fmt.Errorf("check match after update conflict: %w", err)
	}
	if !exists {
		return application.ErrMatchNotFound
	}
	return application.ErrMatchRevisionConflict
}

// AssignReservedTickets persists the READY Match and assigns every reserved
// Ticket in one transaction, removing the cross-repository failure window.
func (s *MatchStore) AssignReservedTickets(
	ctx context.Context,
	reservationID string,
	match *domain.Match,
	now time.Time,
) error {
	if match == nil || match.State() != domain.MatchStateReady || match.Revision() < 2 {
		return domain.ErrInvalidMatch
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin match assignment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tickets, err := lockReservation(ctx, tx, reservationID)
	if err != nil {
		return err
	}
	if err := updateMatchRow(ctx, tx, match); err != nil {
		return err
	}
	for _, ticket := range tickets {
		if err := ticket.Assign(reservationID, match.ID(), now); err != nil {
			return application.ErrReservationConflict
		}
		if err := updateTicket(ctx, tx, ticket); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return matchWriteError(err)
	}
	return nil
}

type matchExecer interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func updateMatchRow(ctx context.Context, executor matchExecer, match *domain.Match) error {
	snapshot := match.Snapshot()
	teamA, teamB, quality, result, err := encodeMatchSnapshot(snapshot)
	if err != nil {
		return err
	}
	tag, err := executor.Exec(ctx, `
		UPDATE matches SET
			mode = $1, team_a = $2, team_b = $3, state = $4, server_region = $5,
			server_address = $6, connection_token = $7, policy_version = $8,
			quality = $9, result = $10, revision = $11, updated_at = $12
		WHERE id = $13 AND revision = $14
	`, snapshot.Mode, teamA, teamB, string(snapshot.State), snapshot.ServerRegion,
		nullableString(snapshot.ServerAddress), nullableString(snapshot.ConnectionToken),
		snapshot.PolicyVersion, quality, result, snapshot.Revision, snapshot.UpdatedAt,
		snapshot.ID, snapshot.Revision-1)
	if err != nil {
		return matchWriteError(err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var exists bool
	if err := executor.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM matches WHERE id = $1)`, snapshot.ID).Scan(&exists); err != nil {
		return fmt.Errorf("check match after update conflict: %w", err)
	}
	if !exists {
		return application.ErrMatchNotFound
	}
	return application.ErrMatchRevisionConflict
}

func encodeMatchSnapshot(snapshot domain.MatchSnapshot) ([]byte, []byte, []byte, []byte, error) {
	teamA, err := json.Marshal(snapshot.TeamA)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal team A: %w", err)
	}
	teamB, err := json.Marshal(snapshot.TeamB)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal team B: %w", err)
	}
	quality, err := json.Marshal(snapshot.Quality)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("marshal match quality: %w", err)
	}
	var result []byte
	if snapshot.Result != nil {
		result, err = json.Marshal(snapshot.Result)
		if err != nil {
			return nil, nil, nil, nil, fmt.Errorf("marshal match result: %w", err)
		}
	}
	return teamA, teamB, quality, result, nil
}

type matchRow interface {
	Scan(dest ...any) error
}

func scanMatch(row matchRow) (*domain.Match, error) {
	var snapshot domain.MatchSnapshot
	var state string
	var teamA, teamB, quality, result []byte
	var serverAddress, connectionToken *string
	if err := row.Scan(
		&snapshot.ID, &snapshot.Mode, &teamA, &teamB, &state, &snapshot.ServerRegion,
		&serverAddress, &connectionToken, &snapshot.PolicyVersion, &quality, &result,
		&snapshot.Revision, &snapshot.CreatedAt, &snapshot.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(teamA, &snapshot.TeamA); err != nil {
		return nil, fmt.Errorf("unmarshal team A: %w", err)
	}
	if err := json.Unmarshal(teamB, &snapshot.TeamB); err != nil {
		return nil, fmt.Errorf("unmarshal team B: %w", err)
	}
	if err := json.Unmarshal(quality, &snapshot.Quality); err != nil {
		return nil, fmt.Errorf("unmarshal match quality: %w", err)
	}
	if len(result) > 0 {
		snapshot.Result = &domain.MatchResult{}
		if err := json.Unmarshal(result, snapshot.Result); err != nil {
			return nil, fmt.Errorf("unmarshal match result: %w", err)
		}
	}
	snapshot.State = domain.MatchState(state)
	if serverAddress != nil {
		snapshot.ServerAddress = *serverAddress
	}
	if connectionToken != nil {
		snapshot.ConnectionToken = *connectionToken
	}
	match, err := domain.RestoreMatch(snapshot)
	if err != nil {
		return nil, fmt.Errorf("restore match: %w", err)
	}
	return match, nil
}

func matchWriteError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "40001" || postgresError.Code == "40P01") {
		return application.ErrMatchRevisionConflict
	}
	return fmt.Errorf("update match: %w", err)
}
