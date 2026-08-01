package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/engine"
	platformid "github.com/ccKccK-JF/MatchMind/internal/platform/id"
)

var ErrNoMatchAvailable = errors.New("no match available")

type MatchQueue interface {
	PoolKeys(ctx context.Context) ([]domain.PoolKey, error)
	QueueSnapshot(ctx context.Context, key domain.PoolKey, limit int) ([]*domain.MatchTicket, error)
	ReserveAll(ctx context.Context, ticketIDs []string, reservationID string, expiresAt, now time.Time) ([]*domain.MatchTicket, error)
	ReleaseAll(ctx context.Context, reservationID string, now time.Time) error
	AssignAll(ctx context.Context, reservationID, matchID string, now time.Time) ([]*domain.MatchTicket, error)
	RecoverExpiredReservations(ctx context.Context, now time.Time) (int, error)
}

type Allocation struct {
	Address string
	Token   string
}

type ServerAllocator interface {
	Allocate(ctx context.Context, matchID, region string) (Allocation, error)
}

type Worker struct {
	queue       MatchQueue
	matches     MatchRepository
	allocator   ServerAllocator
	policy      domain.MatchPolicy
	idGenerator platformid.Generator
	clock       func() time.Time
}

func NewWorker(
	queue MatchQueue,
	matches MatchRepository,
	allocator ServerAllocator,
	policy domain.MatchPolicy,
	idGenerator platformid.Generator,
	clock func() time.Time,
) (*Worker, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if idGenerator == nil {
		idGenerator = platformid.UUID
	}
	if clock == nil {
		clock = time.Now
	}
	return &Worker{
		queue: queue, matches: matches, allocator: allocator, policy: policy,
		idGenerator: idGenerator, clock: clock,
	}, nil
}

func (w *Worker) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, err := w.RunOnce(ctx)
			if err != nil && !errors.Is(err, ErrNoMatchAvailable) && !errors.Is(err, context.Canceled) {
				slog.Error("matchmaking worker iteration failed", "error", err, "policy_version", w.policy.Version)
			}
		}
	}
}

func (w *Worker) RunOnce(ctx context.Context) (*domain.Match, error) {
	now := w.clock()
	if _, err := w.queue.RecoverExpiredReservations(ctx, now); err != nil {
		return nil, err
	}
	poolKeys, err := w.queue.PoolKeys(ctx)
	if err != nil {
		return nil, err
	}
	for _, poolKey := range poolKeys {
		match, err := w.tryPool(ctx, poolKey, now)
		switch {
		case err == nil:
			return match, nil
		case errors.Is(err, ErrNoMatchAvailable), errors.Is(err, engine.ErrInsufficientPlayers), errors.Is(err, engine.ErrNoValidTeamSplit):
			continue
		default:
			return nil, err
		}
	}
	return nil, ErrNoMatchAvailable
}

func (w *Worker) tryPool(ctx context.Context, poolKey domain.PoolKey, now time.Time) (*domain.Match, error) {
	tickets, err := w.queue.QueueSnapshot(ctx, poolKey, w.policy.CandidateLimit)
	if err != nil {
		return nil, err
	}
	if len(tickets) < w.policy.TeamSize*2 {
		return nil, ErrNoMatchAvailable
	}
	candidates, err := engine.GenerateCandidates(tickets, now, w.policy)
	if err != nil {
		return nil, err
	}
	formation, err := engine.FormTeams(candidates, w.policy)
	if err != nil {
		return nil, err
	}
	quality, err := engine.EvaluateQuality(formation, poolKey.Region, now, w.policy)
	if err != nil {
		return nil, err
	}
	if quality.TotalScore < w.policy.MinQualityScore {
		return nil, ErrNoMatchAvailable
	}

	reservationID, err := w.idGenerator()
	if err != nil {
		return nil, fmt.Errorf("generate reservation id: %w", err)
	}
	matchID, err := w.idGenerator()
	if err != nil {
		return nil, fmt.Errorf("generate match id: %w", err)
	}
	ticketIDs := formationTicketIDs(formation)
	if _, err := w.queue.ReserveAll(ctx, ticketIDs, reservationID, now.Add(w.policy.ReservationTTL), now); err != nil {
		return nil, err
	}

	match, err := domain.NewMatch(domain.NewMatchParams{
		ID:            matchID,
		Mode:          poolKey.Mode,
		TeamA:         matchTeamFromEngine(matchID+"-a", formation.TeamA),
		TeamB:         matchTeamFromEngine(matchID+"-b", formation.TeamB),
		ServerRegion:  poolKey.Region,
		PolicyVersion: w.policy.Version,
		Quality:       matchQualityFromEngine(quality),
		CreatedAt:     now,
	})
	if err != nil {
		_ = w.queue.ReleaseAll(ctx, reservationID, now)
		return nil, err
	}
	if err := w.matches.Create(ctx, match); err != nil {
		_ = w.queue.ReleaseAll(ctx, reservationID, now)
		return nil, err
	}
	if err := match.StartAllocation(now); err != nil {
		_ = w.queue.ReleaseAll(ctx, reservationID, now)
		return nil, err
	}
	_ = w.matches.Update(ctx, match)

	allocation, err := w.allocator.Allocate(ctx, match.ID(), match.ServerRegion())
	if err != nil {
		_ = match.Fail(now)
		_ = w.matches.Update(ctx, match)
		_ = w.queue.ReleaseAll(ctx, reservationID, now)
		return nil, err
	}
	if err := match.MarkReady(allocation.Address, allocation.Token, now); err != nil {
		_ = match.Fail(now)
		_ = w.matches.Update(ctx, match)
		_ = w.queue.ReleaseAll(ctx, reservationID, now)
		return nil, err
	}
	if _, err := w.queue.AssignAll(ctx, reservationID, match.ID(), now); err != nil {
		_ = match.Fail(now)
		_ = w.matches.Update(ctx, match)
		_ = w.queue.ReleaseAll(ctx, reservationID, now)
		return nil, err
	}
	if err := w.matches.Update(ctx, match); err != nil {
		return nil, err
	}
	return match.Clone(), nil
}

func formationTicketIDs(formation engine.TeamFormation) []string {
	result := make([]string, 0, 10)
	for _, team := range []engine.Team{formation.TeamA, formation.TeamB} {
		for _, player := range team.Players {
			result = append(result, player.Ticket.ID())
		}
	}
	return result
}

func matchTeamFromEngine(id string, team engine.Team) domain.MatchTeam {
	result := domain.MatchTeam{ID: id, AverageRating: team.AverageRating, Players: make([]domain.MatchPlayer, 0, len(team.Players))}
	for _, player := range team.Players {
		result.Players = append(result.Players, domain.MatchPlayer{
			PlayerID: player.Ticket.PlayerID(), TicketID: player.Ticket.ID(), PartyID: player.Ticket.PartyID(),
			Role: player.Role, Rating: player.Ticket.Rating(),
		})
	}
	return result
}

func matchQualityFromEngine(quality engine.MatchQuality) domain.MatchQuality {
	return domain.MatchQuality{
		TotalScore: quality.TotalScore, SkillScore: quality.SkillScore, RoleScore: quality.RoleScore,
		LatencyScore: quality.LatencyScore, PartyScore: quality.PartyScore, WaitScore: quality.WaitScore,
		PredictedWinRateA: quality.PredictedWinRateA, PredictedWinRateB: quality.PredictedWinRateB,
		Reasons: append([]string(nil), quality.Reasons...),
	}
}
