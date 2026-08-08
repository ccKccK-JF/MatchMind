package application

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/player/domain"
	"github.com/ccKccK-JF/MatchMind/internal/rating/elo"
	"github.com/ccKccK-JF/MatchMind/internal/rating/glicko2"
)

var (
	ErrInvalidMatchResult = errors.New("invalid match result")
	ErrRatingConflict     = errors.New("rating update conflict")
)

type MatchOutcome int

const (
	MatchOutcomeUnspecified MatchOutcome = iota
	MatchOutcomeTeamAWin
	MatchOutcomeTeamBWin
	MatchOutcomeDraw
)

type RatingRepository interface {
	GetByID(ctx context.Context, playerID string) (*domain.Player, error)
	ApplyRatingChanges(ctx context.Context, matchID string, changes []*domain.RatingChange) ([]*domain.RatingChange, error)
	RatingHistory(ctx context.Context, playerID string) ([]*domain.RatingChange, error)
}

type RatingService struct {
	repository       RatingRepository
	system           domain.RatingSystem
	eloCalculator    elo.Calculator
	glickoCalculator glicko2.Calculator
	clock            Clock
}

type RecordMatchResultCommand struct {
	MatchID        string
	TeamAPlayerIDs []string
	TeamBPlayerIDs []string
	Outcome        MatchOutcome
	Reason         string
}

func NewRatingService(repository RatingRepository, calculator elo.Calculator, clock Clock) *RatingService {
	if clock == nil {
		clock = defaultClock
	}
	return &RatingService{
		repository: repository, system: domain.RatingSystemElo,
		eloCalculator: calculator, clock: clock,
	}
}

func NewGlicko2RatingService(repository RatingRepository, calculator glicko2.Calculator, clock Clock) *RatingService {
	if clock == nil {
		clock = defaultClock
	}
	return &RatingService{
		repository: repository, system: domain.RatingSystemGlicko2,
		glickoCalculator: calculator, clock: clock,
	}
}

func (s *RatingService) System() domain.RatingSystem {
	return s.system
}

func (s *RatingService) RecordMatchResult(ctx context.Context, command RecordMatchResultCommand) ([]*domain.RatingChange, error) {
	command.MatchID = strings.TrimSpace(command.MatchID)
	command.Reason = strings.TrimSpace(command.Reason)
	if command.MatchID == "" {
		return nil, invalidMatchResult("match id is required")
	}
	if command.Reason == "" {
		command.Reason = "ranked_match"
	}
	if len(command.TeamAPlayerIDs) != 5 || len(command.TeamBPlayerIDs) != 5 {
		return nil, invalidMatchResult("each team must contain exactly five players")
	}
	if command.Outcome < MatchOutcomeTeamAWin || command.Outcome > MatchOutcomeDraw {
		return nil, invalidMatchResult("outcome is required")
	}
	if err := validateUniquePlayers(command.TeamAPlayerIDs, command.TeamBPlayerIDs); err != nil {
		return nil, err
	}

	teamA, err := s.loadPlayers(ctx, command.TeamAPlayerIDs)
	if err != nil {
		return nil, err
	}
	teamB, err := s.loadPlayers(ctx, command.TeamBPlayerIDs)
	if err != nil {
		return nil, err
	}

	scoreA := outcomeScoreA(command.Outcome)
	createdAt := s.clock()
	var changes []*domain.RatingChange
	switch s.system {
	case domain.RatingSystemElo:
		changes, err = s.eloChanges(command, teamA, teamB, scoreA, createdAt)
	case domain.RatingSystemGlicko2:
		changes, err = s.glicko2Changes(command, teamA, teamB, scoreA, createdAt)
	default:
		err = ErrInvalidMatchResult
	}
	if err != nil {
		return nil, err
	}

	return s.repository.ApplyRatingChanges(ctx, command.MatchID, changes)
}

func (s *RatingService) eloChanges(
	command RecordMatchResultCommand,
	teamA, teamB []*domain.Player,
	scoreA float64,
	createdAt time.Time,
) ([]*domain.RatingChange, error) {
	result, err := s.eloCalculator.UpdatePair(averageRating(teamA), averageRating(teamB), scoreA)
	if err != nil {
		return nil, err
	}
	changes := make([]*domain.RatingChange, 0, 10)
	for _, entry := range []struct {
		players []*domain.Player
		delta   float64
	}{{teamA, result.DeltaA}, {teamB, result.DeltaB}} {
		for _, player := range entry.players {
			before := player.RatingState()
			after := before
			after.Rating += entry.delta
			change, err := newStateChange(player.ID(), command, before, after, domain.RatingSystemElo, createdAt)
			if err != nil {
				return nil, err
			}
			changes = append(changes, change)
		}
	}
	return changes, nil
}

func (s *RatingService) glicko2Changes(
	command RecordMatchResultCommand,
	teamA, teamB []*domain.Player,
	scoreA float64,
	createdAt time.Time,
) ([]*domain.RatingChange, error) {
	teamAState := aggregateTeamRating(teamA)
	teamBState := aggregateTeamRating(teamB)
	changes := make([]*domain.RatingChange, 0, 10)
	for _, entry := range []struct {
		players  []*domain.Player
		opponent glicko2.Rating
		score    float64
	}{
		{teamA, teamBState, scoreA},
		{teamB, teamAState, 1 - scoreA},
	} {
		for _, player := range entry.players {
			before := player.RatingState()
			updated, err := s.glickoCalculator.Update(glickoRating(before), []glicko2.Result{{
				Opponent: entry.opponent, Score: entry.score,
			}})
			if err != nil {
				return nil, err
			}
			after := domain.RatingState{Rating: updated.Rating, Deviation: updated.Deviation, Volatility: updated.Volatility}
			change, err := newStateChange(player.ID(), command, before, after, domain.RatingSystemGlicko2, createdAt)
			if err != nil {
				return nil, err
			}
			changes = append(changes, change)
		}
	}
	return changes, nil
}

func aggregateTeamRating(players []*domain.Player) glicko2.Rating {
	var ratingTotal, deviationSquares, volatilityTotal float64
	for _, player := range players {
		ratingTotal += player.Rating()
		deviationSquares += player.RatingDeviation() * player.RatingDeviation()
		volatilityTotal += player.RatingVolatility()
	}
	count := float64(len(players))
	return glicko2.Rating{
		Rating:     ratingTotal / count,
		Deviation:  math.Sqrt(deviationSquares) / count,
		Volatility: volatilityTotal / count,
	}
}

func glickoRating(state domain.RatingState) glicko2.Rating {
	return glicko2.Rating{Rating: state.Rating, Deviation: state.Deviation, Volatility: state.Volatility}
}

func newStateChange(
	playerID string,
	command RecordMatchResultCommand,
	before, after domain.RatingState,
	system domain.RatingSystem,
	createdAt time.Time,
) (*domain.RatingChange, error) {
	return domain.NewRatingChangeWithState(domain.NewRatingChangeParams{
		PlayerID: playerID, MatchID: command.MatchID, Before: before, After: after,
		System: system, Reason: command.Reason, CreatedAt: createdAt,
	})
}

func (s *RatingService) History(ctx context.Context, playerID string) ([]*domain.RatingChange, error) {
	if strings.TrimSpace(playerID) == "" {
		return nil, invalidMatchResult("player id is required")
	}
	return s.repository.RatingHistory(ctx, playerID)
}

func (s *RatingService) loadPlayers(ctx context.Context, playerIDs []string) ([]*domain.Player, error) {
	players := make([]*domain.Player, 0, len(playerIDs))
	for _, playerID := range playerIDs {
		player, err := s.repository.GetByID(ctx, playerID)
		if err != nil {
			return nil, err
		}
		players = append(players, player)
	}
	return players, nil
}

func validateUniquePlayers(teamA, teamB []string) error {
	seen := make(map[string]struct{}, len(teamA)+len(teamB))
	for _, playerID := range append(append([]string(nil), teamA...), teamB...) {
		playerID = strings.TrimSpace(playerID)
		if playerID == "" {
			return invalidMatchResult("player id is required")
		}
		if _, exists := seen[playerID]; exists {
			return invalidMatchResult(fmt.Sprintf("player %q appears more than once", playerID))
		}
		seen[playerID] = struct{}{}
	}
	return nil
}

func averageRating(players []*domain.Player) float64 {
	var total float64
	for _, player := range players {
		total += player.Rating()
	}
	return total / float64(len(players))
}

func outcomeScoreA(outcome MatchOutcome) float64 {
	switch outcome {
	case MatchOutcomeTeamAWin:
		return 1
	case MatchOutcomeDraw:
		return 0.5
	default:
		return 0
	}
}

func invalidMatchResult(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidMatchResult, message)
}
