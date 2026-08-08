package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/game/hero"
)

const (
	DefaultRatingDeviation  = 350.0
	DefaultRatingVolatility = 0.06
	minimumLatencyMS        = 0
	maximumLatencyMS        = 1000
	minimumBehaviorScore    = 0.0
	maximumBehaviorScore    = 100.0
)

var ErrInvalidPlayer = errors.New("invalid player")

type NewPlayerParams struct {
	ID              string
	Name            string
	InitialRating   float64
	PreferredRoles  []Role
	HomeRegion      string
	RegionLatency   map[string]int
	BehaviorScore   float64
	HeroProficiency map[string]float64
	CreatedAt       time.Time
}

// PlayerSnapshot is the durable representation accepted when restoring a
// player from persistence. RestorePlayer applies the same validation as a new
// player instead of allowing repositories to mutate private fields.
type PlayerSnapshot struct {
	ID               string
	Name             string
	Rating           float64
	RatingDeviation  float64
	RatingVolatility float64
	Banned           bool
	BanReason        string
	BannedAt         time.Time
	BannedBy         string
	PreferredRoles   []Role
	HomeRegion       string
	RegionLatency    map[string]int
	BehaviorScore    float64
	HeroProficiency  map[string]float64
	CreatedAt        time.Time
}

// Player is an immutable player snapshot. Slices and maps are copied at the
// boundary so callers cannot mutate shared repository state accidentally.
type Player struct {
	id               string
	name             string
	rating           float64
	ratingDeviation  float64
	ratingVolatility float64
	banned           bool
	banReason        string
	bannedAt         time.Time
	bannedBy         string
	preferredRoles   []Role
	homeRegion       string
	regionLatency    map[string]int
	behaviorScore    float64
	heroProficiency  map[string]float64
	createdAt        time.Time
}

func NewPlayer(params NewPlayerParams) (*Player, error) {
	params.ID = strings.TrimSpace(params.ID)
	params.Name = strings.TrimSpace(params.Name)
	params.HomeRegion = strings.ToLower(strings.TrimSpace(params.HomeRegion))
	params.RegionLatency = normalizeRegionLatency(params.RegionLatency)

	if params.ID == "" {
		return nil, invalidPlayer("id is required")
	}
	if params.Name == "" {
		return nil, invalidPlayer("name is required")
	}
	if params.InitialRating <= 0 {
		return nil, invalidPlayer("initial rating must be greater than zero")
	}
	if params.HomeRegion == "" {
		return nil, invalidPlayer("home region is required")
	}
	if params.CreatedAt.IsZero() {
		return nil, invalidPlayer("created time is required")
	}
	if params.BehaviorScore < minimumBehaviorScore || params.BehaviorScore > maximumBehaviorScore ||
		math.IsNaN(params.BehaviorScore) || math.IsInf(params.BehaviorScore, 0) {
		return nil, invalidPlayer("behavior score must be between 0 and 100")
	}
	if err := validateHeroProficiency(params.HeroProficiency); err != nil {
		return nil, err
	}
	if err := validateRoles(params.PreferredRoles); err != nil {
		return nil, err
	}
	if err := validateRegionLatency(params.RegionLatency); err != nil {
		return nil, err
	}

	return &Player{
		id:               params.ID,
		name:             params.Name,
		rating:           params.InitialRating,
		ratingDeviation:  DefaultRatingDeviation,
		ratingVolatility: DefaultRatingVolatility,
		preferredRoles:   cloneRoles(params.PreferredRoles),
		homeRegion:       params.HomeRegion,
		regionLatency:    cloneLatency(params.RegionLatency),
		behaviorScore:    params.BehaviorScore,
		heroProficiency:  cloneProficiency(params.HeroProficiency),
		createdAt:        params.CreatedAt.UTC(),
	}, nil
}

func RestorePlayer(snapshot PlayerSnapshot) (*Player, error) {
	if snapshot.RatingDeviation <= 0 {
		return nil, invalidPlayer("rating deviation must be greater than zero")
	}
	if snapshot.RatingVolatility == 0 {
		snapshot.RatingVolatility = DefaultRatingVolatility
	}
	if snapshot.RatingVolatility < 0 || math.IsNaN(snapshot.RatingVolatility) || math.IsInf(snapshot.RatingVolatility, 0) {
		return nil, invalidPlayer("rating volatility must be finite and greater than zero")
	}
	snapshot.BanReason = strings.TrimSpace(snapshot.BanReason)
	snapshot.BannedBy = strings.TrimSpace(snapshot.BannedBy)
	if snapshot.Banned {
		if snapshot.BanReason == "" || snapshot.BannedBy == "" || snapshot.BannedAt.IsZero() {
			return nil, invalidPlayer("banned player must include reason, operator, and timestamp")
		}
	} else if snapshot.BanReason != "" || snapshot.BannedBy != "" || !snapshot.BannedAt.IsZero() {
		return nil, invalidPlayer("active player cannot contain ban metadata")
	}
	player, err := NewPlayer(NewPlayerParams{
		ID:              snapshot.ID,
		Name:            snapshot.Name,
		InitialRating:   snapshot.Rating,
		PreferredRoles:  snapshot.PreferredRoles,
		HomeRegion:      snapshot.HomeRegion,
		RegionLatency:   snapshot.RegionLatency,
		BehaviorScore:   snapshot.BehaviorScore,
		HeroProficiency: snapshot.HeroProficiency,
		CreatedAt:       snapshot.CreatedAt,
	})
	if err != nil {
		return nil, err
	}
	player.ratingDeviation = snapshot.RatingDeviation
	player.ratingVolatility = snapshot.RatingVolatility
	player.banned = snapshot.Banned
	player.banReason = snapshot.BanReason
	player.bannedAt = snapshot.BannedAt.UTC()
	player.bannedBy = snapshot.BannedBy
	return player, nil
}

func (p *Player) ID() string                          { return p.id }
func (p *Player) Name() string                        { return p.name }
func (p *Player) Rating() float64                     { return p.rating }
func (p *Player) RatingDeviation() float64            { return p.ratingDeviation }
func (p *Player) RatingVolatility() float64           { return p.ratingVolatility }
func (p *Player) Banned() bool                        { return p.banned }
func (p *Player) BanReason() string                   { return p.banReason }
func (p *Player) BannedAt() time.Time                 { return p.bannedAt }
func (p *Player) BannedBy() string                    { return p.bannedBy }
func (p *Player) HomeRegion() string                  { return p.homeRegion }
func (p *Player) BehaviorScore() float64              { return p.behaviorScore }
func (p *Player) HeroProficiency() map[string]float64 { return cloneProficiency(p.heroProficiency) }
func (p *Player) CreatedAt() time.Time                { return p.createdAt }
func (p *Player) PreferredRoles() []Role              { return cloneRoles(p.preferredRoles) }
func (p *Player) RegionLatency() map[string]int       { return cloneLatency(p.regionLatency) }

func (p *Player) WithBanState(banned bool, reason, operatorID string, changedAt time.Time) (*Player, error) {
	reason = strings.TrimSpace(reason)
	operatorID = strings.TrimSpace(operatorID)
	if operatorID == "" || changedAt.IsZero() {
		return nil, invalidPlayer("ban state change requires operator and timestamp")
	}
	if banned && reason == "" {
		return nil, invalidPlayer("ban reason is required")
	}
	clone := p.Clone()
	clone.banned = banned
	if banned {
		clone.banReason = reason
		clone.bannedAt = changedAt.UTC()
		clone.bannedBy = operatorID
	} else {
		clone.banReason = ""
		clone.bannedAt = time.Time{}
		clone.bannedBy = ""
	}
	return clone, nil
}

func (p *Player) Clone() *Player {
	if p == nil {
		return nil
	}
	clone := *p
	clone.preferredRoles = cloneRoles(p.preferredRoles)
	clone.regionLatency = cloneLatency(p.regionLatency)
	clone.heroProficiency = cloneProficiency(p.heroProficiency)
	return &clone
}

// WithRegionLatency returns a validated copy so repositories can update
// network measurements without exposing mutable Player internals.
func (p *Player) WithRegionLatency(latencies map[string]int) (*Player, error) {
	latencies = normalizeRegionLatency(latencies)
	if err := validateRegionLatency(latencies); err != nil {
		return nil, err
	}
	updated := p.Clone()
	updated.regionLatency = cloneLatency(latencies)
	return updated, nil
}

func validateRoles(roles []Role) error {
	if len(roles) == 0 {
		return invalidPlayer("at least one preferred role is required")
	}
	if len(roles) > 5 {
		return invalidPlayer("no more than five preferred roles are allowed")
	}

	seen := make(map[Role]struct{}, len(roles))
	for _, role := range roles {
		if !role.Valid() {
			return invalidPlayer(fmt.Sprintf("unsupported role %q", role))
		}
		if _, exists := seen[role]; exists {
			return invalidPlayer(fmt.Sprintf("duplicate role %q", role))
		}
		seen[role] = struct{}{}
	}
	return nil
}

func validateRegionLatency(latencies map[string]int) error {
	if len(latencies) == 0 {
		return invalidPlayer("at least one region latency is required")
	}
	for region, latency := range latencies {
		if strings.TrimSpace(region) == "" {
			return invalidPlayer("latency region is required")
		}
		if latency < minimumLatencyMS || latency > maximumLatencyMS {
			return invalidPlayer(fmt.Sprintf("latency for region %q must be between 0 and 1000 ms", region))
		}
	}
	return nil
}

func validateHeroProficiency(values map[string]float64) error {
	if len(values) > 100 {
		return invalidPlayer("hero proficiency cannot contain more than 100 heroes")
	}
	seen := make(map[string]struct{}, len(values))
	for heroID, score := range values {
		normalizedID := strings.ToLower(strings.TrimSpace(heroID))
		if _, duplicate := seen[normalizedID]; duplicate {
			return invalidPlayer("hero proficiency contains duplicate normalized hero ids")
		}
		seen[normalizedID] = struct{}{}
		if _, exists := hero.Get(heroID); !exists || score < 0 || score > 100 || math.IsNaN(score) || math.IsInf(score, 0) {
			return invalidPlayer("hero proficiency must reference known heroes with scores between 0 and 100")
		}
	}
	return nil
}

func invalidPlayer(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidPlayer, message)
}

func cloneRoles(roles []Role) []Role {
	return append([]Role(nil), roles...)
}

func cloneLatency(latencies map[string]int) map[string]int {
	clone := make(map[string]int, len(latencies))
	for region, latency := range latencies {
		clone[region] = latency
	}
	return clone
}

func cloneProficiency(values map[string]float64) map[string]float64 {
	clone := make(map[string]float64, len(values))
	for heroID, score := range values {
		clone[strings.ToLower(strings.TrimSpace(heroID))] = score
	}
	return clone
}

func normalizeRegionLatency(latencies map[string]int) map[string]int {
	normalized := make(map[string]int, len(latencies))
	for region, latency := range latencies {
		region = strings.ToLower(strings.TrimSpace(region))
		if region == "" {
			normalized[region] = latency
			continue
		}
		if existing, duplicate := normalized[region]; duplicate && latency > existing {
			continue
		}
		normalized[region] = latency
	}
	return normalized
}
