package application

import (
	"context"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/game/hero"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/engine"
)

const (
	DefaultAnalysisLimit = 100
	MaxAnalysisLimit     = 1000
	MaxReplayPolicies    = 16
)

var ErrInvalidAnalysis = errors.New("invalid matchmaking analysis request")

type MatchHistoryFilter struct {
	PolicyVersion string
	Mode          string
	ServerRegion  string
	From          time.Time
	To            time.Time
	Limit         int
}

type MatchHistoryReader interface {
	Get(ctx context.Context, matchID string) (*domain.Match, error)
	ListFinished(ctx context.Context, filter MatchHistoryFilter) ([]*domain.Match, error)
}

type HistoricalTicketReader interface {
	Get(ctx context.Context, ticketID string) (*domain.MatchTicket, error)
}

type MatchQualityObservation struct {
	MatchID              string
	PolicyVersion        string
	Mode                 string
	ServerRegion         string
	PredictedQuality     float64
	ActualQuality        float64
	SkillScore           float64
	RoleScore            float64
	LatencyScore         float64
	PartyScore           float64
	WaitScore            float64
	AverageRating        float64
	SignedQualityError   float64
	AbsoluteQualityError float64
	PredictedWinRateA    float64
	TeamAOutcome         float64
	WinProbabilityBrier  float64
	DurationSeconds      int
	OneSided             bool
	HasAFK               bool
	Surrendered          bool
	CreatedAt            time.Time
}

type PolicyQualitySummary struct {
	PolicyVersion            string
	MatchCount               int
	AveragePredictedQuality  float64
	AverageActualQuality     float64
	MeanSignedQualityError   float64
	MeanAbsoluteQualityError float64
	WinProbabilityBrierScore float64
	TeamAWinRate             float64
	AverageDurationSeconds   float64
	OneSidedRate             float64
	AFKRate                  float64
	SurrenderRate            float64
}

type QualityAnalysis struct {
	Observations []MatchQualityObservation
	Summaries    []PolicyQualitySummary
}

type ReplayRequest struct {
	MatchID           string
	PolicyVersions    []string
	TicketIDs         []string
	CandidatePolicies []domain.MatchPolicy
}

type ReplayPlayer struct {
	PlayerID        string
	TicketID        string
	PartyID         string
	Role            domain.Role
	Rating          float64
	HeroID          string
	HeroProficiency float64
	BehaviorScore   float64
}

type ReplayTeam struct {
	Players       []ReplayPlayer
	AverageRating float64
	RoleScore     float64
}

type ReplayOutcome struct {
	PolicyVersion       string
	Algorithm           domain.TeamAlgorithm
	Matched             bool
	FailureReason       string
	AcceptedTickets     int
	RejectedTickets     int
	CandidateSets       int
	FormationsEvaluated int
	TeamA               ReplayTeam
	TeamB               ReplayTeam
	Quality             domain.MatchQuality
	QualityDelta        float64
	SameTeamSplit       bool
	SameRoleAssignments bool
}

type ReplayReport struct {
	SourceMatchID          string
	SourcePolicyVersion    string
	SourcePredictedQuality float64
	SourceActualQuality    float64
	SourceAbsoluteError    float64
	TicketCount            int
	Outcomes               []ReplayOutcome
}

type AnalysisService struct {
	matches  MatchHistoryReader
	tickets  HistoricalTicketReader
	policies map[string]domain.MatchPolicy
	catalog  interface{ Policies() []domain.MatchPolicy }
}

func (s *AnalysisService) SetPolicyCatalog(catalog interface{ Policies() []domain.MatchPolicy }) {
	s.catalog = catalog
}

func NewAnalysisService(
	matches MatchHistoryReader,
	tickets HistoricalTicketReader,
	policies []domain.MatchPolicy,
) (*AnalysisService, error) {
	if matches == nil || tickets == nil || len(policies) == 0 {
		return nil, ErrInvalidAnalysis
	}
	service := &AnalysisService{
		matches: matches, tickets: tickets, policies: make(map[string]domain.MatchPolicy, len(policies)),
	}
	for _, policy := range policies {
		if err := policy.Validate(); err != nil {
			return nil, err
		}
		if _, duplicate := service.policies[policy.Version]; duplicate {
			return nil, domain.ErrInvalidPolicy
		}
		service.policies[policy.Version] = policy
	}
	return service, nil
}

func (s *AnalysisService) AnalyzeMatchQuality(
	ctx context.Context,
	filter MatchHistoryFilter,
) (QualityAnalysis, error) {
	filter.PolicyVersion = strings.TrimSpace(filter.PolicyVersion)
	filter.Mode = strings.TrimSpace(filter.Mode)
	filter.ServerRegion = strings.ToLower(strings.TrimSpace(filter.ServerRegion))
	if filter.Limit == 0 {
		filter.Limit = DefaultAnalysisLimit
	}
	if filter.Limit < 1 || filter.Limit > MaxAnalysisLimit ||
		(!filter.From.IsZero() && !filter.To.IsZero() && filter.To.Before(filter.From)) {
		return QualityAnalysis{}, ErrInvalidAnalysis
	}
	matches, err := s.matches.ListFinished(ctx, filter)
	if err != nil {
		return QualityAnalysis{}, err
	}
	analysis := QualityAnalysis{Observations: make([]MatchQualityObservation, 0, len(matches))}
	summaries := make(map[string]*PolicyQualitySummary)
	for _, match := range matches {
		result, exists := match.Result()
		if !exists {
			continue
		}
		quality := match.Quality()
		outcomeA := 0.0
		if result.WinningTeam == domain.WinningTeamA {
			outcomeA = 1
		}
		signedError := result.ActualQualityScore - quality.TotalScore
		observation := MatchQualityObservation{
			MatchID: match.ID(), PolicyVersion: match.PolicyVersion(), Mode: match.Mode(),
			ServerRegion: match.ServerRegion(), PredictedQuality: quality.TotalScore,
			ActualQuality: result.ActualQualityScore, SkillScore: quality.SkillScore,
			RoleScore: quality.RoleScore, LatencyScore: quality.LatencyScore,
			PartyScore: quality.PartyScore, WaitScore: quality.WaitScore,
			AverageRating:        (match.TeamA().AverageRating + match.TeamB().AverageRating) / 2,
			SignedQualityError:   signedError,
			AbsoluteQualityError: math.Abs(signedError), PredictedWinRateA: quality.PredictedWinRateA,
			TeamAOutcome: outcomeA, WinProbabilityBrier: square(quality.PredictedWinRateA - outcomeA),
			DurationSeconds: result.DurationSeconds, OneSided: result.OneSided,
			HasAFK: result.HasAFK, Surrendered: result.Surrendered, CreatedAt: match.CreatedAt(),
		}
		analysis.Observations = append(analysis.Observations, observation)
		summary := summaries[match.PolicyVersion()]
		if summary == nil {
			summary = &PolicyQualitySummary{PolicyVersion: match.PolicyVersion()}
			summaries[match.PolicyVersion()] = summary
		}
		accumulateQualitySummary(summary, observation)
	}
	versions := make([]string, 0, len(summaries))
	for version := range summaries {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	for _, version := range versions {
		summary := *summaries[version]
		finalizeQualitySummary(&summary)
		analysis.Summaries = append(analysis.Summaries, summary)
	}
	return analysis, nil
}

func (s *AnalysisService) ReplayHistoricalMatch(ctx context.Context, request ReplayRequest) (ReplayReport, error) {
	request.MatchID = strings.TrimSpace(request.MatchID)
	if request.MatchID == "" {
		return ReplayReport{}, ErrInvalidAnalysis
	}
	source, err := s.matches.Get(ctx, request.MatchID)
	if err != nil {
		return ReplayReport{}, err
	}
	result, finished := source.Result()
	if source.State() != domain.MatchStateFinished || !finished {
		return ReplayReport{}, ErrInvalidAnalysis
	}
	policies, versions, err := s.replayPolicies(request.PolicyVersions, request.CandidatePolicies)
	if err != nil {
		return ReplayReport{}, err
	}
	ticketIDs := replayTicketIDs(source, request.TicketIDs)
	maximumCandidates := maximumCandidateLimit(versions, policies)
	if len(ticketIDs) < domain.DefaultPolicy().TeamSize*2 || len(ticketIDs) > maximumCandidates {
		return ReplayReport{}, ErrInvalidAnalysis
	}
	tickets := make([]*domain.MatchTicket, 0, len(ticketIDs))
	for _, ticketID := range ticketIDs {
		historical, readErr := s.tickets.Get(ctx, ticketID)
		if readErr != nil {
			return ReplayReport{}, readErr
		}
		replay, rebuildErr := rebuildQueuedTicket(historical)
		if rebuildErr != nil {
			return ReplayReport{}, rebuildErr
		}
		tickets = append(tickets, replay)
	}
	sourceQuality := source.Quality()
	report := ReplayReport{
		SourceMatchID: source.ID(), SourcePolicyVersion: source.PolicyVersion(),
		SourcePredictedQuality: sourceQuality.TotalScore, SourceActualQuality: result.ActualQualityScore,
		SourceAbsoluteError: math.Abs(result.ActualQualityScore - sourceQuality.TotalScore),
		TicketCount:         len(tickets), Outcomes: make([]ReplayOutcome, 0, len(versions)),
	}
	for _, version := range versions {
		policy := policies[version]
		outcome := ReplayOutcome{PolicyVersion: version, Algorithm: policy.TeamAlgorithm}
		candidates, candidateErr := engine.GenerateCandidates(tickets, source.CreatedAt(), policy)
		outcome.AcceptedTickets = len(candidates.Tickets)
		outcome.RejectedTickets = len(candidates.Decisions) - len(candidates.Tickets)
		if candidateErr != nil {
			outcome.FailureReason = candidateErr.Error()
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		formation, quality, diagnostics, optimizeErr := engine.OptimizeTeams(
			candidates, source.ServerRegion(), source.CreatedAt(), policy,
		)
		outcome.CandidateSets = diagnostics.CandidateSets
		outcome.FormationsEvaluated = diagnostics.FormationsEvaluated
		if optimizeErr != nil {
			outcome.FailureReason = optimizeErr.Error()
			report.Outcomes = append(report.Outcomes, outcome)
			continue
		}
		outcome.Matched = true
		outcome.TeamA = replayTeam(formation.TeamA)
		outcome.TeamB = replayTeam(formation.TeamB)
		outcome.Quality = replayQuality(quality)
		outcome.QualityDelta = quality.TotalScore - sourceQuality.TotalScore
		outcome.SameTeamSplit = sameTeamSplit(source, outcome)
		outcome.SameRoleAssignments = sameRoleAssignments(source, outcome)
		report.Outcomes = append(report.Outcomes, outcome)
	}
	return report, nil
}

func (s *AnalysisService) replayPolicies(
	requested []string,
	candidates []domain.MatchPolicy,
) (map[string]domain.MatchPolicy, []string, error) {
	basePolicies := s.policies
	if s.catalog != nil {
		basePolicies = make(map[string]domain.MatchPolicy)
		for _, policy := range s.catalog.Policies() {
			basePolicies[policy.Version] = policy
		}
	}
	policies := make(map[string]domain.MatchPolicy, len(basePolicies)+len(candidates))
	for version, policy := range basePolicies {
		policies[version] = policy
	}
	for _, policy := range candidates {
		if err := policy.Validate(); err != nil {
			return nil, nil, err
		}
		if _, exists := policies[policy.Version]; exists {
			return nil, nil, domain.ErrInvalidPolicy
		}
		policies[policy.Version] = policy
	}
	if len(requested) == 0 {
		requested = make([]string, 0, len(policies))
		for version := range policies {
			requested = append(requested, version)
		}
	}
	if len(requested) > MaxReplayPolicies {
		return nil, nil, ErrInvalidAnalysis
	}
	seen := make(map[string]struct{}, len(requested))
	versions := make([]string, 0, len(requested))
	for _, version := range requested {
		version = strings.TrimSpace(version)
		if _, exists := policies[version]; !exists {
			return nil, nil, ErrPolicyNotFound
		}
		if _, duplicate := seen[version]; duplicate {
			continue
		}
		seen[version] = struct{}{}
		versions = append(versions, version)
	}
	if len(versions) == 0 {
		return nil, nil, ErrInvalidAnalysis
	}
	sort.Strings(versions)
	return policies, versions, nil
}

func maximumCandidateLimit(versions []string, policies map[string]domain.MatchPolicy) int {
	maximum := 0
	for _, version := range versions {
		if policies[version].CandidateLimit > maximum {
			maximum = policies[version].CandidateLimit
		}
	}
	return maximum
}

func replayTicketIDs(source *domain.Match, requested []string) []string {
	if len(requested) == 0 {
		for _, player := range append(source.TeamA().Players, source.TeamB().Players...) {
			requested = append(requested, player.TicketID)
		}
	}
	seen := make(map[string]struct{}, len(requested))
	result := make([]string, 0, len(requested))
	for _, ticketID := range requested {
		ticketID = strings.TrimSpace(ticketID)
		if ticketID == "" {
			continue
		}
		if _, duplicate := seen[ticketID]; duplicate {
			continue
		}
		seen[ticketID] = struct{}{}
		result = append(result, ticketID)
	}
	return result
}

func rebuildQueuedTicket(historical *domain.MatchTicket) (*domain.MatchTicket, error) {
	if historical == nil {
		return nil, ErrInvalidAnalysis
	}
	ticket, err := domain.NewTicket(domain.NewTicketParams{
		ID: historical.ID(), PlayerID: historical.PlayerID(), PartyID: historical.PartyID(),
		Mode: historical.Mode(), ClientVersion: historical.ClientVersion(), Region: historical.Region(),
		Rating: historical.Rating(), PreferredRoles: historical.PreferredRoles(),
		RegionLatency: historical.RegionLatency(), CreatedAt: historical.CreatedAt(),
	})
	if err != nil {
		return nil, err
	}
	if err := ticket.Queue(historical.CreatedAt()); err != nil {
		return nil, err
	}
	return ticket, nil
}

func replayTeam(team engine.Team) ReplayTeam {
	result := ReplayTeam{AverageRating: team.AverageRating, RoleScore: team.RoleScore}
	for _, player := range team.Players {
		selectedHero, proficiency, _ := hero.BestForRole(player.Ticket.HeroProficiency(), string(player.Role))
		result.Players = append(result.Players, ReplayPlayer{
			PlayerID: player.Ticket.PlayerID(), TicketID: player.Ticket.ID(), PartyID: player.Ticket.PartyID(),
			Role: player.Role, Rating: player.Ticket.Rating(), HeroID: selectedHero.ID,
			HeroProficiency: proficiency, BehaviorScore: player.Ticket.BehaviorScore(),
		})
	}
	return result
}

func replayQuality(quality engine.MatchQuality) domain.MatchQuality {
	return domain.MatchQuality{
		TotalScore: quality.TotalScore, SkillScore: quality.SkillScore, RoleScore: quality.RoleScore,
		LatencyScore: quality.LatencyScore, PartyScore: quality.PartyScore, WaitScore: quality.WaitScore,
		PredictedWinRateA: quality.PredictedWinRateA, PredictedWinRateB: quality.PredictedWinRateB,
		Reasons: append([]string(nil), quality.Reasons...),
	}
}

func sameTeamSplit(source *domain.Match, outcome ReplayOutcome) bool {
	sourceA := sortedMatchPlayerIDs(source.TeamA().Players)
	sourceB := sortedMatchPlayerIDs(source.TeamB().Players)
	replayA := sortedReplayPlayerIDs(outcome.TeamA.Players)
	replayB := sortedReplayPlayerIDs(outcome.TeamB.Players)
	return (equalStrings(sourceA, replayA) && equalStrings(sourceB, replayB)) ||
		(equalStrings(sourceA, replayB) && equalStrings(sourceB, replayA))
}

func sameRoleAssignments(source *domain.Match, outcome ReplayOutcome) bool {
	sourceRoles := make(map[string]domain.Role, 10)
	for _, player := range append(source.TeamA().Players, source.TeamB().Players...) {
		sourceRoles[player.PlayerID] = player.Role
	}
	for _, player := range append(outcome.TeamA.Players, outcome.TeamB.Players...) {
		if sourceRoles[player.PlayerID] != player.Role {
			return false
		}
		delete(sourceRoles, player.PlayerID)
	}
	return len(sourceRoles) == 0
}

func sortedMatchPlayerIDs(players []domain.MatchPlayer) []string {
	result := make([]string, 0, len(players))
	for _, player := range players {
		result = append(result, player.PlayerID)
	}
	sort.Strings(result)
	return result
}

func sortedReplayPlayerIDs(players []ReplayPlayer) []string {
	result := make([]string, 0, len(players))
	for _, player := range players {
		result = append(result, player.PlayerID)
	}
	sort.Strings(result)
	return result
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func accumulateQualitySummary(summary *PolicyQualitySummary, observation MatchQualityObservation) {
	summary.MatchCount++
	summary.AveragePredictedQuality += observation.PredictedQuality
	summary.AverageActualQuality += observation.ActualQuality
	summary.MeanSignedQualityError += observation.SignedQualityError
	summary.MeanAbsoluteQualityError += observation.AbsoluteQualityError
	summary.WinProbabilityBrierScore += observation.WinProbabilityBrier
	summary.TeamAWinRate += observation.TeamAOutcome
	summary.AverageDurationSeconds += float64(observation.DurationSeconds)
	if observation.OneSided {
		summary.OneSidedRate++
	}
	if observation.HasAFK {
		summary.AFKRate++
	}
	if observation.Surrendered {
		summary.SurrenderRate++
	}
}

func finalizeQualitySummary(summary *PolicyQualitySummary) {
	if summary.MatchCount == 0 {
		return
	}
	count := float64(summary.MatchCount)
	summary.AveragePredictedQuality /= count
	summary.AverageActualQuality /= count
	summary.MeanSignedQualityError /= count
	summary.MeanAbsoluteQualityError /= count
	summary.WinProbabilityBrierScore /= count
	summary.TeamAWinRate /= count
	summary.AverageDurationSeconds /= count
	summary.OneSidedRate /= count
	summary.AFKRate /= count
	summary.SurrenderRate /= count
}

func square(value float64) float64 { return value * value }
