package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/game/hero"
	gamemode "github.com/ccKccK-JF/MatchMind/internal/game/mode"
)

var (
	ErrInvalidMatch           = errors.New("invalid match")
	ErrIllegalMatchTransition = errors.New("illegal match state transition")
)

type MatchState string

const (
	MatchStateCreated    MatchState = "CREATED"
	MatchStateAllocating MatchState = "ALLOCATING"
	MatchStateReady      MatchState = "READY"
	MatchStateRunning    MatchState = "RUNNING"
	MatchStateFinished   MatchState = "FINISHED"
	MatchStateFailed     MatchState = "FAILED"
)

type MatchPlayer struct {
	PlayerID        string
	TicketID        string
	PartyID         string
	Role            Role
	Rating          float64
	HeroID          string
	HeroProficiency float64
	BehaviorScore   float64
}

type MatchTeam struct {
	ID            string
	Players       []MatchPlayer
	AverageRating float64
}

type MatchQuality struct {
	TotalScore        float64
	SkillScore        float64
	RoleScore         float64
	LatencyScore      float64
	PartyScore        float64
	WaitScore         float64
	PredictedWinRateA float64
	PredictedWinRateB float64
	Reasons           []string
}

type WinningTeam string

const (
	WinningTeamA WinningTeam = "A"
	WinningTeamB WinningTeam = "B"
)

type MatchResult struct {
	WinningTeam        WinningTeam
	RandomSeed         int64
	DurationSeconds    int
	ScoreA             int
	ScoreB             int
	MaxAdvantage       float64
	HasAFK             bool
	Surrendered        bool
	OneSided           bool
	ActualQualityScore float64
}

type NewMatchParams struct {
	ID            string
	Mode          string
	TeamA         MatchTeam
	TeamB         MatchTeam
	ServerRegion  string
	PolicyVersion string
	Quality       MatchQuality
	CreatedAt     time.Time
}

// MatchSnapshot is the persistence representation of a Match aggregate.
// RestoreMatch validates the snapshot before hydrating private domain state.
type MatchSnapshot struct {
	ID              string
	Mode            string
	TeamA           MatchTeam
	TeamB           MatchTeam
	State           MatchState
	ServerRegion    string
	ServerAddress   string
	ConnectionToken string
	PolicyVersion   string
	Quality         MatchQuality
	Result          *MatchResult
	Revision        int64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type Match struct {
	id              string
	mode            string
	teamA           MatchTeam
	teamB           MatchTeam
	state           MatchState
	serverRegion    string
	serverAddress   string
	connectionToken string
	policyVersion   string
	quality         MatchQuality
	result          *MatchResult
	revision        int64
	createdAt       time.Time
	updatedAt       time.Time
}

func NewMatch(params NewMatchParams) (*Match, error) {
	params.ID = strings.TrimSpace(params.ID)
	params.Mode = strings.TrimSpace(params.Mode)
	params.ServerRegion = strings.TrimSpace(params.ServerRegion)
	params.PolicyVersion = strings.TrimSpace(params.PolicyVersion)
	if params.ID == "" || params.Mode == "" || params.ServerRegion == "" || params.PolicyVersion == "" || params.CreatedAt.IsZero() {
		return nil, ErrInvalidMatch
	}
	modeID, err := gamemode.Parse(params.Mode)
	if err != nil {
		return nil, ErrInvalidMatch
	}
	params.Mode = string(modeID)
	if err := validateMatchTeams(params.TeamA, params.TeamB); err != nil {
		return nil, err
	}
	if !scoreInRange(params.Quality.TotalScore) || !probability(params.Quality.PredictedWinRateA) || !probability(params.Quality.PredictedWinRateB) {
		return nil, ErrInvalidMatch
	}
	createdAt := params.CreatedAt.UTC()
	return &Match{
		id:            params.ID,
		mode:          params.Mode,
		teamA:         cloneMatchTeam(params.TeamA),
		teamB:         cloneMatchTeam(params.TeamB),
		state:         MatchStateCreated,
		serverRegion:  params.ServerRegion,
		policyVersion: params.PolicyVersion,
		quality:       cloneMatchQuality(params.Quality),
		revision:      1,
		createdAt:     createdAt,
		updatedAt:     createdAt,
	}, nil
}

func RestoreMatch(snapshot MatchSnapshot) (*Match, error) {
	match, err := NewMatch(NewMatchParams{
		ID: snapshot.ID, Mode: snapshot.Mode, TeamA: snapshot.TeamA, TeamB: snapshot.TeamB,
		ServerRegion: snapshot.ServerRegion, PolicyVersion: snapshot.PolicyVersion,
		Quality: snapshot.Quality, CreatedAt: snapshot.CreatedAt,
	})
	if err != nil || snapshot.Revision < 1 || snapshot.UpdatedAt.IsZero() || snapshot.UpdatedAt.Before(snapshot.CreatedAt) {
		return nil, ErrInvalidMatch
	}
	if err := validateRestoredMatchState(snapshot); err != nil {
		return nil, err
	}
	match.state = snapshot.State
	match.serverAddress = strings.TrimSpace(snapshot.ServerAddress)
	match.connectionToken = strings.TrimSpace(snapshot.ConnectionToken)
	match.result = cloneMatchResult(snapshot.Result)
	match.revision = snapshot.Revision
	match.updatedAt = snapshot.UpdatedAt.UTC()
	return match, nil
}

func (m *Match) StartAllocation(now time.Time) error {
	return m.transition(MatchStateCreated, MatchStateAllocating, now)
}

func (m *Match) MarkReady(address, token string, now time.Time) error {
	if m.state != MatchStateAllocating {
		return matchTransitionError(m.state, MatchStateReady)
	}
	address = strings.TrimSpace(address)
	token = strings.TrimSpace(token)
	if address == "" || token == "" {
		return ErrInvalidMatch
	}
	m.serverAddress = address
	m.connectionToken = token
	m.state = MatchStateReady
	m.revision++
	m.updatedAt = now.UTC()
	return nil
}

func (m *Match) Start(now time.Time) error {
	return m.transition(MatchStateReady, MatchStateRunning, now)
}

func (m *Match) Complete(result MatchResult, now time.Time) error {
	if m.state == MatchStateFinished {
		return nil
	}
	if m.state != MatchStateRunning {
		return matchTransitionError(m.state, MatchStateFinished)
	}
	if err := validateMatchResult(result); err != nil {
		return err
	}
	m.result = cloneMatchResult(&result)
	m.state = MatchStateFinished
	m.revision++
	m.updatedAt = now.UTC()
	return nil
}

func (m *Match) Fail(now time.Time) error {
	switch m.state {
	case MatchStateCreated, MatchStateAllocating, MatchStateReady, MatchStateRunning:
		m.state = MatchStateFailed
		m.revision++
		m.updatedAt = now.UTC()
		return nil
	default:
		return matchTransitionError(m.state, MatchStateFailed)
	}
}

func (m *Match) ID() string              { return m.id }
func (m *Match) Mode() string            { return m.mode }
func (m *Match) TeamA() MatchTeam        { return cloneMatchTeam(m.teamA) }
func (m *Match) TeamB() MatchTeam        { return cloneMatchTeam(m.teamB) }
func (m *Match) State() MatchState       { return m.state }
func (m *Match) ServerRegion() string    { return m.serverRegion }
func (m *Match) ServerAddress() string   { return m.serverAddress }
func (m *Match) ConnectionToken() string { return m.connectionToken }
func (m *Match) PolicyVersion() string   { return m.policyVersion }
func (m *Match) Quality() MatchQuality   { return cloneMatchQuality(m.quality) }
func (m *Match) CreatedAt() time.Time    { return m.createdAt }
func (m *Match) UpdatedAt() time.Time    { return m.updatedAt }
func (m *Match) Revision() int64         { return m.revision }
func (m *Match) Result() (MatchResult, bool) {
	if m.result == nil {
		return MatchResult{}, false
	}
	return *cloneMatchResult(m.result), true
}

func (m *Match) Snapshot() MatchSnapshot {
	if m == nil {
		return MatchSnapshot{}
	}
	return MatchSnapshot{
		ID: m.id, Mode: m.mode, TeamA: cloneMatchTeam(m.teamA), TeamB: cloneMatchTeam(m.teamB),
		State: m.state, ServerRegion: m.serverRegion, ServerAddress: m.serverAddress,
		ConnectionToken: m.connectionToken, PolicyVersion: m.policyVersion,
		Quality: cloneMatchQuality(m.quality), Result: cloneMatchResult(m.result),
		Revision: m.revision, CreatedAt: m.createdAt, UpdatedAt: m.updatedAt,
	}
}

func (m *Match) Clone() *Match {
	if m == nil {
		return nil
	}
	clone := *m
	clone.teamA = cloneMatchTeam(m.teamA)
	clone.teamB = cloneMatchTeam(m.teamB)
	clone.quality = cloneMatchQuality(m.quality)
	clone.result = cloneMatchResult(m.result)
	return &clone
}

func (m *Match) transition(from, to MatchState, now time.Time) error {
	if m.state != from {
		return matchTransitionError(m.state, to)
	}
	m.state = to
	m.revision++
	m.updatedAt = now.UTC()
	return nil
}

func validateRestoredMatchState(snapshot MatchSnapshot) error {
	hasAddress := strings.TrimSpace(snapshot.ServerAddress) != ""
	hasToken := strings.TrimSpace(snapshot.ConnectionToken) != ""
	if hasAddress != hasToken {
		return ErrInvalidMatch
	}
	switch snapshot.State {
	case MatchStateCreated, MatchStateAllocating:
		if hasAddress || snapshot.Result != nil {
			return ErrInvalidMatch
		}
	case MatchStateReady, MatchStateRunning:
		if !hasAddress || snapshot.Result != nil {
			return ErrInvalidMatch
		}
	case MatchStateFinished:
		if !hasAddress || snapshot.Result == nil || validateMatchResult(*snapshot.Result) != nil {
			return ErrInvalidMatch
		}
	case MatchStateFailed:
		if snapshot.Result != nil {
			return ErrInvalidMatch
		}
	default:
		return ErrInvalidMatch
	}
	return nil
}

func validateMatchTeams(teamA, teamB MatchTeam) error {
	if strings.TrimSpace(teamA.ID) == "" || strings.TrimSpace(teamB.ID) == "" || len(teamA.Players) != 5 || len(teamB.Players) != 5 {
		return ErrInvalidMatch
	}
	seenPlayers := make(map[string]struct{}, 10)
	seenTickets := make(map[string]struct{}, 10)
	for _, player := range append(append([]MatchPlayer(nil), teamA.Players...), teamB.Players...) {
		if strings.TrimSpace(player.PlayerID) == "" || strings.TrimSpace(player.TicketID) == "" || player.Rating <= 0 ||
			!validMatchRole(player.Role) || !scoreInRange(player.HeroProficiency) || !scoreInRange(player.BehaviorScore) {
			return ErrInvalidMatch
		}
		heroID := strings.TrimSpace(player.HeroID)
		// Matches persisted before hero assignment was introduced have no HeroID.
		// Keep those snapshots readable, while all newly-created worker matches carry one.
		if heroID == "" {
			if player.HeroProficiency != 0 {
				return ErrInvalidMatch
			}
		} else {
			entry, exists := hero.Get(heroID)
			if !exists || entry.Role != string(player.Role) {
				return ErrInvalidMatch
			}
		}
		if _, duplicate := seenPlayers[player.PlayerID]; duplicate {
			return ErrInvalidMatch
		}
		if _, duplicate := seenTickets[player.TicketID]; duplicate {
			return ErrInvalidMatch
		}
		seenPlayers[player.PlayerID] = struct{}{}
		seenTickets[player.TicketID] = struct{}{}
	}
	return nil
}

func validMatchRole(role Role) bool {
	switch role {
	case RoleVanguard, RoleRoamer, RoleCore, RoleRanged, RoleSupport:
		return true
	default:
		return false
	}
}

func cloneMatchTeam(team MatchTeam) MatchTeam {
	team.Players = append([]MatchPlayer(nil), team.Players...)
	return team
}

func cloneMatchQuality(quality MatchQuality) MatchQuality {
	quality.Reasons = append([]string(nil), quality.Reasons...)
	return quality
}

func cloneMatchResult(result *MatchResult) *MatchResult {
	if result == nil {
		return nil
	}
	clone := *result
	return &clone
}

func validateMatchResult(result MatchResult) error {
	if result.WinningTeam != WinningTeamA && result.WinningTeam != WinningTeamB {
		return ErrInvalidMatch
	}
	if result.DurationSeconds < 0 || result.ScoreA < 0 || result.ScoreB < 0 || result.ScoreA == result.ScoreB {
		return ErrInvalidMatch
	}
	if result.MaxAdvantage < 0 || !scoreInRange(result.ActualQualityScore) {
		return ErrInvalidMatch
	}
	return nil
}

func scoreInRange(score float64) bool {
	return !math.IsNaN(score) && !math.IsInf(score, 0) && score >= 0 && score <= 100
}

func probability(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value <= 1
}

func matchTransitionError(from, to MatchState) error {
	return fmt.Errorf("%w: %s -> %s", ErrIllegalMatchTransition, from, to)
}
