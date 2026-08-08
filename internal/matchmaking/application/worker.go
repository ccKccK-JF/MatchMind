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

var (
	ErrNoMatchAvailable       = errors.New("no match available")
	ErrNoServerCapacity       = errors.New("no game server capacity")
	ErrNoSuitableServerRegion = errors.New("no suitable game server region")
)

type MatchQueue interface {
	PoolKeys(ctx context.Context) ([]domain.PoolKey, error)
	QueueSnapshot(ctx context.Context, key domain.PoolKey, limit int) ([]*domain.MatchTicket, error)
	ReserveAll(ctx context.Context, ticketIDs []string, reservationID string, expiresAt, now time.Time) ([]*domain.MatchTicket, error)
	ReleaseAll(ctx context.Context, reservationID string, now time.Time) error
	AssignAll(ctx context.Context, reservationID, matchID string, now time.Time) ([]*domain.MatchTicket, error)
	RecoverExpiredReservations(ctx context.Context, now time.Time) (int, error)
}

type IneligibleTicketCanceller interface {
	Cancel(ctx context.Context, ticketID, playerID, idempotencyKey string, now time.Time) (*domain.MatchTicket, error)
}

type PlayerEligibilityReader interface {
	CheckPlayersEligibility(ctx context.Context, playerIDs []string) (map[string]bool, error)
}

type MatchAssignmentCoordinator interface {
	AssignReservedTickets(ctx context.Context, reservationID string, match *domain.Match, now time.Time) error
}

type AssignmentFinalizer interface {
	FinalizeAssignment(ctx context.Context, reservationID, matchID string, now time.Time) error
}

type Allocation struct {
	Address string
	Token   string
}

type RegionCapacity struct {
	Region           string
	AvailableServers int
}

type ServerAllocator interface {
	Capacities(ctx context.Context) ([]RegionCapacity, error)
	Allocate(ctx context.Context, matchID, region string) (Allocation, error)
}

type ServerReleaser interface {
	Release(ctx context.Context, matchID, region string) error
}

type WorkerMetrics interface {
	SetQueueSize(int)
	IncMatchAttempt()
	IncMatchSuccess()
	IncMatchFailure()
	IncReservationConflict()
	ObserveWaitSeconds(float64)
	ObserveQualityScore(float64)
	ObserveTeamFormation(domain.TeamAlgorithm, float64, float64)
	ObserveWorkerDuration(float64)
}

type noopWorkerMetrics struct{}

func (noopWorkerMetrics) SetQueueSize(int)            {}
func (noopWorkerMetrics) IncMatchAttempt()            {}
func (noopWorkerMetrics) IncMatchSuccess()            {}
func (noopWorkerMetrics) IncMatchFailure()            {}
func (noopWorkerMetrics) IncReservationConflict()     {}
func (noopWorkerMetrics) ObserveWaitSeconds(float64)  {}
func (noopWorkerMetrics) ObserveQualityScore(float64) {}
func (noopWorkerMetrics) ObserveTeamFormation(domain.TeamAlgorithm, float64, float64) {
}
func (noopWorkerMetrics) ObserveWorkerDuration(float64) {}

type Worker struct {
	queue       MatchQueue
	matches     MatchRepository
	allocator   ServerAllocator
	eligibility PlayerEligibilityReader
	canceller   IneligibleTicketCanceller
	policy      domain.MatchPolicy
	policies    PolicySelector
	idGenerator platformid.Generator
	clock       func() time.Time
	metrics     WorkerMetrics
}

func NewWorker(
	queue MatchQueue,
	matches MatchRepository,
	allocator ServerAllocator,
	eligibility PlayerEligibilityReader,
	policy domain.MatchPolicy,
	idGenerator platformid.Generator,
	clock func() time.Time,
) (*Worker, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if eligibility == nil {
		return nil, ErrPlayerServiceUnavailable
	}
	canceller, ok := queue.(IneligibleTicketCanceller)
	if !ok {
		return nil, errors.New("match queue cannot cancel ineligible tickets")
	}
	if idGenerator == nil {
		idGenerator = platformid.UUID
	}
	if clock == nil {
		clock = time.Now
	}
	return &Worker{
		queue: queue, matches: matches, allocator: allocator, eligibility: eligibility, canceller: canceller, policy: policy,
		idGenerator: idGenerator, clock: clock, metrics: noopWorkerMetrics{},
	}, nil
}

func (w *Worker) SetMetrics(metrics WorkerMetrics) {
	if metrics == nil {
		w.metrics = noopWorkerMetrics{}
		return
	}
	w.metrics = metrics
}

func (w *Worker) SetPolicySelector(selector PolicySelector) {
	w.policies = selector
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
	startedAt := time.Now()
	defer func() { w.metrics.ObserveWorkerDuration(time.Since(startedAt).Seconds()) }()

	now := w.clock()
	if _, err := w.queue.RecoverExpiredReservations(ctx, now); err != nil {
		return nil, err
	}
	if sizer, ok := w.queue.(interface {
		QueueSize(context.Context) (int, error)
	}); ok {
		if size, err := sizer.QueueSize(ctx); err == nil {
			w.metrics.SetQueueSize(size)
		}
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
		case errors.Is(err, ErrNoMatchAvailable), errors.Is(err, ErrNoServerCapacity), errors.Is(err, ErrNoSuitableServerRegion),
			errors.Is(err, engine.ErrInsufficientPlayers), errors.Is(err, engine.ErrNoValidTeamSplit):
			continue
		default:
			return nil, err
		}
	}
	return nil, ErrNoMatchAvailable
}

func (w *Worker) tryPool(ctx context.Context, poolKey domain.PoolKey, now time.Time) (result *domain.Match, resultErr error) {
	candidateLimit := w.policy.CandidateLimit
	if w.policies != nil && w.policies.MaxCandidateLimit() > candidateLimit {
		candidateLimit = w.policies.MaxCandidateLimit()
	}
	tickets, err := w.queue.QueueSnapshot(ctx, poolKey, candidateLimit)
	if err != nil {
		return nil, err
	}
	if len(tickets) == 0 {
		return nil, ErrNoMatchAvailable
	}
	tickets, err = w.filterEligibleTickets(ctx, tickets, now)
	if err != nil {
		return nil, err
	}
	if len(tickets) == 0 {
		return nil, ErrNoMatchAvailable
	}
	selection := PolicySelection{Policy: w.policy, Variant: "default", Bucket: -1}
	if w.policies != nil {
		selection = w.policies.SelectPolicy(tickets[0].PlayerID())
	}
	policy := selection.Policy
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if len(tickets) < policy.TeamSize*2 {
		return nil, ErrNoMatchAvailable
	}
	w.metrics.IncMatchAttempt()
	defer func() {
		if resultErr != nil {
			w.metrics.IncMatchFailure()
		}
	}()
	candidates, err := engine.GenerateCandidates(tickets, now, policy)
	if err != nil {
		return nil, err
	}
	regionSelection, err := selectServerRegion(ctx, w.allocator, candidates, now, policy)
	if err != nil {
		return nil, err
	}
	formation := regionSelection.Formation
	quality := regionSelection.Quality
	search := regionSelection.Diagnostics
	w.metrics.ObserveTeamFormation(search.Algorithm, search.Duration.Seconds(), quality.TotalScore)
	w.metrics.ObserveQualityScore(quality.TotalScore)
	if quality.TotalScore < policy.MinQualityScore {
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
	selectedTickets := formationTickets(formation)
	selectedTickets, err = w.filterEligibleTickets(ctx, selectedTickets, now)
	if err != nil {
		return nil, err
	}
	if len(selectedTickets) != policy.TeamSize*2 {
		return nil, ErrNoMatchAvailable
	}
	if _, err := w.queue.ReserveAll(ctx, ticketIDs, reservationID, now.Add(policy.ReservationTTL), now); err != nil {
		if errors.Is(err, ErrReservationConflict) {
			w.metrics.IncReservationConflict()
		}
		return nil, err
	}

	match, err := domain.NewMatch(domain.NewMatchParams{
		ID:            matchID,
		Mode:          poolKey.Mode,
		TeamA:         matchTeamFromEngine(matchID+"-a", formation.TeamA),
		TeamB:         matchTeamFromEngine(matchID+"-b", formation.TeamB),
		ServerRegion:  regionSelection.Region,
		PolicyVersion: policy.Version,
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
	if err := w.matches.Update(ctx, match); err != nil {
		_ = w.queue.ReleaseAll(ctx, reservationID, now)
		return nil, err
	}

	allocation, err := w.allocator.Allocate(ctx, match.ID(), match.ServerRegion())
	if err != nil {
		w.failStoredMatch(ctx, match.ID(), now)
		_ = w.queue.ReleaseAll(ctx, reservationID, now)
		return nil, err
	}
	if err := match.MarkReady(allocation.Address, allocation.Token, now); err != nil {
		w.releaseAllocation(ctx, match.ID(), match.ServerRegion())
		w.failStoredMatch(ctx, match.ID(), now)
		_ = w.queue.ReleaseAll(ctx, reservationID, now)
		return nil, err
	}
	if coordinator, ok := w.matches.(MatchAssignmentCoordinator); ok {
		if err := coordinator.AssignReservedTickets(ctx, reservationID, match, now); err != nil {
			w.releaseAllocation(ctx, match.ID(), match.ServerRegion())
			w.failStoredMatch(ctx, match.ID(), now)
			_ = w.queue.ReleaseAll(ctx, reservationID, now)
			return nil, err
		}
		if finalizer, ok := w.queue.(AssignmentFinalizer); ok {
			if err := finalizer.FinalizeAssignment(ctx, reservationID, match.ID(), now); err != nil {
				slog.Warn("durable Match assignment committed but queue finalization failed", "error", err, "match_id", match.ID())
			}
		}
	} else {
		if _, err := w.queue.AssignAll(ctx, reservationID, match.ID(), now); err != nil {
			w.releaseAllocation(ctx, match.ID(), match.ServerRegion())
			w.failStoredMatch(ctx, match.ID(), now)
			_ = w.queue.ReleaseAll(ctx, reservationID, now)
			return nil, err
		}
		if err := w.matches.Update(ctx, match); err != nil {
			return nil, err
		}
	}
	w.metrics.IncMatchSuccess()
	for _, ticket := range ticketsForFormation(formation) {
		w.metrics.ObserveWaitSeconds(now.Sub(ticket.CreatedAt()).Seconds())
	}
	slog.Info(
		"match created",
		"match_id", match.ID(),
		"reservation_id", reservationID,
		"policy_version", policy.Version,
		"experiment_id", selection.ExperimentID,
		"experiment_variant", selection.Variant,
		"experiment_bucket", selection.Bucket,
		"quality_score", quality.TotalScore,
		"team_algorithm", search.Algorithm,
		"formation_duration_seconds", search.Duration.Seconds(),
		"formations_evaluated", search.FormationsEvaluated,
		"server_region", match.ServerRegion(),
		"region_score", regionSelection.Score,
		"region_available_servers", regionSelection.AvailableServers,
	)
	return match.Clone(), nil
}

func (w *Worker) filterEligibleTickets(
	ctx context.Context,
	tickets []*domain.MatchTicket,
	now time.Time,
) ([]*domain.MatchTicket, error) {
	playerIDs := make([]string, 0, len(tickets))
	for _, ticket := range tickets {
		playerIDs = append(playerIDs, ticket.PlayerID())
	}
	eligible, err := w.eligibility.CheckPlayersEligibility(ctx, playerIDs)
	if err != nil {
		return nil, err
	}
	result := make([]*domain.MatchTicket, 0, len(tickets))
	for _, ticket := range tickets {
		if eligible[ticket.PlayerID()] {
			result = append(result, ticket)
			continue
		}
		_, cancelErr := w.canceller.Cancel(
			ctx, ticket.ID(), ticket.PlayerID(), "system-ineligible-"+ticket.ID(), now,
		)
		if cancelErr != nil && !errors.Is(cancelErr, ErrTicketNotFound) &&
			!errors.Is(cancelErr, domain.ErrIllegalStateTransition) && !errors.Is(cancelErr, ErrReservationConflict) {
			return nil, cancelErr
		}
	}
	return result, nil
}

func (w *Worker) releaseAllocation(ctx context.Context, matchID, region string) {
	if releaser, ok := w.allocator.(ServerReleaser); ok {
		if err := releaser.Release(ctx, matchID, region); err != nil {
			slog.Warn("release game server allocation failed", "error", err, "match_id", matchID, "server_region", region)
		}
	}
}

func (w *Worker) failStoredMatch(ctx context.Context, matchID string, now time.Time) {
	stored, err := w.matches.Get(ctx, matchID)
	if err != nil {
		return
	}
	if err := stored.Fail(now); err != nil {
		return
	}
	_ = w.matches.Update(ctx, stored)
}

func ticketsForFormation(formation engine.TeamFormation) []*domain.MatchTicket {
	result := make([]*domain.MatchTicket, 0, 10)
	for _, team := range []engine.Team{formation.TeamA, formation.TeamB} {
		for _, player := range team.Players {
			result = append(result, player.Ticket)
		}
	}
	return result
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

func formationTickets(formation engine.TeamFormation) []*domain.MatchTicket {
	result := make([]*domain.MatchTicket, 0, 10)
	for _, team := range []engine.Team{formation.TeamA, formation.TeamB} {
		for _, player := range team.Players {
			result = append(result, player.Ticket)
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
