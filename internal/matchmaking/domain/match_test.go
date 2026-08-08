package domain

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestMatchStateMachine(t *testing.T) {
	now := time.Now()
	match := newTestMatch(t, now)
	if err := match.StartAllocation(now); err != nil {
		t.Fatal(err)
	}
	if err := match.MarkReady("127.0.0.1:7001", "token", now); err != nil {
		t.Fatal(err)
	}
	if err := match.Start(now); err != nil {
		t.Fatal(err)
	}
	if err := match.Complete(MatchResult{
		WinningTeam: WinningTeamA, DurationSeconds: 1200, ScoreA: 20, ScoreB: 15,
		MaxAdvantage: 5000, ActualQualityScore: 85,
	}, now); err != nil {
		t.Fatal(err)
	}
	if match.State() != MatchStateFinished {
		t.Fatalf("state = %s, want FINISHED", match.State())
	}
	if err := match.Start(now); !errors.Is(err, ErrIllegalMatchTransition) {
		t.Fatalf("Start() error = %v, want ErrIllegalMatchTransition", err)
	}
}

func TestMatchRejectsDuplicatePlayer(t *testing.T) {
	match := validMatchParams(time.Now())
	match.TeamB.Players[0].PlayerID = match.TeamA.Players[0].PlayerID
	_, err := NewMatch(match)
	if !errors.Is(err, ErrInvalidMatch) {
		t.Fatalf("NewMatch() error = %v, want ErrInvalidMatch", err)
	}
}

func TestMatchRejectsHeroAssignedToWrongRole(t *testing.T) {
	params := validMatchParams(time.Now())
	params.TeamA.Players[0].HeroID = "starblade"
	params.TeamA.Players[0].HeroProficiency = 90
	params.TeamA.Players[0].BehaviorScore = 95
	if _, err := NewMatch(params); !errors.Is(err, ErrInvalidMatch) {
		t.Fatalf("NewMatch() error = %v, want ErrInvalidMatch", err)
	}
}

func TestMatchRejectsUnsupportedGameMode(t *testing.T) {
	params := validMatchParams(time.Now())
	params.Mode = "custom_5v5"
	if _, err := NewMatch(params); !errors.Is(err, ErrInvalidMatch) {
		t.Fatalf("NewMatch() error = %v, want ErrInvalidMatch", err)
	}
}

func TestRestoreFinishedMatchSnapshot(t *testing.T) {
	now := time.Now().UTC()
	match := newTestMatch(t, now)
	if err := match.StartAllocation(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := match.MarkReady("127.0.0.1:7001", "token", now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := match.Start(now.Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := match.Complete(MatchResult{
		WinningTeam: WinningTeamB, RandomSeed: 42, DurationSeconds: 900,
		ScoreA: 10, ScoreB: 15, MaxAdvantage: 1200, ActualQualityScore: 88,
	}, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreMatch(match.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if restored.State() != MatchStateFinished || restored.Revision() != 5 {
		t.Fatalf("restored state/revision = %s/%d", restored.State(), restored.Revision())
	}
	if result, ok := restored.Result(); !ok || result.RandomSeed != 42 {
		t.Fatalf("restored result = %#v, %v", result, ok)
	}
}

func TestRestoreMatchRejectsInconsistentState(t *testing.T) {
	snapshot := newTestMatch(t, time.Now().UTC()).Snapshot()
	snapshot.State = MatchStateRunning
	if _, err := RestoreMatch(snapshot); !errors.Is(err, ErrInvalidMatch) {
		t.Fatalf("RestoreMatch() error = %v, want ErrInvalidMatch", err)
	}
}

func newTestMatch(t *testing.T, now time.Time) *Match {
	t.Helper()
	match, err := NewMatch(validMatchParams(now))
	if err != nil {
		t.Fatal(err)
	}
	return match
}

func validMatchParams(now time.Time) NewMatchParams {
	team := func(prefix string) MatchTeam {
		players := make([]MatchPlayer, 0, 5)
		for index := 1; index <= 5; index++ {
			players = append(players, MatchPlayer{
				PlayerID: fmt.Sprintf("%s-player-%d", prefix, index),
				TicketID: fmt.Sprintf("%s-ticket-%d", prefix, index),
				Role:     []Role{RoleVanguard, RoleRoamer, RoleCore, RoleRanged, RoleSupport}[index-1],
				Rating:   1500,
			})
		}
		return MatchTeam{ID: "team-" + prefix, Players: players, AverageRating: 1500}
	}
	return NewMatchParams{
		ID:            "match-1",
		Mode:          "ranked_5v5",
		TeamA:         team("a"),
		TeamB:         team("b"),
		ServerRegion:  "hongkong",
		PolicyVersion: "v1",
		Quality: MatchQuality{
			TotalScore: 90, SkillScore: 100, RoleScore: 100, LatencyScore: 80,
			PartyScore: 100, WaitScore: 90, PredictedWinRateA: 0.5, PredictedWinRateB: 0.5,
		},
		CreatedAt: now,
	}
}
