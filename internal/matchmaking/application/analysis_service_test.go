package application

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

type analysisHistoryStub struct {
	matches map[string]*domain.Match
	filter  MatchHistoryFilter
}

func (s *analysisHistoryStub) Get(_ context.Context, matchID string) (*domain.Match, error) {
	match := s.matches[matchID]
	if match == nil {
		return nil, ErrMatchNotFound
	}
	return match.Clone(), nil
}

func (s *analysisHistoryStub) ListFinished(_ context.Context, filter MatchHistoryFilter) ([]*domain.Match, error) {
	s.filter = filter
	result := make([]*domain.Match, 0, len(s.matches))
	for _, match := range s.matches {
		if match.State() == domain.MatchStateFinished &&
			(filter.PolicyVersion == "" || match.PolicyVersion() == filter.PolicyVersion) {
			result = append(result, match.Clone())
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID() < result[j].ID() })
	return result, nil
}

type analysisTicketStub map[string]*domain.MatchTicket

func (s analysisTicketStub) Get(_ context.Context, ticketID string) (*domain.MatchTicket, error) {
	ticket := s[ticketID]
	if ticket == nil {
		return nil, ErrTicketNotFound
	}
	return ticket.Clone(), nil
}

func TestAnalyzeMatchQualityGroupsPoliciesAndComputesErrors(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	history := &analysisHistoryStub{matches: map[string]*domain.Match{
		"match-1": finishedAnalysisMatch(t, "match-1", "v1-greedy", now, 80, .6, 70, domain.WinningTeamA, false),
		"match-2": finishedAnalysisMatch(t, "match-2", "v1-greedy", now.Add(time.Minute), 90, .4, 95, domain.WinningTeamB, true),
		"match-3": finishedAnalysisMatch(t, "match-3", "v2-beam", now.Add(2*time.Minute), 88, .5, 90, domain.WinningTeamA, false),
	}}
	service, err := NewAnalysisService(history, analysisTicketStub{}, []domain.MatchPolicy{domain.DefaultPolicy(), domain.BeamPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := service.AnalyzeMatchQuality(context.Background(), MatchHistoryFilter{
		PolicyVersion: " v1-greedy ", ServerRegion: " HONGKONG ", Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if history.filter.PolicyVersion != "v1-greedy" || history.filter.ServerRegion != "hongkong" {
		t.Fatalf("normalized filter = %#v", history.filter)
	}
	if len(analysis.Observations) != 2 || len(analysis.Summaries) != 1 {
		t.Fatalf("analysis sizes = observations %d, summaries %d", len(analysis.Observations), len(analysis.Summaries))
	}
	summary := analysis.Summaries[0]
	if summary.MatchCount != 2 || summary.AveragePredictedQuality != 85 ||
		summary.AverageActualQuality != 82.5 || summary.MeanSignedQualityError != -2.5 ||
		summary.MeanAbsoluteQualityError != 7.5 || summary.TeamAWinRate != .5 || summary.OneSidedRate != .5 {
		t.Fatalf("summary = %#v", summary)
	}
	if summary.WinProbabilityBrierScore < .159999 || summary.WinProbabilityBrierScore > .160001 {
		t.Fatalf("Brier score = %v", summary.WinProbabilityBrierScore)
	}
}

func TestReplayHistoricalMatchIsDeterministicAndReadOnly(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	tickets := make(analysisTicketStub, 15)
	for index := 0; index < 15; index++ {
		role := analysisRoles[index%len(analysisRoles)]
		ticket, err := domain.NewTicket(domain.NewTicketParams{
			ID: "ticket-" + twoDigits(index), PlayerID: "player-" + twoDigits(index),
			Mode: "ranked_5v5", ClientVersion: "1.0.0", Region: "hongkong",
			Rating: 1480 + float64(index*3), PreferredRoles: []domain.Role{role},
			RegionLatency: map[string]int{"hongkong": 20 + index}, CreatedAt: now.Add(time.Duration(index-20) * time.Second),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := ticket.Queue(ticket.CreatedAt()); err != nil {
			t.Fatal(err)
		}
		tickets[ticket.ID()] = ticket
	}
	source := finishedAnalysisMatch(t, "match-replay", "v1-greedy", now, 82, .52, 76, domain.WinningTeamA, false)
	history := &analysisHistoryStub{matches: map[string]*domain.Match{source.ID(): source}}
	service, err := NewAnalysisService(history, tickets, []domain.MatchPolicy{domain.DefaultPolicy(), domain.BeamPolicy()})
	if err != nil {
		t.Fatal(err)
	}
	request := ReplayRequest{MatchID: source.ID(), TicketIDs: ticketMapIDs(tickets)}
	first, err := service.ReplayHistoricalMatch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ReplayHistoricalMatch(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("replay is not deterministic\nfirst: %#v\nsecond: %#v", first, second)
	}
	var waitGroup sync.WaitGroup
	reports := make(chan ReplayReport, 16)
	errorsFound := make(chan error, 16)
	for range 16 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			report, replayErr := service.ReplayHistoricalMatch(context.Background(), request)
			if replayErr != nil {
				errorsFound <- replayErr
				return
			}
			reports <- report
		}()
	}
	waitGroup.Wait()
	close(reports)
	close(errorsFound)
	for replayErr := range errorsFound {
		t.Fatalf("concurrent replay error = %v", replayErr)
	}
	for report := range reports {
		if !reflect.DeepEqual(first, report) {
			t.Fatalf("concurrent replay changed report: %#v", report)
		}
	}
	if first.TicketCount != 15 || len(first.Outcomes) != 2 {
		t.Fatalf("replay report = %#v", first)
	}
	for _, outcome := range first.Outcomes {
		if !outcome.Matched || len(outcome.TeamA.Players) != 5 || len(outcome.TeamB.Players) != 5 ||
			outcome.FormationsEvaluated == 0 || outcome.Quality.TotalScore <= 0 {
			t.Fatalf("outcome = %#v", outcome)
		}
	}
	for _, ticket := range tickets {
		if ticket.State() != domain.TicketStateQueued {
			t.Fatalf("historical ticket %s was mutated to %s", ticket.ID(), ticket.State())
		}
	}
	candidate := domain.BeamPolicy()
	candidate.Version = "candidate-offline-only"
	candidate.MinQualityScore = 65
	candidateReport, err := service.ReplayHistoricalMatch(context.Background(), ReplayRequest{
		MatchID: source.ID(), TicketIDs: ticketMapIDs(tickets),
		PolicyVersions: []string{candidate.Version}, CandidatePolicies: []domain.MatchPolicy{candidate},
	})
	if err != nil || len(candidateReport.Outcomes) != 1 || !candidateReport.Outcomes[0].Matched {
		t.Fatalf("candidate policy replay = %#v, %v", candidateReport, err)
	}
	if _, exists := service.policies[candidate.Version]; exists {
		t.Fatal("offline candidate was registered in the live policy catalog")
	}
}

func TestAnalysisRejectsInvalidLimitsAndUnknownReplayPolicy(t *testing.T) {
	now := time.Date(2026, 8, 8, 12, 0, 0, 0, time.UTC)
	source := finishedAnalysisMatch(t, "match-1", "v1-greedy", now, 80, .5, 80, domain.WinningTeamA, false)
	service, err := NewAnalysisService(
		&analysisHistoryStub{matches: map[string]*domain.Match{source.ID(): source}},
		analysisTicketStub{}, []domain.MatchPolicy{domain.DefaultPolicy()},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.AnalyzeMatchQuality(context.Background(), MatchHistoryFilter{Limit: MaxAnalysisLimit + 1}); !errors.Is(err, ErrInvalidAnalysis) {
		t.Fatalf("invalid limit error = %v", err)
	}
	if _, err := service.ReplayHistoricalMatch(context.Background(), ReplayRequest{
		MatchID: source.ID(), PolicyVersions: []string{"missing"},
	}); !errors.Is(err, ErrPolicyNotFound) {
		t.Fatalf("unknown policy error = %v", err)
	}
}

var analysisRoles = []domain.Role{
	domain.RoleVanguard, domain.RoleRoamer, domain.RoleCore, domain.RoleRanged, domain.RoleSupport,
}

func finishedAnalysisMatch(
	t *testing.T,
	id, policyVersion string,
	createdAt time.Time,
	predictedQuality, predictedWinRateA, actualQuality float64,
	winner domain.WinningTeam,
	oneSided bool,
) *domain.Match {
	t.Helper()
	team := func(teamID string, start int) domain.MatchTeam {
		result := domain.MatchTeam{ID: teamID}
		for index := 0; index < 5; index++ {
			rating := 1480 + float64((start+index)*3)
			result.AverageRating += rating
			result.Players = append(result.Players, domain.MatchPlayer{
				PlayerID: "player-" + twoDigits(start+index), TicketID: "ticket-" + twoDigits(start+index),
				Role: analysisRoles[index], Rating: rating,
			})
		}
		result.AverageRating /= 5
		return result
	}
	match, err := domain.NewMatch(domain.NewMatchParams{
		ID: id, Mode: "ranked_5v5", TeamA: team(id+"-a", 0), TeamB: team(id+"-b", 5),
		ServerRegion: "hongkong", PolicyVersion: policyVersion,
		Quality: domain.MatchQuality{
			TotalScore: predictedQuality, SkillScore: 90, RoleScore: 90, LatencyScore: 90,
			PartyScore: 100, WaitScore: 90, PredictedWinRateA: predictedWinRateA,
			PredictedWinRateB: 1 - predictedWinRateA,
		},
		CreatedAt: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := match.StartAllocation(createdAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := match.MarkReady("127.0.0.1:7000", "token", createdAt.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := match.Start(createdAt.Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	scoreA, scoreB := 20, 15
	if winner == domain.WinningTeamB {
		scoreA, scoreB = scoreB, scoreA
	}
	if err := match.Complete(domain.MatchResult{
		WinningTeam: winner, RandomSeed: 42, DurationSeconds: 1200,
		ScoreA: scoreA, ScoreB: scoreB, MaxAdvantage: 5000,
		OneSided: oneSided, ActualQualityScore: actualQuality,
	}, createdAt.Add(20*time.Minute)); err != nil {
		t.Fatal(err)
	}
	return match
}

func ticketMapIDs(tickets analysisTicketStub) []string {
	result := make([]string, 0, len(tickets))
	for ticketID := range tickets {
		result = append(result, ticketID)
	}
	sort.Strings(result)
	return result
}

func twoDigits(value int) string {
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
