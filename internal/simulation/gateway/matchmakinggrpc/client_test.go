package matchmakinggrpc

import (
	"testing"

	matchmakingv1 "github.com/ccKccK-JF/MatchMind/gen/go/matchmind/matchmaking/v1"
)

func TestTeamSimulationFactorsAveragePlayerSnapshots(t *testing.T) {
	proficiency, behavior := teamSimulationFactors(&matchmakingv1.Team{PlayerDetails: []*matchmakingv1.TeamPlayer{
		{HeroId: "starblade", HeroProficiency: 90, BehaviorScore: 100},
		{HeroId: "emberlord", HeroProficiency: 70, BehaviorScore: 80},
	}})
	if proficiency != 80 || behavior != 90 {
		t.Fatalf("simulation factors = %v/%v", proficiency, behavior)
	}
}

func TestTeamSimulationFactorsUseNeutralLegacyDefaults(t *testing.T) {
	proficiency, behavior := teamSimulationFactors(&matchmakingv1.Team{PlayerDetails: []*matchmakingv1.TeamPlayer{{}}})
	if proficiency != 50 || behavior != 100 {
		t.Fatalf("legacy simulation factors = %v/%v", proficiency, behavior)
	}
	proficiency, behavior = teamSimulationFactors(nil)
	if proficiency != 50 || behavior != 100 {
		t.Fatalf("missing team simulation factors = %v/%v", proficiency, behavior)
	}
}
