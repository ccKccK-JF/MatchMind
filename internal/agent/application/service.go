package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	agentdomain "github.com/ccKccK-JF/MatchMind/internal/agent/domain"
	matchapp "github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	matchdomain "github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	platformid "github.com/ccKccK-JF/MatchMind/internal/platform/id"
)

const (
	DefaultHistoricalSample = 20
	MaxHistoricalSample     = 50
	MinimumRiskSample       = 10
)

var (
	ErrRunNotFound        = errors.New("agent run not found")
	ErrProposalNotFound   = errors.New("policy proposal not found")
	ErrRepositoryConflict = errors.New("agent repository revision conflict")
	ErrInvalidCommand     = errors.New("invalid agent command")
	ErrToolUnavailable    = errors.New("agent tool unavailable")
)

type Repository interface {
	CreateRun(ctx context.Context, run agentdomain.AuditRun) error
	FailRun(ctx context.Context, run agentdomain.AuditRun) error
	CompleteRun(ctx context.Context, run agentdomain.AuditRun, proposal agentdomain.PolicyProposal) error
	GetRun(ctx context.Context, runID string) (agentdomain.AuditRun, error)
	ListRuns(ctx context.Context, limit int) ([]agentdomain.AuditRun, error)
	GetProposal(ctx context.Context, proposalID string) (agentdomain.PolicyProposal, error)
	ListProposals(ctx context.Context, limit int) ([]agentdomain.PolicyProposal, error)
	UpdateProposal(ctx context.Context, proposal agentdomain.PolicyProposal, expectedState agentdomain.ProposalState) error
	RecoverIncompleteRuns(ctx context.Context, now time.Time) (int, error)
}

type ToolClient interface {
	OperationalSnapshot(ctx context.Context) (matchapp.OperationalSnapshot, error)
	AnalyzeMatchQuality(ctx context.Context, filter matchapp.MatchHistoryFilter) (matchapp.QualityAnalysis, error)
	ReplayHistoricalMatch(ctx context.Context, request matchapp.ReplayRequest) (matchapp.ReplayReport, error)
	ActivateApprovedPolicy(ctx context.Context, approvalID string, policy matchdomain.MatchPolicy, treatmentBasisPoints int, assignmentSalt string) (matchapp.PolicyExperiment, error)
	RollbackPolicyExperiment(ctx context.Context, experimentID string) error
}

type Metrics interface {
	IncRunSucceeded()
	IncRunFailed()
	ObserveRunDuration(float64)
	IncProposalApproved()
	IncProposalRejected()
	IncProposalActivated()
	IncProposalRolledBack()
}

type noopMetrics struct{}

func (noopMetrics) IncRunSucceeded()           {}
func (noopMetrics) IncRunFailed()              {}
func (noopMetrics) ObserveRunDuration(float64) {}
func (noopMetrics) IncProposalApproved()       {}
func (noopMetrics) IncProposalRejected()       {}
func (noopMetrics) IncProposalActivated()      {}
func (noopMetrics) IncProposalRolledBack()     {}

type RunCommand struct {
	RequestedBy       string    `json:"requested_by"`
	BasePolicyVersion string    `json:"base_policy_version"`
	Mode              string    `json:"mode"`
	ServerRegion      string    `json:"server_region"`
	From              time.Time `json:"from"`
	To                time.Time `json:"to"`
	HistoricalLimit   int       `json:"historical_limit"`
}

type ReviewCommand struct {
	ProposalID string
	ReviewerID string
	Reason     string
	Approve    bool
}

type ActivateCommand struct {
	ProposalID           string
	OperatorID           string
	TreatmentBasisPoints int
	AssignmentSalt       string
}

type RollbackCommand struct {
	ProposalID string
	OperatorID string
}

type Service struct {
	repository        Repository
	tools             ToolClient
	agentName         string
	model             string
	promptVersion     string
	defaultBasePolicy string
	idGenerator       platformid.Generator
	clock             func() time.Time
	metrics           Metrics
}

func NewService(
	repository Repository,
	tools ToolClient,
	agentName, model, promptVersion, defaultBasePolicy string,
	idGenerator platformid.Generator,
	clock func() time.Time,
) (*Service, error) {
	if repository == nil || tools == nil || strings.TrimSpace(agentName) == "" ||
		strings.TrimSpace(model) == "" || strings.TrimSpace(promptVersion) == "" ||
		strings.TrimSpace(defaultBasePolicy) == "" {
		return nil, ErrInvalidCommand
	}
	if idGenerator == nil {
		idGenerator = platformid.UUID
	}
	if clock == nil {
		clock = time.Now
	}
	return &Service{
		repository: repository, tools: tools, agentName: strings.TrimSpace(agentName),
		model: strings.TrimSpace(model), promptVersion: strings.TrimSpace(promptVersion),
		defaultBasePolicy: strings.TrimSpace(defaultBasePolicy), idGenerator: idGenerator, clock: clock,
		metrics: noopMetrics{},
	}, nil
}

func (s *Service) SetMetrics(metrics Metrics) {
	if metrics != nil {
		s.metrics = metrics
	}
}

func (s *Service) Recover(ctx context.Context) (int, error) {
	return s.repository.RecoverIncompleteRuns(ctx, s.clock())
}

func (s *Service) RunAnalysis(ctx context.Context, command RunCommand) (agentdomain.AuditRun, agentdomain.PolicyProposal, error) {
	command.RequestedBy = strings.TrimSpace(command.RequestedBy)
	command.BasePolicyVersion = strings.TrimSpace(command.BasePolicyVersion)
	command.Mode = strings.TrimSpace(command.Mode)
	command.ServerRegion = strings.ToLower(strings.TrimSpace(command.ServerRegion))
	if command.BasePolicyVersion == "" {
		command.BasePolicyVersion = s.defaultBasePolicy
	}
	if command.HistoricalLimit == 0 {
		command.HistoricalLimit = DefaultHistoricalSample
	}
	if command.RequestedBy == "" || command.HistoricalLimit < 1 || command.HistoricalLimit > MaxHistoricalSample ||
		(!command.From.IsZero() && !command.To.IsZero() && command.To.Before(command.From)) {
		return agentdomain.AuditRun{}, agentdomain.PolicyProposal{}, ErrInvalidCommand
	}
	inputJSON, _ := json.Marshal(command)
	runID, err := s.idGenerator()
	if err != nil {
		return agentdomain.AuditRun{}, agentdomain.PolicyProposal{}, fmt.Errorf("generate agent run id: %w", err)
	}
	proposalID, err := s.idGenerator()
	if err != nil {
		return agentdomain.AuditRun{}, agentdomain.PolicyProposal{}, fmt.Errorf("generate proposal id: %w", err)
	}
	run, err := agentdomain.NewAuditRun(
		runID, s.agentName, s.model, s.promptVersion, command.RequestedBy, string(inputJSON), s.clock(),
	)
	if err != nil {
		return agentdomain.AuditRun{}, agentdomain.PolicyProposal{}, err
	}
	if err := s.repository.CreateRun(ctx, run.Clone()); err != nil {
		return agentdomain.AuditRun{}, agentdomain.PolicyProposal{}, err
	}
	toolCalls := make([]agentdomain.ToolCall, 0, command.HistoricalLimit+2)

	snapshotInput := map[string]any{"include": []string{"queue", "policies", "active_experiment"}}
	snapshot, call, err := callTool(ctx, s.clock, "operational_snapshot", snapshotInput, s.tools.OperationalSnapshot)
	toolCalls = append(toolCalls, call)
	if err != nil {
		return s.failRun(ctx, run, toolCalls, err)
	}
	basePolicy, exists := findPolicy(snapshot.Policies, command.BasePolicyVersion)
	if !exists {
		return s.failRun(ctx, run, toolCalls, matchapp.ErrPolicyNotFound)
	}

	filter := matchapp.MatchHistoryFilter{
		Mode: command.Mode, ServerRegion: command.ServerRegion, From: command.From,
		To: command.To, Limit: command.HistoricalLimit,
	}
	analysis, call, err := callTool(ctx, s.clock, "match_quality_analysis", filter, func(ctx context.Context) (matchapp.QualityAnalysis, error) {
		return s.tools.AnalyzeMatchQuality(ctx, filter)
	})
	toolCalls = append(toolCalls, call)
	if err != nil {
		return s.failRun(ctx, run, toolCalls, err)
	}

	candidate, rationale := generateCandidatePolicy(basePolicy, proposalID, analysis.Observations)
	replays := make([]matchapp.ReplayReport, 0, len(analysis.Observations))
	for _, observation := range analysis.Observations {
		replayRequest := matchapp.ReplayRequest{
			MatchID: observation.MatchID, PolicyVersions: []string{candidate.Version},
			CandidatePolicies: []matchdomain.MatchPolicy{candidate},
		}
		replay, replayCall, replayErr := callTool(ctx, s.clock, "historical_replay", replayRequest, func(ctx context.Context) (matchapp.ReplayReport, error) {
			return s.tools.ReplayHistoricalMatch(ctx, replayRequest)
		})
		toolCalls = append(toolCalls, replayCall)
		if replayErr != nil {
			return s.failRun(ctx, run, toolCalls, replayErr)
		}
		replays = append(replays, replay)
	}
	risk := assessRisk(candidate, analysis.Observations, replays)
	proposal, err := agentdomain.NewPolicyProposal(
		proposalID, run.ID, command.RequestedBy, basePolicy.Version,
		candidate, rationale, risk, s.clock(),
	)
	if err != nil {
		return s.failRun(ctx, run, toolCalls, err)
	}
	output := map[string]any{
		"proposal_id": proposal.ID, "candidate_policy": candidate,
		"rationale": rationale, "risk_report": risk,
		"queue_size_observed": snapshot.QueueSize,
	}
	outputJSON, _ := json.Marshal(output)
	if err := run.Succeed(string(outputJSON), candidate.Version, toolCalls, s.clock()); err != nil {
		return agentdomain.AuditRun{}, agentdomain.PolicyProposal{}, err
	}
	if err := s.repository.CompleteRun(ctx, run.Clone(), proposal.Clone()); err != nil {
		return agentdomain.AuditRun{}, agentdomain.PolicyProposal{}, err
	}
	s.metrics.IncRunSucceeded()
	s.metrics.ObserveRunDuration(run.FinishedAt.Sub(run.StartedAt).Seconds())
	return run.Clone(), proposal.Clone(), nil
}

func (s *Service) ReviewProposal(ctx context.Context, command ReviewCommand) (agentdomain.PolicyProposal, error) {
	proposal, err := s.repository.GetProposal(ctx, strings.TrimSpace(command.ProposalID))
	if err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	expected := proposal.State
	if err := proposal.Review(command.ReviewerID, command.Reason, command.Approve, s.clock()); err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	if err := s.repository.UpdateProposal(ctx, proposal, expected); err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	if command.Approve {
		s.metrics.IncProposalApproved()
	} else {
		s.metrics.IncProposalRejected()
	}
	return proposal.Clone(), nil
}

func (s *Service) ActivateProposal(ctx context.Context, command ActivateCommand) (agentdomain.PolicyProposal, error) {
	command.ProposalID = strings.TrimSpace(command.ProposalID)
	command.OperatorID = strings.TrimSpace(command.OperatorID)
	command.AssignmentSalt = strings.TrimSpace(command.AssignmentSalt)
	if command.ProposalID == "" || command.OperatorID == "" || command.AssignmentSalt == "" ||
		command.TreatmentBasisPoints < 1 || command.TreatmentBasisPoints > 10000 {
		return agentdomain.PolicyProposal{}, ErrInvalidCommand
	}
	proposal, err := s.repository.GetProposal(ctx, command.ProposalID)
	if err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	if proposal.State == agentdomain.ProposalActive {
		return proposal, nil
	}
	if proposal.State == agentdomain.ProposalApproved {
		expected := proposal.State
		if err := proposal.BeginActivation(command.OperatorID, command.TreatmentBasisPoints, command.AssignmentSalt, s.clock()); err != nil {
			return agentdomain.PolicyProposal{}, err
		}
		if err := s.repository.UpdateProposal(ctx, proposal, expected); err != nil {
			return agentdomain.PolicyProposal{}, err
		}
	}
	if proposal.State != agentdomain.ProposalActivating {
		return agentdomain.PolicyProposal{}, agentdomain.ErrIllegalProposalState
	}
	experiment, err := s.tools.ActivateApprovedPolicy(
		ctx, proposal.ID, proposal.CandidatePolicy, proposal.TreatmentBasisPoints, proposal.AssignmentSalt,
	)
	if err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	expected := proposal.State
	if err := proposal.CompleteActivation(experiment.ID, s.clock()); err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	if err := s.repository.UpdateProposal(ctx, proposal, expected); err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	s.metrics.IncProposalActivated()
	return proposal.Clone(), nil
}

func (s *Service) RollbackProposal(ctx context.Context, command RollbackCommand) (agentdomain.PolicyProposal, error) {
	command.ProposalID = strings.TrimSpace(command.ProposalID)
	command.OperatorID = strings.TrimSpace(command.OperatorID)
	if command.ProposalID == "" || command.OperatorID == "" {
		return agentdomain.PolicyProposal{}, ErrInvalidCommand
	}
	proposal, err := s.repository.GetProposal(ctx, command.ProposalID)
	if err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	if proposal.State == agentdomain.ProposalRolledBack {
		return proposal, nil
	}
	if proposal.State == agentdomain.ProposalActive {
		expected := proposal.State
		if err := proposal.BeginRollback(command.OperatorID, s.clock()); err != nil {
			return agentdomain.PolicyProposal{}, err
		}
		if err := s.repository.UpdateProposal(ctx, proposal, expected); err != nil {
			return agentdomain.PolicyProposal{}, err
		}
	}
	if proposal.State != agentdomain.ProposalRollingBack {
		return agentdomain.PolicyProposal{}, agentdomain.ErrIllegalProposalState
	}
	if err := s.tools.RollbackPolicyExperiment(ctx, proposal.ExperimentID); err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	expected := proposal.State
	if err := proposal.CompleteRollback(s.clock()); err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	if err := s.repository.UpdateProposal(ctx, proposal, expected); err != nil {
		return agentdomain.PolicyProposal{}, err
	}
	s.metrics.IncProposalRolledBack()
	return proposal.Clone(), nil
}

func (s *Service) GetRun(ctx context.Context, runID string) (agentdomain.AuditRun, error) {
	return s.repository.GetRun(ctx, strings.TrimSpace(runID))
}

func (s *Service) ListRuns(ctx context.Context, limit int) ([]agentdomain.AuditRun, error) {
	return s.repository.ListRuns(ctx, normalizedListLimit(limit))
}

func (s *Service) GetProposal(ctx context.Context, proposalID string) (agentdomain.PolicyProposal, error) {
	return s.repository.GetProposal(ctx, strings.TrimSpace(proposalID))
}

func (s *Service) ListProposals(ctx context.Context, limit int) ([]agentdomain.PolicyProposal, error) {
	return s.repository.ListProposals(ctx, normalizedListLimit(limit))
}

func (s *Service) failRun(
	ctx context.Context,
	run *agentdomain.AuditRun,
	calls []agentdomain.ToolCall,
	cause error,
) (agentdomain.AuditRun, agentdomain.PolicyProposal, error) {
	if failErr := run.Fail(cause.Error(), calls, s.clock()); failErr == nil {
		if repositoryErr := s.repository.FailRun(ctx, run.Clone()); repositoryErr != nil {
			return agentdomain.AuditRun{}, agentdomain.PolicyProposal{}, fmt.Errorf("%w; persist failed run: %v", cause, repositoryErr)
		}
		s.metrics.IncRunFailed()
		s.metrics.ObserveRunDuration(run.FinishedAt.Sub(run.StartedAt).Seconds())
	}
	return run.Clone(), agentdomain.PolicyProposal{}, cause
}

func callTool[T any](
	ctx context.Context,
	clock func() time.Time,
	name string,
	input any,
	call func(context.Context) (T, error),
) (T, agentdomain.ToolCall, error) {
	inputJSON, _ := json.Marshal(input)
	startedAt := clock()
	result, err := call(ctx)
	finishedAt := clock()
	output := any(result)
	status := agentdomain.ToolCallSucceeded
	if err != nil {
		output = map[string]string{"error": err.Error()}
		status = agentdomain.ToolCallFailed
	}
	outputJSON, marshalErr := json.Marshal(output)
	if marshalErr != nil {
		outputJSON = []byte(`{"error":"tool output encoding failed"}`)
		if err == nil {
			err = marshalErr
		}
		status = agentdomain.ToolCallFailed
	}
	return result, agentdomain.ToolCall{
		Name: name, InputJSON: string(inputJSON), OutputJSON: string(outputJSON), Status: status,
		StartedAt: startedAt, FinishedAt: finishedAt,
	}, err
}

func findPolicy(policies []matchdomain.MatchPolicy, version string) (matchdomain.MatchPolicy, bool) {
	for _, policy := range policies {
		if policy.Version == version {
			return policy, true
		}
	}
	return matchdomain.MatchPolicy{}, false
}

func generateCandidatePolicy(
	base matchdomain.MatchPolicy,
	proposalID string,
	observations []matchapp.MatchQualityObservation,
) (matchdomain.MatchPolicy, []string) {
	candidate := base
	candidate.Version = "proposal-" + strings.TrimSpace(proposalID)
	candidate.TeamAlgorithm = matchdomain.TeamAlgorithmBeam
	rationale := []string{"use deterministic Beam Search for offline-reviewed team formation"}
	if len(observations) == 0 {
		return candidate, append(rationale, "retain base parameters because no finished Match samples were available")
	}
	average := averageObservations(observations)
	if average.oneSidedRate > .15 || average.brierScore > .25 {
		shiftPolicyWeight(&candidate, &candidate.SkillWeight, .04)
		candidate.MinQualityScore = math.Min(100, candidate.MinQualityScore+2)
		rationale = append(rationale, "increase fairness weight and minimum quality after one-sided or calibration degradation")
	}
	if average.roleScore < 85 {
		shiftPolicyWeight(&candidate, &candidate.RoleWeight, .03)
		rationale = append(rationale, "increase role preference weight after low historical role satisfaction")
	}
	if average.latencyScore < 75 {
		shiftPolicyWeight(&candidate, &candidate.LatencyWeight, .03)
		if candidate.MaxLatencyMS > 220 {
			candidate.MaxLatencyMS = 220
		}
		rationale = append(rationale, "increase latency weight and cap admitted latency at 220 ms")
	}
	return candidate, rationale
}

type observationAverages struct {
	skillScore, roleScore, latencyScore float64
	oneSidedRate, brierScore            float64
}

func averageObservations(observations []matchapp.MatchQualityObservation) observationAverages {
	var result observationAverages
	for _, observation := range observations {
		result.skillScore += observation.SkillScore
		result.roleScore += observation.RoleScore
		result.latencyScore += observation.LatencyScore
		result.brierScore += observation.WinProbabilityBrier
		if observation.OneSided {
			result.oneSidedRate++
		}
	}
	if len(observations) > 0 {
		count := float64(len(observations))
		result.skillScore /= count
		result.roleScore /= count
		result.latencyScore /= count
		result.brierScore /= count
		result.oneSidedRate /= count
	}
	return result
}

func shiftPolicyWeight(policy *matchdomain.MatchPolicy, target *float64, amount float64) {
	remaining := amount
	for _, donor := range []*float64{&policy.WaitWeight, &policy.PartyWeight} {
		take := math.Min(*donor, remaining)
		*donor -= take
		*target += take
		remaining -= take
		if remaining <= 1e-12 {
			return
		}
	}
}

func assessRisk(
	candidate matchdomain.MatchPolicy,
	observations []matchapp.MatchQualityObservation,
	replays []matchapp.ReplayReport,
) agentdomain.RiskReport {
	report := agentdomain.RiskReport{SampleCount: len(observations)}
	candidateOutcomes := make([]matchapp.ReplayOutcome, 0, len(replays))
	for _, replay := range replays {
		if len(replay.Outcomes) == 1 && replay.Outcomes[0].Matched {
			candidateOutcomes = append(candidateOutcomes, replay.Outcomes[0])
		}
	}
	baseline := averageObservations(observations)
	candidateSkill, candidateRole, candidateLatency := averageReplayScores(candidateOutcomes)
	replayComplete := len(candidateOutcomes) == len(observations)
	report.Findings = append(report.Findings,
		riskFinding("fairness", baseline.skillScore-candidateSkill, 2, replayComplete, "candidate skill fairness deterioration"),
	)
	latencyPassed := replayComplete && candidate.MaxLatencyMS <= 250 && baseline.latencyScore-candidateLatency <= 5
	report.Findings = append(report.Findings, agentdomain.RiskFinding{
		Category: "latency", Status: riskStatus(latencyPassed), Observed: baseline.latencyScore - candidateLatency,
		Threshold: 5, Message: "candidate latency score deterioration and maximum latency were checked",
	})
	report.Findings = append(report.Findings,
		riskFinding("role_fill", baseline.roleScore-candidateRole, 5, replayComplete, "candidate role-fill deterioration"),
	)
	samplePassed := len(observations) >= MinimumRiskSample
	report.Findings = append(report.Findings, agentdomain.RiskFinding{
		Category: "sample_size", Status: riskStatus(samplePassed), Observed: float64(len(observations)),
		Threshold: MinimumRiskSample, Message: "minimum historical sample size was checked",
	})
	highRatingDelta, highRatingCount, highRatingReplayComplete := highRatingQualityDelta(observations, replays)
	highRatingStatus := agentdomain.RiskStatusWarning
	highRatingMessage := "no high-rating sample was available; monitor during a guarded experiment"
	if highRatingCount > 0 {
		highRatingStatus = riskStatus(highRatingReplayComplete && highRatingDelta >= -3)
		highRatingMessage = "high-rating predicted quality deterioration was checked"
	}
	report.Findings = append(report.Findings, agentdomain.RiskFinding{
		Category: "high_rating", Status: highRatingStatus, Observed: highRatingDelta,
		Threshold: -3, Message: highRatingMessage,
	})
	report.Passed = true
	for _, finding := range report.Findings {
		if finding.Status == agentdomain.RiskStatusBlock {
			report.Passed = false
		}
	}
	return report
}

func riskFinding(category string, observed, threshold float64, complete bool, message string) agentdomain.RiskFinding {
	return agentdomain.RiskFinding{
		Category: category, Status: riskStatus(complete && observed <= threshold),
		Observed: observed, Threshold: threshold, Message: message,
	}
}

func riskStatus(passed bool) agentdomain.RiskStatus {
	if passed {
		return agentdomain.RiskStatusPass
	}
	return agentdomain.RiskStatusBlock
}

func averageReplayScores(outcomes []matchapp.ReplayOutcome) (skill, role, latency float64) {
	for _, outcome := range outcomes {
		skill += outcome.Quality.SkillScore
		role += outcome.Quality.RoleScore
		latency += outcome.Quality.LatencyScore
	}
	if len(outcomes) > 0 {
		count := float64(len(outcomes))
		skill /= count
		role /= count
		latency /= count
	}
	return
}

func highRatingQualityDelta(observations []matchapp.MatchQualityObservation, replays []matchapp.ReplayReport) (float64, int, bool) {
	outcomes := make(map[string]matchapp.ReplayOutcome, len(replays))
	for _, replay := range replays {
		if len(replay.Outcomes) == 1 && replay.Outcomes[0].Matched {
			outcomes[replay.SourceMatchID] = replay.Outcomes[0]
		}
	}
	var total float64
	eligible := 0
	matched := 0
	for _, observation := range observations {
		if observation.AverageRating < 1800 {
			continue
		}
		eligible++
		outcome, exists := outcomes[observation.MatchID]
		if !exists {
			continue
		}
		total += outcome.Quality.TotalScore - observation.PredictedQuality
		matched++
	}
	if eligible == 0 {
		return 0, 0, true
	}
	if matched == 0 {
		return 0, eligible, false
	}
	return total / float64(matched), eligible, matched == eligible
}

func normalizedListLimit(limit int) int {
	if limit <= 0 {
		return 100
	}
	if limit > 1000 {
		return 1000
	}
	return limit
}
