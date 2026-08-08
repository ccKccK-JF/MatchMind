package redis

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	redisclient "github.com/redis/go-redis/v9"
)

type Queue struct {
	client redisclient.UniversalClient
	base   string
}

type storedTicket struct {
	ID                   string             `json:"id"`
	PlayerID             string             `json:"player_id"`
	PartyID              string             `json:"party_id"`
	Mode                 string             `json:"mode"`
	ClientVersion        string             `json:"client_version"`
	Region               string             `json:"region"`
	Rating               float64            `json:"rating"`
	BehaviorScore        float64            `json:"behavior_score"`
	HeroProficiency      map[string]float64 `json:"hero_proficiency"`
	PreferredRoles       []domain.Role      `json:"preferred_roles"`
	RegionLatency        map[string]int     `json:"region_latency"`
	State                domain.TicketState `json:"state"`
	CreatedAt            time.Time          `json:"created_at"`
	UpdatedAt            time.Time          `json:"updated_at"`
	ReservationID        string             `json:"reservation_id,omitempty"`
	ReservationExpiresAt time.Time          `json:"reservation_expires_at,omitempty"`
	MatchID              string             `json:"match_id,omitempty"`
}

func NewQueue(client redisclient.UniversalClient, prefix string) *Queue {
	prefix = strings.Trim(strings.TrimSpace(prefix), "{}:")
	if prefix == "" {
		prefix = "matchmind"
	}
	// The shared hash tag keeps every Lua key in one Redis Cluster slot.
	return &Queue{client: client, base: "{" + prefix + "}:matchmaking"}
}

func (q *Queue) Ping(ctx context.Context) error {
	if q == nil || q.client == nil {
		return errors.New("redis queue client is required")
	}
	if err := q.client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping Redis: %w", err)
	}
	return nil
}

func (q *Queue) UpsertTicket(ctx context.Context, ticket *domain.MatchTicket, now time.Time) error {
	if ticket == nil || (ticket.State() != domain.TicketStateQueued && ticket.State() != domain.TicketStateReserved) {
		return domain.ErrInvalidTicket
	}
	encoded, err := json.Marshal(storedTicketFromDomain(ticket))
	if err != nil {
		return fmt.Errorf("encode Redis ticket: %w", err)
	}
	poolToken, poolDefinition, err := encodePool(domain.PoolKey{
		Mode: ticket.Mode(), ClientVersion: ticket.ClientVersion(), Region: ticket.Region(),
	})
	if err != nil {
		return err
	}
	poolKey := q.poolKey(poolToken)
	score := float64(ticket.CreatedAt().UnixMilli())
	pipe := q.client.TxPipeline()
	pipe.HSet(ctx, q.ticketsKey(), ticket.ID(), encoded)
	pipe.HSet(ctx, q.statesKey(), ticket.ID(), string(ticket.State()))
	pipe.HSet(ctx, q.ticketPoolsKey(), ticket.ID(), poolKey)
	pipe.HSet(ctx, q.ticketScoresKey(), ticket.ID(), ticket.CreatedAt().UnixMilli())
	pipe.HSet(ctx, q.poolDefinitionsKey(), poolToken, poolDefinition)
	pipe.SAdd(ctx, q.poolTokensKey(), poolToken)
	if ticket.State() == domain.TicketStateQueued {
		pipe.ZAdd(ctx, poolKey, redisclient.Z{Score: score, Member: ticket.ID()})
		pipe.HDel(ctx, q.reservationsKey(), ticket.ID())
		pipe.ZRem(ctx, q.expiryKey(), ticket.ID())
	} else {
		remaining := ticket.ReservationExpiresAt().Sub(now)
		if remaining <= 0 {
			remaining = time.Millisecond
		}
		pipe.ZRem(ctx, poolKey, ticket.ID())
		pipe.HSet(ctx, q.reservationsKey(), ticket.ID(), ticket.ReservationID())
		pipe.ZAdd(ctx, q.expiryKey(), redisclient.Z{
			Score: float64(ticket.ReservationExpiresAt().UnixMilli()), Member: ticket.ID(),
		})
		pipe.SAdd(ctx, q.reservationKey(ticket.ReservationID()), ticket.ID())
		pipe.PExpire(ctx, q.reservationKey(ticket.ReservationID()), remaining)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("upsert Redis ticket: %w", err)
	}
	return nil
}

func (q *Queue) RemoveTicket(ctx context.Context, ticket *domain.MatchTicket) error {
	if ticket == nil {
		return domain.ErrInvalidTicket
	}
	poolKey, _ := q.client.HGet(ctx, q.ticketPoolsKey(), ticket.ID()).Result()
	reservationID, _ := q.client.HGet(ctx, q.reservationsKey(), ticket.ID()).Result()
	pipe := q.client.TxPipeline()
	if poolKey != "" {
		pipe.ZRem(ctx, poolKey, ticket.ID())
	}
	if reservationID != "" {
		pipe.SRem(ctx, q.reservationKey(reservationID), ticket.ID())
	}
	pipe.HSet(ctx, q.statesKey(), ticket.ID(), string(ticket.State()))
	pipe.HDel(ctx, q.reservationsKey(), ticket.ID())
	pipe.ZRem(ctx, q.expiryKey(), ticket.ID())
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("remove Redis ticket: %w", err)
	}
	return nil
}

func (q *Queue) PoolKeys(ctx context.Context) ([]domain.PoolKey, error) {
	tokens, err := q.client.SMembers(ctx, q.poolTokensKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("list Redis pools: %w", err)
	}
	result := make([]domain.PoolKey, 0, len(tokens))
	for _, token := range tokens {
		count, err := q.client.ZCard(ctx, q.poolKey(token)).Result()
		if err != nil {
			return nil, fmt.Errorf("count Redis pool: %w", err)
		}
		if count == 0 {
			continue
		}
		definition, err := q.client.HGet(ctx, q.poolDefinitionsKey(), token).Bytes()
		if err != nil {
			return nil, fmt.Errorf("read Redis pool definition: %w", err)
		}
		var key domain.PoolKey
		if err := json.Unmarshal(definition, &key); err != nil {
			return nil, fmt.Errorf("decode Redis pool definition: %w", err)
		}
		result = append(result, key)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Mode != result[j].Mode {
			return result[i].Mode < result[j].Mode
		}
		if result[i].ClientVersion != result[j].ClientVersion {
			return result[i].ClientVersion < result[j].ClientVersion
		}
		return result[i].Region < result[j].Region
	})
	return result, nil
}

func (q *Queue) QueueSnapshot(ctx context.Context, key domain.PoolKey, limit int) ([]*domain.MatchTicket, error) {
	token, _, err := encodePool(key)
	if err != nil {
		return nil, err
	}
	stop := int64(-1)
	if limit > 0 {
		stop = int64(limit - 1)
	}
	ids, err := q.client.ZRange(ctx, q.poolKey(token), 0, stop).Result()
	if err != nil {
		return nil, fmt.Errorf("read Redis queue: %w", err)
	}
	if len(ids) == 0 {
		return nil, nil
	}
	values, err := q.client.HMGet(ctx, q.ticketsKey(), ids...).Result()
	if err != nil {
		return nil, fmt.Errorf("read Redis ticket snapshots: %w", err)
	}
	result := make([]*domain.MatchTicket, 0, len(ids))
	for index, value := range values {
		if value == nil {
			continue
		}
		var payload storedTicket
		if err := json.Unmarshal([]byte(fmt.Sprint(value)), &payload); err != nil {
			return nil, fmt.Errorf("decode Redis ticket %s: %w", ids[index], err)
		}
		payload.State = domain.TicketStateQueued
		payload.ReservationID = ""
		payload.ReservationExpiresAt = time.Time{}
		payload.MatchID = ""
		ticket, err := payload.restore()
		if err != nil {
			return nil, err
		}
		result = append(result, ticket)
	}
	return result, nil
}

func (q *Queue) QueueSize(ctx context.Context) (int, error) {
	tokens, err := q.client.SMembers(ctx, q.poolTokensKey()).Result()
	if err != nil {
		return 0, err
	}
	pipe := q.client.Pipeline()
	commands := make([]*redisclient.IntCmd, 0, len(tokens))
	for _, token := range tokens {
		commands = append(commands, pipe.ZCard(ctx, q.poolKey(token)))
	}
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redisclient.Nil) {
		return 0, fmt.Errorf("count Redis queues: %w", err)
	}
	total := int64(0)
	for _, command := range commands {
		total += command.Val()
	}
	return int(total), nil
}

func storedTicketFromDomain(ticket *domain.MatchTicket) storedTicket {
	return storedTicket{
		ID: ticket.ID(), PlayerID: ticket.PlayerID(), PartyID: ticket.PartyID(),
		Mode: ticket.Mode(), ClientVersion: ticket.ClientVersion(), Region: ticket.Region(),
		Rating: ticket.Rating(), BehaviorScore: ticket.BehaviorScore(), HeroProficiency: ticket.HeroProficiency(),
		PreferredRoles: ticket.PreferredRoles(), RegionLatency: ticket.RegionLatency(),
		State: ticket.State(), CreatedAt: ticket.CreatedAt(), UpdatedAt: ticket.UpdatedAt(),
		ReservationID: ticket.ReservationID(), ReservationExpiresAt: ticket.ReservationExpiresAt(), MatchID: ticket.MatchID(),
	}
}

func (ticket storedTicket) restore() (*domain.MatchTicket, error) {
	return domain.RestoreTicket(domain.TicketSnapshot{
		ID: ticket.ID, PlayerID: ticket.PlayerID, PartyID: ticket.PartyID,
		Mode: ticket.Mode, ClientVersion: ticket.ClientVersion, Region: ticket.Region,
		Rating: ticket.Rating, BehaviorScore: ticket.BehaviorScore, HeroProficiency: ticket.HeroProficiency,
		PreferredRoles: ticket.PreferredRoles, RegionLatency: ticket.RegionLatency,
		State: ticket.State, CreatedAt: ticket.CreatedAt, UpdatedAt: ticket.UpdatedAt,
		ReservationID: ticket.ReservationID, ReservationExpiresAt: ticket.ReservationExpiresAt, MatchID: ticket.MatchID,
	})
}

func encodePool(key domain.PoolKey) (string, []byte, error) {
	definition, err := json.Marshal(key)
	if err != nil {
		return "", nil, fmt.Errorf("encode pool key: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(definition), definition, nil
}

func (q *Queue) ticketsKey() string              { return q.base + ":tickets" }
func (q *Queue) statesKey() string               { return q.base + ":states" }
func (q *Queue) ticketPoolsKey() string          { return q.base + ":ticket-pools" }
func (q *Queue) ticketScoresKey() string         { return q.base + ":ticket-scores" }
func (q *Queue) reservationsKey() string         { return q.base + ":reservations" }
func (q *Queue) matchesKey() string              { return q.base + ":matches" }
func (q *Queue) expiryKey() string               { return q.base + ":reservation-expiry" }
func (q *Queue) poolTokensKey() string           { return q.base + ":pool-tokens" }
func (q *Queue) poolDefinitionsKey() string      { return q.base + ":pool-definitions" }
func (q *Queue) poolKey(token string) string     { return q.base + ":pool:" + token }
func (q *Queue) reservationKey(id string) string { return q.base + ":reservation:" + id }

var _ application.MatchQueue = (*Queue)(nil)
