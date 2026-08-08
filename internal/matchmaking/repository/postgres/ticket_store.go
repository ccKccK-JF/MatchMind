package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TicketStore struct {
	pool *pgxpool.Pool
}

var _ application.TicketStore = (*TicketStore)(nil)
var _ application.MatchQueue = (*TicketStore)(nil)

func NewTicketStore(pool *pgxpool.Pool) *TicketStore {
	return &TicketStore{pool: pool}
}

func (s *TicketStore) CreateQueued(
	ctx context.Context,
	ticket *domain.MatchTicket,
	idempotencyKey string,
) (*domain.MatchTicket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if ticket == nil || ticket.State() != domain.TicketStateQueued || idempotencyKey == "" {
		return nil, domain.ErrInvalidTicket
	}
	latencies, err := json.Marshal(ticket.RegionLatency())
	if err != nil {
		return nil, fmt.Errorf("marshal ticket latency: %w", err)
	}
	var ticketID string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO tickets (
			id, player_id, party_id, mode, client_version, region, rating,
			preferred_roles, region_latency, state, create_idempotency_key,
			created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		ON CONFLICT ON CONSTRAINT tickets_player_create_key_unique
		DO UPDATE SET create_idempotency_key = EXCLUDED.create_idempotency_key
		RETURNING id
	`,
		ticket.ID(), ticket.PlayerID(), ticket.PartyID(), ticket.Mode(),
		ticket.ClientVersion(), ticket.Region(), ticket.Rating(),
		rolesToStrings(ticket.PreferredRoles()), latencies, string(ticket.State()),
		idempotencyKey, ticket.CreatedAt(), ticket.UpdatedAt(),
	).Scan(&ticketID)
	if postgresConstraint(err, "tickets_one_active_per_player_idx") {
		return nil, application.ErrActiveTicketExists
	}
	if postgresConstraint(err, "tickets_player_id_fkey") {
		return nil, application.ErrPlayerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("insert ticket: %w", err)
	}
	return s.Get(ctx, ticketID)
}

func (s *TicketStore) Get(ctx context.Context, ticketID string) (*domain.MatchTicket, error) {
	ticket, err := scanTicket(s.pool.QueryRow(ctx, `SELECT `+ticketColumns+` FROM tickets WHERE id = $1`, strings.TrimSpace(ticketID)))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, application.ErrTicketNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select ticket: %w", err)
	}
	return ticket, nil
}

func (s *TicketStore) Cancel(
	ctx context.Context,
	ticketID, playerID, idempotencyKey string,
	now time.Time,
) (*domain.MatchTicket, error) {
	ticketID = strings.TrimSpace(ticketID)
	playerID = strings.TrimSpace(playerID)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if idempotencyKey == "" {
		return nil, application.ErrIdempotencyKeyRequired
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin cancel ticket: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, playerID+"\x00"+idempotencyKey); err != nil {
		return nil, fmt.Errorf("lock cancel idempotency key: %w", err)
	}
	var previousTicketID string
	err = tx.QueryRow(ctx, `
		SELECT ticket_id FROM ticket_cancel_idempotency
		WHERE player_id = $1 AND idempotency_key = $2
	`, playerID, idempotencyKey).Scan(&previousTicketID)
	switch {
	case err == nil:
		if previousTicketID != ticketID {
			return nil, application.ErrTicketForbidden
		}
		ticket, err := getTicketTx(ctx, tx, ticketID, false)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("commit idempotent cancel: %w", err)
		}
		return ticket, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("query cancel idempotency: %w", err)
	}
	ticket, err := getTicketTx(ctx, tx, ticketID, true)
	if err != nil {
		return nil, err
	}
	if ticket.PlayerID() != playerID {
		return nil, application.ErrTicketForbidden
	}
	if err := ticket.Cancel(now); err != nil {
		return nil, err
	}
	if err := updateTicket(ctx, tx, ticket); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO ticket_cancel_idempotency (player_id, idempotency_key, ticket_id, created_at)
		VALUES ($1,$2,$3,$4)
	`, playerID, idempotencyKey, ticketID, now); err != nil {
		return nil, fmt.Errorf("insert cancel idempotency: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit cancel ticket: %w", err)
	}
	return ticket.Clone(), nil
}

func (s *TicketStore) PoolKeys(ctx context.Context) ([]domain.PoolKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT mode, client_version, region
		FROM tickets
		WHERE state = 'QUEUED'
		ORDER BY mode, client_version, region
	`)
	if err != nil {
		return nil, fmt.Errorf("query ticket pools: %w", err)
	}
	defer rows.Close()
	var result []domain.PoolKey
	for rows.Next() {
		var key domain.PoolKey
		if err := rows.Scan(&key.Mode, &key.ClientVersion, &key.Region); err != nil {
			return nil, err
		}
		result = append(result, key)
	}
	return result, rows.Err()
}

func (s *TicketStore) QueueSnapshot(
	ctx context.Context,
	key domain.PoolKey,
	limit int,
) ([]*domain.MatchTicket, error) {
	if limit <= 0 {
		limit = 1<<31 - 1
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+ticketColumns+`
		FROM tickets
		WHERE state = 'QUEUED' AND mode = $1 AND client_version = $2 AND region = $3
		ORDER BY created_at, id
		LIMIT $4
	`, key.Mode, key.ClientVersion, key.Region, limit)
	if err != nil {
		return nil, fmt.Errorf("query ticket queue: %w", err)
	}
	return scanTickets(rows)
}

func (s *TicketStore) ReserveAll(
	ctx context.Context,
	ticketIDs []string,
	reservationID string,
	expiresAt, now time.Time,
) ([]*domain.MatchTicket, error) {
	reservationID = strings.TrimSpace(reservationID)
	if reservationID == "" || len(ticketIDs) == 0 {
		return nil, application.ErrReservationConflict
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin ticket reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	existingRows, err := tx.Query(ctx, `
		SELECT `+ticketColumns+` FROM tickets
		WHERE reservation_id = $1 AND state = 'RESERVED'
		ORDER BY id FOR UPDATE
	`, reservationID)
	if err != nil {
		return nil, reservationError(err)
	}
	existing, err := scanTickets(existingRows)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		if !sameTicketIDs(ticketIDs, existing) {
			return nil, application.ErrReservationConflict
		}
		result, err := orderTickets(ticketIDs, existing)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, reservationError(err)
		}
		return result, nil
	}

	locked, err := lockTicketsByID(ctx, tx, ticketIDs)
	if err != nil {
		return nil, err
	}
	seenPlayers := make(map[string]struct{}, len(locked))
	for _, ticket := range locked {
		if !ticket.IsQueueCandidate() {
			return nil, application.ErrReservationConflict
		}
		if _, duplicate := seenPlayers[ticket.PlayerID()]; duplicate {
			return nil, application.ErrReservationConflict
		}
		seenPlayers[ticket.PlayerID()] = struct{}{}
		if err := ticket.Reserve(reservationID, expiresAt, now); err != nil {
			return nil, application.ErrReservationConflict
		}
		if err := updateTicket(ctx, tx, ticket); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, reservationError(err)
	}
	return orderTickets(ticketIDs, locked)
}

func (s *TicketStore) ReleaseAll(ctx context.Context, reservationID string, now time.Time) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("begin reservation release: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tickets, err := lockReservation(ctx, tx, reservationID)
	if err != nil {
		return err
	}
	for _, ticket := range tickets {
		if err := ticket.ReleaseReservation(reservationID, now); err != nil {
			return application.ErrReservationConflict
		}
		if err := updateTicket(ctx, tx, ticket); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return reservationError(err)
	}
	return nil
}

func (s *TicketStore) AssignAll(
	ctx context.Context,
	reservationID, matchID string,
	now time.Time,
) ([]*domain.MatchTicket, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin ticket assignment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tickets, err := lockReservation(ctx, tx, reservationID)
	if err != nil {
		return nil, err
	}
	for _, ticket := range tickets {
		if err := ticket.Assign(reservationID, matchID, now); err != nil {
			return nil, application.ErrReservationConflict
		}
		if err := updateTicket(ctx, tx, ticket); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, reservationError(err)
	}
	return cloneTickets(tickets), nil
}

func (s *TicketStore) RecoverExpiredReservations(ctx context.Context, now time.Time) (int, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return 0, fmt.Errorf("begin reservation recovery: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT `+ticketColumns+` FROM tickets
		WHERE state = 'RESERVED' AND reservation_expires_at <= $1
		ORDER BY reservation_expires_at, id
		LIMIT 1000 FOR UPDATE SKIP LOCKED
	`, now)
	if err != nil {
		return 0, reservationError(err)
	}
	tickets, err := scanTickets(rows)
	if err != nil {
		return 0, err
	}
	for _, ticket := range tickets {
		if err := ticket.ReleaseExpiredReservation(now); err != nil {
			return 0, application.ErrReservationConflict
		}
		if err := updateTicket(ctx, tx, ticket); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, reservationError(err)
	}
	return len(tickets), nil
}

func (s *TicketStore) QueueSize(ctx context.Context) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM tickets WHERE state = 'QUEUED'`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count queued tickets: %w", err)
	}
	return count, nil
}

func (s *TicketStore) CompleteAssignedTickets(ctx context.Context, matchID string, now time.Time) error {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return domain.ErrInvalidMatch
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE tickets SET active = FALSE, updated_at = GREATEST(updated_at, $2)
		WHERE match_id = $1 AND state = 'ASSIGNED' AND active
	`, matchID, now); err != nil {
		return fmt.Errorf("complete assigned tickets: %w", err)
	}
	return nil
}

func (s *TicketStore) ActiveTickets(ctx context.Context) ([]*domain.MatchTicket, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+ticketColumns+` FROM tickets
		WHERE active AND state IN ('QUEUED', 'RESERVED')
		ORDER BY created_at, id
	`)
	if err != nil {
		return nil, fmt.Errorf("list active tickets: %w", err)
	}
	return scanTickets(rows)
}

func getTicketTx(ctx context.Context, tx pgx.Tx, ticketID string, lock bool) (*domain.MatchTicket, error) {
	query := `SELECT ` + ticketColumns + ` FROM tickets WHERE id = $1`
	if lock {
		query += ` FOR UPDATE`
	}
	ticket, err := scanTicket(tx.QueryRow(ctx, query, ticketID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, application.ErrTicketNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("select ticket transactionally: %w", err)
	}
	return ticket, nil
}

func lockTicketsByID(ctx context.Context, tx pgx.Tx, ticketIDs []string) ([]*domain.MatchTicket, error) {
	unique := make(map[string]struct{}, len(ticketIDs))
	for _, ticketID := range ticketIDs {
		ticketID = strings.TrimSpace(ticketID)
		if ticketID == "" {
			return nil, application.ErrReservationConflict
		}
		if _, duplicate := unique[ticketID]; duplicate {
			return nil, application.ErrReservationConflict
		}
		unique[ticketID] = struct{}{}
	}
	rows, err := tx.Query(ctx, `
		SELECT `+ticketColumns+` FROM tickets
		WHERE id = ANY($1)
		ORDER BY id FOR UPDATE
	`, ticketIDs)
	if err != nil {
		return nil, reservationError(err)
	}
	tickets, err := scanTickets(rows)
	if err != nil {
		return nil, err
	}
	if len(tickets) != len(ticketIDs) {
		return nil, application.ErrReservationConflict
	}
	return tickets, nil
}

func lockReservation(ctx context.Context, tx pgx.Tx, reservationID string) ([]*domain.MatchTicket, error) {
	reservationID = strings.TrimSpace(reservationID)
	if reservationID == "" {
		return nil, application.ErrReservationConflict
	}
	rows, err := tx.Query(ctx, `
		SELECT `+ticketColumns+` FROM tickets
		WHERE reservation_id = $1 AND state = 'RESERVED'
		ORDER BY id FOR UPDATE
	`, reservationID)
	if err != nil {
		return nil, reservationError(err)
	}
	tickets, err := scanTickets(rows)
	if err != nil {
		return nil, err
	}
	if len(tickets) == 0 {
		return nil, application.ErrReservationConflict
	}
	return tickets, nil
}

func updateTicket(ctx context.Context, tx pgx.Tx, ticket *domain.MatchTicket) error {
	tag, err := tx.Exec(ctx, `
		UPDATE tickets SET
			state = $1, reservation_id = $2, reservation_expires_at = $3,
			match_id = $4, updated_at = $5, active = $6
		WHERE id = $7
	`,
		string(ticket.State()), nullableString(ticket.ReservationID()),
		nullableTime(ticket.ReservationExpiresAt()), nullableString(ticket.MatchID()),
		ticket.UpdatedAt(), ticket.IsActive(), ticket.ID(),
	)
	if err != nil {
		return reservationError(err)
	}
	if tag.RowsAffected() != 1 {
		return application.ErrTicketNotFound
	}
	return nil
}

func scanTickets(rows pgx.Rows) ([]*domain.MatchTicket, error) {
	defer rows.Close()
	var result []*domain.MatchTicket
	for rows.Next() {
		ticket, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, ticket)
	}
	return result, rows.Err()
}

func orderTickets(ticketIDs []string, tickets []*domain.MatchTicket) ([]*domain.MatchTicket, error) {
	byID := make(map[string]*domain.MatchTicket, len(tickets))
	for _, ticket := range tickets {
		byID[ticket.ID()] = ticket
	}
	result := make([]*domain.MatchTicket, 0, len(ticketIDs))
	for _, ticketID := range ticketIDs {
		ticket := byID[ticketID]
		if ticket == nil {
			return nil, application.ErrReservationConflict
		}
		result = append(result, ticket.Clone())
	}
	return result, nil
}

func sameTicketIDs(ticketIDs []string, tickets []*domain.MatchTicket) bool {
	if len(ticketIDs) != len(tickets) {
		return false
	}
	left := append([]string(nil), ticketIDs...)
	right := make([]string, len(tickets))
	for index, ticket := range tickets {
		right[index] = ticket.ID()
	}
	sort.Strings(left)
	sort.Strings(right)
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func cloneTickets(tickets []*domain.MatchTicket) []*domain.MatchTicket {
	result := make([]*domain.MatchTicket, 0, len(tickets))
	for _, ticket := range tickets {
		result = append(result, ticket.Clone())
	}
	return result
}

func postgresConstraint(err error, constraint string) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.ConstraintName == constraint
}

func reservationError(err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && (postgresError.Code == "40001" || postgresError.Code == "40P01") {
		return application.ErrReservationConflict
	}
	return err
}
