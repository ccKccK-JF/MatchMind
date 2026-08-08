package application

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/engine"
)

type RegionSelection struct {
	Region                   string
	AvailableServers         int
	AverageLatencyMS         float64
	MaxLatencyMS             int
	TeamAverageDifferenceMS  float64
	LatencyStandardDeviation float64
	Score                    float64
	Formation                engine.TeamFormation
	Quality                  engine.MatchQuality
	Diagnostics              engine.TeamSearchDiagnostics
}

func selectServerRegion(
	ctx context.Context,
	allocator ServerAllocator,
	candidates engine.CandidateResult,
	now time.Time,
	policy domain.MatchPolicy,
) (RegionSelection, error) {
	capacities, err := allocator.Capacities(ctx)
	if err != nil {
		return RegionSelection{}, err
	}
	normalized := normalizeCapacities(capacities)
	if len(normalized) == 0 {
		return RegionSelection{}, ErrNoServerCapacity
	}
	maxAvailable := 0
	for _, capacity := range normalized {
		if capacity.AvailableServers > maxAvailable {
			maxAvailable = capacity.AvailableServers
		}
	}
	var best RegionSelection
	found := false
	for _, capacity := range normalized {
		if capacity.AvailableServers <= 0 {
			continue
		}
		formation, quality, diagnostics, optimizeErr := engine.OptimizeTeams(candidates, capacity.Region, now, policy)
		if optimizeErr != nil {
			continue
		}
		selection, eligible := scoreServerRegion(formation, capacity, maxAvailable, candidates.AdmissibleLatencyMS, policy.MaxLatencyMS)
		if !eligible {
			continue
		}
		selection.Formation = formation
		selection.Quality = quality
		selection.Diagnostics = diagnostics
		if !found || betterRegionSelection(selection, best) {
			best = selection
			found = true
		}
	}
	if !found {
		return RegionSelection{}, ErrNoSuitableServerRegion
	}
	return best, nil
}

func normalizeCapacities(capacities []RegionCapacity) []RegionCapacity {
	combined := make(map[string]int, len(capacities))
	for _, capacity := range capacities {
		region := strings.ToLower(strings.TrimSpace(capacity.Region))
		if region != "" && capacity.AvailableServers > 0 {
			combined[region] += capacity.AvailableServers
		}
	}
	result := make([]RegionCapacity, 0, len(combined))
	for region, available := range combined {
		result = append(result, RegionCapacity{Region: region, AvailableServers: available})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Region < result[j].Region })
	return result
}

func scoreServerRegion(
	formation engine.TeamFormation,
	capacity RegionCapacity,
	maxAvailable, admissibleLatencyMS, hardLatencyMS int,
) (RegionSelection, bool) {
	teamLatencies := make([][]float64, 2)
	for teamIndex, team := range []engine.Team{formation.TeamA, formation.TeamB} {
		for _, player := range team.Players {
			latency, exists := player.Ticket.RegionLatency()[capacity.Region]
			if !exists || latency > admissibleLatencyMS || latency > hardLatencyMS {
				return RegionSelection{}, false
			}
			teamLatencies[teamIndex] = append(teamLatencies[teamIndex], float64(latency))
		}
	}
	all := append(append([]float64(nil), teamLatencies[0]...), teamLatencies[1]...)
	average := mean(all)
	maximum := 0
	var variance float64
	for _, latency := range all {
		if int(latency) > maximum {
			maximum = int(latency)
		}
		difference := latency - average
		variance += difference * difference
	}
	standardDeviation := math.Sqrt(variance / float64(len(all)))
	teamDifference := math.Abs(mean(teamLatencies[0]) - mean(teamLatencies[1]))
	denominator := float64(max(1, hardLatencyMS))
	capacityScore := 100 * float64(capacity.AvailableServers) / float64(max(1, maxAvailable))
	score := boundedScore(100-average/denominator*100)*0.35 +
		boundedScore(100-float64(maximum)/denominator*100)*0.25 +
		boundedScore(100-teamDifference/denominator*100)*0.15 +
		boundedScore(100-standardDeviation/denominator*100)*0.15 +
		boundedScore(capacityScore)*0.10
	return RegionSelection{
		Region: capacity.Region, AvailableServers: capacity.AvailableServers,
		AverageLatencyMS: average, MaxLatencyMS: maximum,
		TeamAverageDifferenceMS: teamDifference, LatencyStandardDeviation: standardDeviation,
		Score: score,
	}, true
}

func betterRegionSelection(candidate, current RegionSelection) bool {
	if math.Abs(candidate.Score-current.Score) > 1e-9 {
		return candidate.Score > current.Score
	}
	if math.Abs(candidate.Quality.TotalScore-current.Quality.TotalScore) > 1e-9 {
		return candidate.Quality.TotalScore > current.Quality.TotalScore
	}
	if candidate.AvailableServers != current.AvailableServers {
		return candidate.AvailableServers > current.AvailableServers
	}
	return candidate.Region < current.Region
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var total float64
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func boundedScore(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
