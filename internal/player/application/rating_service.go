package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ccKccK-JF/MatchMind/internal/player/domain"
	"github.com/ccKccK-JF/MatchMind/internal/rating/elo"
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
	repository RatingRepository
	calculator elo.Calculator
	clock      Clock
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
	return &RatingService{repository: repository, calculator: calculator, clock: clock}
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
	result, err := s.calculator.UpdatePair(averageRating(teamA), averageRating(teamB), scoreA)
	if err != nil {
		return nil, err
	}

	createdAt := s.clock()
	changes := make([]*domain.RatingChange, 0, 10)
	for _, player := range teamA {
		change, err := domain.NewRatingChange(
			player.ID(), command.MatchID, player.Rating(), player.Rating()+result.DeltaA, command.Reason, createdAt,
		)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}
	for _, player := range teamB {
		change, err := domain.NewRatingChange(
			player.ID(), command.MatchID, player.Rating(), player.Rating()+result.DeltaB, command.Reason, createdAt,
		)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}

	return s.repository.ApplyRatingChanges(ctx, command.MatchID, changes)
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
