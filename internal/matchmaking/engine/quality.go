package engine

import (
	"fmt"
	"math"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/ccKccK-JF/MatchMind/internal/rating/elo"
)

type MatchQuality struct {
	TotalScore   float64
	SkillScore   float64
	RoleScore    float64
	LatencyScore float64
	PartyScore   float64
	WaitScore    float64

	PredictedWinRateA float64
	PredictedWinRateB float64
	Reasons           []string
}

func EvaluateQuality(
	formation TeamFormation,
	serverRegion string,
	now time.Time,
	policy domain.MatchPolicy,
) (MatchQuality, error) {
	if err := policy.Validate(); err != nil {
		return MatchQuality{}, err
	}
	if len(formation.TeamA.Players) != policy.TeamSize || len(formation.TeamB.Players) != policy.TeamSize {
		return MatchQuality{}, ErrNoValidTeamSplit
	}

	calculator, _ := elo.NewCalculator(32)
	predictedA, err := calculator.ExpectedScore(formation.TeamA.AverageRating, formation.TeamB.AverageRating)
	if err != nil {
		return MatchQuality{}, err
	}
	quality := MatchQuality{
		PredictedWinRateA: predictedA,
		PredictedWinRateB: 1 - predictedA,
		SkillScore:        clampScore(100 - math.Abs(predictedA-0.5)*200),
		RoleScore:         clampScore((formation.TeamA.RoleScore + formation.TeamB.RoleScore) / 2),
		LatencyScore:      latencyScore(formation, serverRegion, policy.MaxLatencyMS),
		PartyScore:        partySymmetryScore(formation),
		WaitScore:         waitScore(formation, now, policy.TicketTTL),
	}
	quality.TotalScore =
		quality.SkillScore*policy.SkillWeight +
			quality.RoleScore*policy.RoleWeight +
			quality.LatencyScore*policy.LatencyWeight +
			quality.PartyScore*policy.PartyWeight +
			quality.WaitScore*policy.WaitWeight
	quality.TotalScore = clampScore(quality.TotalScore)
	quality.Reasons = qualityReasons(quality)
	return quality, nil
}

func latencyScore(formation TeamFormation, serverRegion string, maximum int) float64 {
	players := append(append([]AssignedPlayer(nil), formation.TeamA.Players...), formation.TeamB.Players...)
	var total int
	maxLatency := 0
	for _, player := range players {
		latency, exists := player.Ticket.RegionLatency()[serverRegion]
		if !exists || latency > maximum {
			return 0
		}
		total += latency
		if latency > maxLatency {
			maxLatency = latency
		}
	}
	average := float64(total) / float64(len(players))
	score := 100 - (average/float64(maximum))*70 - (float64(maxLatency)/float64(maximum))*30
	return clampScore(score)
}

func partySymmetryScore(formation TeamFormation) float64 {
	distributionA := partyDistribution(formation.TeamA)
	distributionB := partyDistribution(formation.TeamB)
	difference := 0
	for size := 1; size <= 5; size++ {
		difference += absoluteInt(distributionA[size] - distributionB[size])
	}
	return clampScore(100 - float64(difference)*10)
}

func partyDistribution(team Team) map[int]int {
	partySizes := make(map[string]int)
	for _, player := range team.Players {
		key := player.Ticket.PartyID()
		if key == "" {
			key = "ticket:" + player.Ticket.ID()
		}
		partySizes[key]++
	}
	distribution := make(map[int]int)
	for _, size := range partySizes {
		distribution[size]++
	}
	return distribution
}

func waitScore(formation TeamFormation, now time.Time, ticketTTL time.Duration) float64 {
	players := append(append([]AssignedPlayer(nil), formation.TeamA.Players...), formation.TeamB.Players...)
	var total time.Duration
	for _, player := range players {
		wait := now.Sub(player.Ticket.CreatedAt())
		if wait > 0 {
			total += wait
		}
	}
	average := total / time.Duration(len(players))
	return clampScore(100 * (1 - float64(average)/float64(ticketTTL)))
}

func qualityReasons(quality MatchQuality) []string {
	var reasons []string
	if quality.SkillScore < 80 {
		reasons = append(reasons, fmt.Sprintf("skill fairness is low (%.1f)", quality.SkillScore))
	}
	if quality.RoleScore < 80 {
		reasons = append(reasons, fmt.Sprintf("role satisfaction is low (%.1f)", quality.RoleScore))
	}
	if quality.LatencyScore < 70 {
		reasons = append(reasons, fmt.Sprintf("network quality is low (%.1f)", quality.LatencyScore))
	}
	if quality.PartyScore < 80 {
		reasons = append(reasons, fmt.Sprintf("party structures are asymmetric (%.1f)", quality.PartyScore))
	}
	if quality.WaitScore < 50 {
		reasons = append(reasons, fmt.Sprintf("average wait is high (%.1f)", quality.WaitScore))
	}
	return reasons
}

func clampScore(score float64) float64 {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func absoluteInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
