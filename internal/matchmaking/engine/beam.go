package engine

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

type TeamSearchDiagnostics struct {
	Algorithm           domain.TeamAlgorithm
	BeamWidth           int
	CandidateSets       int
	FormationsEvaluated int
	Duration            time.Duration
}

type AlgorithmResult struct {
	Formation   TeamFormation
	Quality     MatchQuality
	Diagnostics TeamSearchDiagnostics
}

type AlgorithmComparison struct {
	Greedy AlgorithmResult
	Beam   AlgorithmResult
}

type subsetState struct {
	groups    []ticketGroup
	players   int
	ageCost   int
	score     float64
	signature string
}

func OptimizeTeams(
	candidates CandidateResult,
	serverRegion string,
	now time.Time,
	policy domain.MatchPolicy,
) (TeamFormation, MatchQuality, TeamSearchDiagnostics, error) {
	startedAt := time.Now()
	diagnostics := TeamSearchDiagnostics{Algorithm: policy.TeamAlgorithm, BeamWidth: policy.BeamWidth}
	if err := policy.Validate(); err != nil {
		return TeamFormation{}, MatchQuality{}, diagnostics, err
	}
	if policy.TeamAlgorithm == domain.TeamAlgorithmGreedy {
		formation, err := formTeamsGreedy(candidates, policy, now)
		if err != nil {
			return TeamFormation{}, MatchQuality{}, diagnostics, err
		}
		quality, err := EvaluateQuality(formation, serverRegion, now, policy)
		diagnostics.CandidateSets = 1
		diagnostics.FormationsEvaluated = 1
		diagnostics.Duration = time.Since(startedAt)
		return formation, quality, diagnostics, err
	}

	states, err := beamCandidateSets(candidates, policy)
	if err != nil {
		return TeamFormation{}, MatchQuality{}, diagnostics, err
	}
	diagnostics.CandidateSets = len(states)
	bestScore := math.Inf(-1)
	bestSignature := ""
	var bestFormation TeamFormation
	var bestQuality MatchQuality
	teamCache := make(map[string]Team)
	buildTeam := func(groups []ticketGroup) (Team, error) {
		key := subsetSignature(groups)
		if team, exists := teamCache[key]; exists {
			return cloneTeam(team), nil
		}
		team, err := buildAssignedTeam(flattenGroups(groups), policy.TeamSize, now, policy)
		if err != nil {
			return Team{}, err
		}
		teamCache[key] = cloneTeam(team)
		return team, nil
	}
	for _, state := range states {
		err := visitPartitions(state.groups, policy.TeamSize, func(teamAGroups, teamBGroups []ticketGroup) error {
			teamA, err := buildTeam(teamAGroups)
			if err != nil {
				return nil
			}
			teamB, err := buildTeam(teamBGroups)
			if err != nil {
				return nil
			}
			formation := TeamFormation{TeamA: teamA, TeamB: teamB}
			quality, err := EvaluateQuality(formation, serverRegion, now, policy)
			if err != nil {
				return err
			}
			diagnostics.FormationsEvaluated++
			signature := formationSignature(formation)
			if quality.TotalScore > bestScore ||
				(math.Abs(quality.TotalScore-bestScore) < 1e-9 && (bestSignature == "" || signature < bestSignature)) {
				bestScore = quality.TotalScore
				bestSignature = signature
				bestFormation = formation
				bestQuality = quality
			}
			return nil
		})
		if err != nil {
			return TeamFormation{}, MatchQuality{}, diagnostics, err
		}
	}
	diagnostics.Duration = time.Since(startedAt)
	if diagnostics.FormationsEvaluated == 0 {
		return TeamFormation{}, MatchQuality{}, diagnostics, ErrNoValidTeamSplit
	}
	return bestFormation, bestQuality, diagnostics, nil
}

func CompareTeamAlgorithms(
	candidates CandidateResult,
	serverRegion string,
	now time.Time,
	basePolicy domain.MatchPolicy,
) (AlgorithmComparison, error) {
	greedyPolicy := basePolicy
	greedyPolicy.Version = "comparison-greedy"
	greedyPolicy.TeamAlgorithm = domain.TeamAlgorithmGreedy
	greedyFormation, greedyQuality, greedyDiagnostics, err := OptimizeTeams(candidates, serverRegion, now, greedyPolicy)
	if err != nil {
		return AlgorithmComparison{}, err
	}
	beamPolicy := basePolicy
	beamPolicy.Version = "comparison-beam"
	beamPolicy.TeamAlgorithm = domain.TeamAlgorithmBeam
	beamFormation, beamQuality, beamDiagnostics, err := OptimizeTeams(candidates, serverRegion, now, beamPolicy)
	if err != nil {
		return AlgorithmComparison{}, err
	}
	return AlgorithmComparison{
		Greedy: AlgorithmResult{Formation: greedyFormation, Quality: greedyQuality, Diagnostics: greedyDiagnostics},
		Beam:   AlgorithmResult{Formation: beamFormation, Quality: beamQuality, Diagnostics: beamDiagnostics},
	}, nil
}

func beamCandidateSets(candidates CandidateResult, policy domain.MatchPolicy) ([]subsetState, error) {
	if len(candidates.Tickets) < policy.TeamSize*2 || candidates.Anchor == nil {
		return nil, ErrInsufficientPlayers
	}
	groups := groupTickets(cloneAndSortTickets(candidates.Tickets))
	moveAnchorGroupFirst(groups, candidates.Anchor)
	if len(groups) == 0 || len(groups[0].tickets) > policy.TeamSize {
		return nil, ErrNoValidTeamSplit
	}
	target := policy.TeamSize * 2
	initial := subsetState{
		groups:  []ticketGroup{groups[0]},
		players: len(groups[0].tickets),
	}
	initial.score = subsetHeuristic(initial, 1, len(groups), target)
	initial.signature = subsetSignature(initial.groups)
	beam := []subsetState{initial}
	for groupIndex := 1; groupIndex < len(groups); groupIndex++ {
		group := groups[groupIndex]
		remainingPlayers := groupPlayerCount(groups[groupIndex+1:])
		next := make([]subsetState, 0, len(beam)*2)
		for _, state := range beam {
			if state.players+remainingPlayers >= target {
				skipped := cloneSubsetState(state)
				skipped.score = subsetHeuristic(skipped, groupIndex+1, len(groups), target)
				skipped.signature = subsetSignature(skipped.groups)
				next = append(next, skipped)
			}
			if state.players+len(group.tickets) <= target {
				selected := cloneSubsetState(state)
				selected.groups = append(selected.groups, group)
				selected.players += len(group.tickets)
				selected.ageCost += groupIndex * len(group.tickets)
				selected.score = subsetHeuristic(selected, groupIndex+1, len(groups), target)
				selected.signature = subsetSignature(selected.groups)
				next = append(next, selected)
			}
		}
		if len(next) == 0 {
			return nil, ErrInsufficientPlayers
		}
		sort.SliceStable(next, func(i, j int) bool {
			if math.Abs(next[i].score-next[j].score) > 1e-9 {
				return next[i].score > next[j].score
			}
			return next[i].signature < next[j].signature
		})
		if len(next) > policy.BeamWidth {
			next = next[:policy.BeamWidth]
		}
		beam = next
	}
	result := make([]subsetState, 0, len(beam))
	for _, state := range beam {
		if state.players == target {
			result = append(result, state)
		}
	}
	if len(result) == 0 {
		return nil, ErrInsufficientPlayers
	}
	return result, nil
}

func subsetHeuristic(state subsetState, processedGroups, totalGroups, target int) float64 {
	tickets := flattenGroups(state.groups)
	roleCoverage := make(map[domain.Role]struct{}, len(canonicalRoles))
	var ratingTotal float64
	for _, ticket := range tickets {
		ratingTotal += ticket.Rating()
		for _, role := range ticket.PreferredRoles() {
			roleCoverage[role] = struct{}{}
		}
	}
	average := ratingTotal / float64(max(1, len(tickets)))
	var variance float64
	for _, ticket := range tickets {
		difference := ticket.Rating() - average
		variance += difference * difference
	}
	variance = math.Sqrt(variance / float64(max(1, len(tickets))))
	expected := int(math.Round(float64(processedGroups) / float64(totalGroups) * float64(target)))
	if expected < len(state.groups[0].tickets) {
		expected = len(state.groups[0].tickets)
	}
	fillPenalty := math.Abs(float64(state.players-expected)) * 30
	return float64(len(roleCoverage))*150 - variance*0.35 - float64(state.ageCost)*0.2 - fillPenalty
}

func visitPartitions(groups []ticketGroup, teamSize int, visit func([]ticketGroup, []ticketGroup) error) error {
	if len(groups) == 0 || len(groups) >= 63 {
		return ErrNoValidTeamSplit
	}
	limit := uint64(1) << len(groups)
	for mask := uint64(1); mask < limit; mask++ {
		if mask&1 == 0 {
			continue
		}
		var teamA, teamB []ticketGroup
		playersA := 0
		for index, group := range groups {
			if mask&(uint64(1)<<index) != 0 {
				teamA = append(teamA, group)
				playersA += len(group.tickets)
			} else {
				teamB = append(teamB, group)
			}
		}
		if playersA == teamSize && groupPlayerCount(teamB) == teamSize {
			if err := visit(teamA, teamB); err != nil {
				return err
			}
		}
	}
	return nil
}

func cloneSubsetState(state subsetState) subsetState {
	state.groups = append([]ticketGroup(nil), state.groups...)
	return state
}

func cloneTeam(team Team) Team {
	clone := team
	clone.Players = make([]AssignedPlayer, len(team.Players))
	for index, player := range team.Players {
		clone.Players[index] = player
		clone.Players[index].Ticket = player.Ticket.Clone()
	}
	return clone
}

func subsetSignature(groups []ticketGroup) string {
	ids := make([]string, 0, groupPlayerCount(groups))
	for _, group := range groups {
		for _, ticket := range group.tickets {
			ids = append(ids, ticket.ID())
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, "\x00")
}

func formationSignature(formation TeamFormation) string {
	teamIDs := func(team Team) string {
		ids := make([]string, 0, len(team.Players))
		for _, player := range team.Players {
			ids = append(ids, player.Ticket.ID()+":"+string(player.Role))
		}
		sort.Strings(ids)
		return strings.Join(ids, ",")
	}
	return teamIDs(formation.TeamA) + "|" + teamIDs(formation.TeamB)
}

func candidateRegion(candidates CandidateResult) string {
	if candidates.Anchor != nil {
		return candidates.Anchor.Region()
	}
	return ""
}

func candidateReferenceTime(candidates CandidateResult) time.Time {
	var latest time.Time
	for _, ticket := range candidates.Tickets {
		if ticket != nil && ticket.CreatedAt().After(latest) {
			latest = ticket.CreatedAt()
		}
	}
	return latest
}
