package application

import (
	"context"
	"errors"
	"strings"
	"sync"

	simulationdomain "github.com/ccKccK-JF/MatchMind/internal/simulation/domain"
)

var (
	ErrMatchNotFound  = errors.New("simulation match not found")
	ErrMatchNotReady  = errors.New("match is not ready for simulation")
	ErrResultNotFound = errors.New("simulation result not found")
)

type MatchSnapshot struct {
	ID                 string
	Mode               string
	State              string
	TeamAPlayerIDs     []string
	TeamBPlayerIDs     []string
	TeamAAverageRating float64
	TeamBAverageRating float64
	PredictedWinRateA  float64
	RoleScore          float64
	LatencyScore       float64
	PartyScore         float64
	ExistingResult     *simulationdomain.Result
}

type MatchGateway interface {
	GetMatch(ctx context.Context, matchID string) (MatchSnapshot, error)
	StartMatch(ctx context.Context, matchID string) error
	CompleteMatch(ctx context.Context, result simulationdomain.Result) error
}

type RatingGateway interface {
	ApplyMatchResult(
		ctx context.Context,
		matchID string,
		teamAPlayerIDs, teamBPlayerIDs []string,
		winningTeam simulationdomain.WinningTeam,
	) error
}

type ResultStore interface {
	Get(ctx context.Context, matchID string) (simulationdomain.Result, error)
	Save(ctx context.Context, result simulationdomain.Result) error
}

type Service struct {
	mu        sync.Mutex
	matches   MatchGateway
	ratings   RatingGateway
	results   ResultStore
	simulator simulationdomain.Simulator
}

func NewService(matches MatchGateway, ratings RatingGateway, results ResultStore, simulator simulationdomain.Simulator) *Service {
	return &Service{matches: matches, ratings: ratings, results: results, simulator: simulator}
}

func (s *Service) SimulateMatch(ctx context.Context, matchID string, randomSeed int64) (simulationdomain.Result, error) {
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return simulationdomain.Result{}, simulationdomain.ErrInvalidSimulation
	}

	// The first implementation serializes result finalization in one process.
	// External persistence will replace this lock with a match-id transaction.
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, err := s.results.Get(ctx, matchID); err == nil {
		return existing, nil
	} else if !errors.Is(err, ErrResultNotFound) {
		return simulationdomain.Result{}, err
	}

	match, err := s.matches.GetMatch(ctx, matchID)
	if err != nil {
		return simulationdomain.Result{}, err
	}
	if match.ExistingResult != nil {
		if err := s.results.Save(ctx, *match.ExistingResult); err != nil {
			return simulationdomain.Result{}, err
		}
		return *match.ExistingResult, nil
	}
	if match.State != "READY" && match.State != "RUNNING" {
		return simulationdomain.Result{}, ErrMatchNotReady
	}
	if match.State == "READY" {
		if err := s.matches.StartMatch(ctx, matchID); err != nil {
			return simulationdomain.Result{}, err
		}
	}

	result, err := s.simulator.Simulate(simulationdomain.Input{
		MatchID: match.ID, RandomSeed: randomSeed,
		RatingA: match.TeamAAverageRating, RatingB: match.TeamBAverageRating,
		PredictedWinRateA: match.PredictedWinRateA,
		RoleScore:         match.RoleScore, LatencyScore: match.LatencyScore, PartyScore: match.PartyScore,
	})
	if err != nil {
		return simulationdomain.Result{}, err
	}
	if match.Mode == "ranked_5v5" {
		if err := s.ratings.ApplyMatchResult(
			ctx, match.ID, match.TeamAPlayerIDs, match.TeamBPlayerIDs, result.WinningTeam,
		); err != nil {
			return simulationdomain.Result{}, err
		}
	}
	if err := s.matches.CompleteMatch(ctx, result); err != nil {
		return simulationdomain.Result{}, err
	}
	if err := s.results.Save(ctx, result); err != nil {
		return simulationdomain.Result{}, err
	}
	return result, nil
}
