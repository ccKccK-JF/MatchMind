package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	redisclient "github.com/redis/go-redis/v9"
)

var reserveScript = redisclient.NewScript(`
local reservation = ARGV[1]
local expires_at = ARGV[2]
local ttl = tonumber(ARGV[3])
local queued = 0
local reserved = 0
for index = 4, #ARGV do
    local ticket = ARGV[index]
    local state = redis.call('HGET', KEYS[1], ticket)
    if not redis.call('HGET', KEYS[2], ticket)
       or not redis.call('HGET', KEYS[3], ticket) then
        return redis.error_reply('RESERVATION_CONFLICT')
    end
    if state == 'QUEUED' then
        queued = queued + 1
    elseif state == 'RESERVED' and redis.call('HGET', KEYS[4], ticket) == reservation then
        reserved = reserved + 1
    else
        return redis.error_reply('RESERVATION_CONFLICT')
    end
end
if queued > 0 and reserved > 0 then
    return redis.error_reply('RESERVATION_CONFLICT')
end
local reservation_entries = redis.call('HGETALL', KEYS[4])
local reservation_ticket_count = 0
for index = 1, #reservation_entries, 2 do
    if reservation_entries[index + 1] == reservation then
        reservation_ticket_count = reservation_ticket_count + 1
    end
end
if queued > 0 and reservation_ticket_count > 0 then
    return redis.error_reply('RESERVATION_CONFLICT')
end
if reserved == (#ARGV - 3) then
    if reservation_ticket_count ~= reserved then
        return redis.error_reply('RESERVATION_CONFLICT')
    end
    for index = 4, #ARGV do
        redis.call('SADD', KEYS[6], ARGV[index])
    end
    redis.call('PEXPIRE', KEYS[6], ttl)
    return reserved
end
for index = 4, #ARGV do
    local ticket = ARGV[index]
    local pool = redis.call('HGET', KEYS[2], ticket)
    if not pool then
        return redis.error_reply('RESERVATION_CONFLICT')
    end
    redis.call('ZREM', pool, ticket)
    redis.call('HSET', KEYS[1], ticket, 'RESERVED')
    redis.call('HSET', KEYS[4], ticket, reservation)
    redis.call('ZADD', KEYS[5], expires_at, ticket)
    redis.call('SADD', KEYS[6], ticket)
end
redis.call('PEXPIRE', KEYS[6], ttl)
return #ARGV - 3
`)

var releaseScript = redisclient.NewScript(`
local reservation = ARGV[1]
local tickets = redis.call('SMEMBERS', KEYS[6])
if #tickets == 0 then
    local entries = redis.call('HGETALL', KEYS[4])
    for index = 1, #entries, 2 do
        if entries[index + 1] == reservation then
            table.insert(tickets, entries[index])
        end
    end
    if #tickets == 0 then
        return 0
    end
end
for _, ticket in ipairs(tickets) do
    if redis.call('HGET', KEYS[1], ticket) ~= 'RESERVED'
       or redis.call('HGET', KEYS[4], ticket) ~= reservation
       or not redis.call('HGET', KEYS[2], ticket)
       or not redis.call('HGET', KEYS[3], ticket) then
        return redis.error_reply('RESERVATION_CONFLICT')
    end
end
for _, ticket in ipairs(tickets) do
    local pool = redis.call('HGET', KEYS[2], ticket)
    local score = redis.call('HGET', KEYS[3], ticket)
    redis.call('HSET', KEYS[1], ticket, 'QUEUED')
    redis.call('HDEL', KEYS[4], ticket)
    redis.call('ZREM', KEYS[5], ticket)
    redis.call('ZADD', pool, score, ticket)
end
redis.call('DEL', KEYS[6])
return #tickets
`)

var finalizeScript = redisclient.NewScript(`
local reservation = ARGV[1]
local match_id = ARGV[2]
local tickets = redis.call('SMEMBERS', KEYS[6])
if #tickets == 0 then
    local entries = redis.call('HGETALL', KEYS[4])
    for index = 1, #entries, 2 do
        if entries[index + 1] == reservation then
            table.insert(tickets, entries[index])
        end
    end
end
for _, ticket in ipairs(tickets) do
    if redis.call('HGET', KEYS[1], ticket) == 'RESERVED'
       and redis.call('HGET', KEYS[4], ticket) == reservation then
        redis.call('HSET', KEYS[1], ticket, 'ASSIGNED')
        redis.call('HSET', KEYS[7], ticket, match_id)
        redis.call('HDEL', KEYS[4], ticket)
        redis.call('ZREM', KEYS[5], ticket)
    end
end
redis.call('DEL', KEYS[6])
return #tickets
`)

var recoverScript = redisclient.NewScript(`
local now = ARGV[1]
local limit = tonumber(ARGV[2])
local tickets = redis.call('ZRANGEBYSCORE', KEYS[5], '-inf', now, 'LIMIT', 0, limit)
local recovered = 0
for _, ticket in ipairs(tickets) do
    local reservation = redis.call('HGET', KEYS[4], ticket)
    local pool = redis.call('HGET', KEYS[2], ticket)
    local score = redis.call('HGET', KEYS[3], ticket)
    if redis.call('HGET', KEYS[1], ticket) == 'RESERVED' and reservation and pool and score then
        redis.call('HSET', KEYS[1], ticket, 'QUEUED')
        redis.call('HDEL', KEYS[4], ticket)
        redis.call('ZADD', pool, score, ticket)
        redis.call('SREM', KEYS[8] .. reservation, ticket)
        recovered = recovered + 1
    end
    redis.call('ZREM', KEYS[5], ticket)
end
return recovered
`)

func (q *Queue) ReserveAll(
	ctx context.Context,
	ticketIDs []string,
	reservationID string,
	expiresAt, now time.Time,
) ([]*domain.MatchTicket, error) {
	reservationID = strings.TrimSpace(reservationID)
	if reservationID == "" || len(ticketIDs) == 0 || !expiresAt.After(now) {
		return nil, application.ErrReservationConflict
	}
	arguments := make([]any, 0, len(ticketIDs)+3)
	arguments = append(arguments, reservationID, expiresAt.UnixMilli(), max(1, expiresAt.Sub(now).Milliseconds()))
	seen := make(map[string]struct{}, len(ticketIDs))
	for _, ticketID := range ticketIDs {
		ticketID = strings.TrimSpace(ticketID)
		if ticketID == "" {
			return nil, application.ErrReservationConflict
		}
		if _, duplicate := seen[ticketID]; duplicate {
			return nil, application.ErrReservationConflict
		}
		seen[ticketID] = struct{}{}
		arguments = append(arguments, ticketID)
	}
	if _, err := reserveScript.Run(ctx, q.client, q.scriptKeys(reservationID), arguments...).Result(); err != nil {
		return nil, redisReservationError(err)
	}
	return q.ticketsByID(ctx, ticketIDs, domain.TicketStateReserved, reservationID, expiresAt, "")
}

func (q *Queue) ReleaseAll(ctx context.Context, reservationID string, _ time.Time) error {
	reservationID = strings.TrimSpace(reservationID)
	if reservationID == "" {
		return application.ErrReservationConflict
	}
	if _, err := releaseScript.Run(ctx, q.client, q.scriptKeys(reservationID), reservationID).Result(); err != nil {
		return redisReservationError(err)
	}
	return nil
}

func (q *Queue) AssignAll(
	ctx context.Context,
	reservationID, matchID string,
	now time.Time,
) ([]*domain.MatchTicket, error) {
	ticketIDs, err := q.client.SMembers(ctx, q.reservationKey(reservationID)).Result()
	if err != nil {
		return nil, fmt.Errorf("read Redis reservation: %w", err)
	}
	if len(ticketIDs) == 0 {
		return nil, application.ErrReservationConflict
	}
	if err := q.FinalizeAssignment(ctx, reservationID, matchID, now); err != nil {
		return nil, err
	}
	return q.ticketsByID(ctx, ticketIDs, domain.TicketStateAssigned, reservationID, time.Time{}, matchID)
}

func (q *Queue) FinalizeAssignment(ctx context.Context, reservationID, matchID string, _ time.Time) error {
	reservationID = strings.TrimSpace(reservationID)
	matchID = strings.TrimSpace(matchID)
	if reservationID == "" || matchID == "" {
		return application.ErrReservationConflict
	}
	if _, err := finalizeScript.Run(
		ctx, q.client, q.scriptKeys(reservationID), reservationID, matchID,
	).Result(); err != nil {
		return redisReservationError(err)
	}
	return nil
}

func (q *Queue) RecoverExpiredReservations(ctx context.Context, now time.Time) (int, error) {
	result, err := recoverScript.Run(
		ctx, q.client, q.scriptKeys(""), now.UnixMilli(), 1000,
	).Result()
	if err != nil {
		return 0, fmt.Errorf("recover Redis reservations: %w", err)
	}
	count, err := strconv.Atoi(fmt.Sprint(result))
	if err != nil {
		return 0, fmt.Errorf("decode Redis recovery count: %w", err)
	}
	return count, nil
}

func (q *Queue) ticketsByID(
	ctx context.Context,
	ticketIDs []string,
	state domain.TicketState,
	reservationID string,
	expiresAt time.Time,
	matchID string,
) ([]*domain.MatchTicket, error) {
	values, err := q.client.HMGet(ctx, q.ticketsKey(), ticketIDs...).Result()
	if err != nil {
		return nil, err
	}
	result := make([]*domain.MatchTicket, 0, len(ticketIDs))
	for index, value := range values {
		if value == nil {
			return nil, application.ErrReservationConflict
		}
		var payload storedTicket
		if err := json.Unmarshal([]byte(fmt.Sprint(value)), &payload); err != nil {
			return nil, err
		}
		payload.State = state
		payload.ReservationID = reservationID
		payload.ReservationExpiresAt = expiresAt
		payload.MatchID = matchID
		if state == domain.TicketStateAssigned && payload.ReservationExpiresAt.IsZero() {
			// The durable PostgreSQL copy is authoritative for the original TTL.
			payload.ReservationExpiresAt = payload.UpdatedAt.Add(time.Millisecond)
		}
		ticket, err := payload.restore()
		if err != nil {
			return nil, fmt.Errorf("restore Redis ticket %s: %w", ticketIDs[index], err)
		}
		result = append(result, ticket)
	}
	return result, nil
}

func (q *Queue) scriptKeys(reservationID string) []string {
	return []string{
		q.statesKey(), q.ticketPoolsKey(), q.ticketScoresKey(), q.reservationsKey(),
		q.expiryKey(), q.reservationKey(reservationID), q.matchesKey(), q.base + ":reservation:",
	}
}

func redisReservationError(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "RESERVATION_CONFLICT") || errors.Is(err, redisclient.Nil) {
		return application.ErrReservationConflict
	}
	return fmt.Errorf("Redis reservation: %w", err)
}
