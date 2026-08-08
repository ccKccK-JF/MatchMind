package engine

import (
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

var canonicalRoles = []domain.Role{
	domain.RoleVanguard,
	domain.RoleRoamer,
	domain.RoleCore,
	domain.RoleRanged,
	domain.RoleSupport,
}

type AssignedPlayer struct {
	Ticket          *domain.MatchTicket
	Role            domain.Role
	PreferenceScore float64
}

type Team struct {
	Players       []AssignedPlayer
	AverageRating float64
	RoleScore     float64
}

type TeamFormation struct {
	TeamA Team
	TeamB Team
}

func FormTeams(candidates CandidateResult, policy domain.MatchPolicy) (TeamFormation, error) {
	now := candidateReferenceTime(candidates)
	if policy.TeamAlgorithm == domain.TeamAlgorithmBeam {
		formation, _, _, err := OptimizeTeams(candidates, candidateRegion(candidates), now, policy)
		return formation, err
	}
	return formTeamsGreedy(candidates, policy, now)
}

func formTeamsGreedy(candidates CandidateResult, policy domain.MatchPolicy, now time.Time) (TeamFormation, error) {
	if err := policy.Validate(); err != nil {
		return TeamFormation{}, err
	}
	if len(candidates.Tickets) < policy.TeamSize*2 || candidates.Anchor == nil {
		return TeamFormation{}, ErrInsufficientPlayers
	}
	selectedGroups, err := selectTenPlayers(candidates.Tickets, candidates.Anchor.ID(), policy.TeamSize*2)
	if err != nil {
		return TeamFormation{}, err
	}
	teamAGroups, teamBGroups, err := balancedPartition(selectedGroups, policy.TeamSize)
	if err != nil {
		return TeamFormation{}, err
	}
	teamA, err := buildAssignedTeam(flattenGroups(teamAGroups), policy.TeamSize, now, policy)
	if err != nil {
		return TeamFormation{}, err
	}
	teamB, err := buildAssignedTeam(flattenGroups(teamBGroups), policy.TeamSize, now, policy)
	if err != nil {
		return TeamFormation{}, err
	}
	return TeamFormation{TeamA: teamA, TeamB: teamB}, nil
}

func selectTenPlayers(candidates []*domain.MatchTicket, anchorID string, target int) ([]ticketGroup, error) {
	groups := groupTickets(cloneAndSortTickets(candidates))
	anchorIndex := -1
	for index, group := range groups {
		for _, ticket := range group.tickets {
			if ticket.ID() == anchorID {
				anchorIndex = index
				break
			}
		}
	}
	if anchorIndex < 0 {
		return nil, ErrNoValidTeamSplit
	}
	groups[0], groups[anchorIndex] = groups[anchorIndex], groups[0]
	if len(groups[0].tickets) > target {
		return nil, ErrNoValidTeamSplit
	}

	combinations := make(map[int][]int)
	combinations[len(groups[0].tickets)] = []int{0}
	for groupIndex := 1; groupIndex < len(groups); groupIndex++ {
		groupSize := len(groups[groupIndex].tickets)
		for size := target - groupSize; size >= 0; size-- {
			selected, exists := combinations[size]
			if !exists {
				continue
			}
			newSize := size + groupSize
			if _, already := combinations[newSize]; already {
				continue
			}
			combination := append([]int(nil), selected...)
			combinations[newSize] = append(combination, groupIndex)
		}
	}
	indexes, exists := combinations[target]
	if !exists {
		return nil, ErrInsufficientPlayers
	}
	selected := make([]ticketGroup, 0, len(indexes))
	for _, index := range indexes {
		selected = append(selected, groups[index])
	}
	return selected, nil
}

func balancedPartition(groups []ticketGroup, teamSize int) ([]ticketGroup, []ticketGroup, error) {
	if len(groups) == 0 || len(groups) >= 63 {
		return nil, nil, ErrNoValidTeamSplit
	}
	var bestA []ticketGroup
	var bestB []ticketGroup
	bestDifference := math.Inf(1)
	bestSignature := ""
	limit := uint64(1) << len(groups)
	for mask := uint64(1); mask < limit; mask++ {
		if mask&1 == 0 { // The anchor group always defines team A.
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
		if playersA != teamSize || groupPlayerCount(teamB) != teamSize {
			continue
		}
		difference := math.Abs(groupRatingTotal(teamA) - groupRatingTotal(teamB))
		signature := teamSignature(teamA)
		if difference < bestDifference || (difference == bestDifference && (bestSignature == "" || signature < bestSignature)) {
			bestDifference = difference
			bestSignature = signature
			bestA = append([]ticketGroup(nil), teamA...)
			bestB = append([]ticketGroup(nil), teamB...)
		}
	}
	if bestA == nil {
		return nil, nil, ErrNoValidTeamSplit
	}
	return bestA, bestB, nil
}

func buildAssignedTeam(tickets []*domain.MatchTicket, teamSize int, now time.Time, policy domain.MatchPolicy) (Team, error) {
	if len(tickets) != teamSize || teamSize != len(canonicalRoles) {
		return Team{}, ErrNoValidTeamSplit
	}
	sort.SliceStable(tickets, func(i, j int) bool { return tickets[i].PlayerID() < tickets[j].PlayerID() })

	bestScore := -1.0
	var bestRoles []domain.Role
	permuteRoles(append([]domain.Role(nil), canonicalRoles...), 0, func(roles []domain.Role) {
		var score float64
		for index, ticket := range tickets {
			score += rolePreferenceScore(ticket, roles[index], now, policy)
		}
		if score > bestScore {
			bestScore = score
			bestRoles = append([]domain.Role(nil), roles...)
		}
	})

	team := Team{Players: make([]AssignedPlayer, 0, teamSize)}
	for index, ticket := range tickets {
		team.AverageRating += ticket.Rating()
		score := rolePreferenceScore(ticket, bestRoles[index], now, policy)
		team.RoleScore += score
		team.Players = append(team.Players, AssignedPlayer{
			Ticket:          ticket.Clone(),
			Role:            bestRoles[index],
			PreferenceScore: score,
		})
	}
	team.AverageRating /= float64(teamSize)
	team.RoleScore /= float64(teamSize)
	return team, nil
}

func permuteRoles(roles []domain.Role, index int, visit func([]domain.Role)) {
	if index == len(roles) {
		visit(roles)
		return
	}
	for current := index; current < len(roles); current++ {
		roles[index], roles[current] = roles[current], roles[index]
		permuteRoles(roles, index+1, visit)
		roles[index], roles[current] = roles[current], roles[index]
	}
}

func rolePreferenceScore(ticket *domain.MatchTicket, assigned domain.Role, now time.Time, policy domain.MatchPolicy) float64 {
	for index, preferred := range ticket.PreferredRoles() {
		if preferred != assigned {
			continue
		}
		switch index {
		case 0:
			return 100
		case 1:
			return 70
		default:
			return 50
		}
	}
	return policy.NonPreferredRoleScore(now.Sub(ticket.CreatedAt()))
}

func flattenGroups(groups []ticketGroup) []*domain.MatchTicket {
	var tickets []*domain.MatchTicket
	for _, group := range groups {
		for _, ticket := range group.tickets {
			tickets = append(tickets, ticket.Clone())
		}
	}
	return tickets
}

func groupPlayerCount(groups []ticketGroup) int {
	count := 0
	for _, group := range groups {
		count += len(group.tickets)
	}
	return count
}

func groupRatingTotal(groups []ticketGroup) float64 {
	var total float64
	for _, group := range groups {
		for _, ticket := range group.tickets {
			total += ticket.Rating()
		}
	}
	return total
}

func teamSignature(groups []ticketGroup) string {
	var playerIDs []string
	for _, group := range groups {
		for _, ticket := range group.tickets {
			playerIDs = append(playerIDs, ticket.PlayerID())
		}
	}
	sort.Strings(playerIDs)
	return strings.Join(playerIDs, "\x00")
}
