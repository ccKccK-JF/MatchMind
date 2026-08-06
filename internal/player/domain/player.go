package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	DefaultRatingDeviation = 350.0
	minimumLatencyMS       = 0
	maximumLatencyMS       = 1000
	minimumBehaviorScore   = 0.0
	maximumBehaviorScore   = 100.0
)

var ErrInvalidPlayer = errors.New("invalid player")

type NewPlayerParams struct {
	ID             string
	Name           string
	InitialRating  float64
	PreferredRoles []Role
	HomeRegion     string
	RegionLatency  map[string]int
	BehaviorScore  float64
	CreatedAt      time.Time
}

// PlayerSnapshot is the durable representation accepted when restoring a
// player from persistence. RestorePlayer applies the same validation as a new
// player instead of allowing repositories to mutate private fields.
type PlayerSnapshot struct {
	ID              string
	Name            string
	Rating          float64
	RatingDeviation float64
	PreferredRoles  []Role
	HomeRegion      string
	RegionLatency   map[string]int
	BehaviorScore   float64
	CreatedAt       time.Time
}

// Player is an immutable player snapshot. Slices and maps are copied at the
// boundary so callers cannot mutate shared repository state accidentally.
type Player struct {
	id              string
	name            string
	rating          float64
	ratingDeviation float64
	preferredRoles  []Role
	homeRegion      string
	regionLatency   map[string]int
	behaviorScore   float64
	createdAt       time.Time
}

func NewPlayer(params NewPlayerParams) (*Player, error) {
	params.ID = strings.TrimSpace(params.ID)
	params.Name = strings.TrimSpace(params.Name)
	params.HomeRegion = strings.ToLower(strings.TrimSpace(params.HomeRegion))

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
	if params.BehaviorScore < minimumBehaviorScore || params.BehaviorScore > maximumBehaviorScore {
		return nil, invalidPlayer("behavior score must be between 0 and 100")
	}
	if err := validateRoles(params.PreferredRoles); err != nil {
		return nil, err
	}
	if err := validateRegionLatency(params.RegionLatency); err != nil {
		return nil, err
	}

	return &Player{
		id:              params.ID,
		name:            params.Name,
		rating:          params.InitialRating,
		ratingDeviation: DefaultRatingDeviation,
		preferredRoles:  cloneRoles(params.PreferredRoles),
		homeRegion:      params.HomeRegion,
		regionLatency:   cloneLatency(params.RegionLatency),
		behaviorScore:   params.BehaviorScore,
		createdAt:       params.CreatedAt.UTC(),
	}, nil
}

func RestorePlayer(snapshot PlayerSnapshot) (*Player, error) {
	if snapshot.RatingDeviation <= 0 {
		return nil, invalidPlayer("rating deviation must be greater than zero")
	}
	player, err := NewPlayer(NewPlayerParams{
		ID:             snapshot.ID,
		Name:           snapshot.Name,
		InitialRating:  snapshot.Rating,
		PreferredRoles: snapshot.PreferredRoles,
		HomeRegion:     snapshot.HomeRegion,
		RegionLatency:  snapshot.RegionLatency,
		BehaviorScore:  snapshot.BehaviorScore,
		CreatedAt:      snapshot.CreatedAt,
	})
	if err != nil {
		return nil, err
	}
	player.ratingDeviation = snapshot.RatingDeviation
	return player, nil
}

func (p *Player) ID() string                    { return p.id }
func (p *Player) Name() string                  { return p.name }
func (p *Player) Rating() float64               { return p.rating }
func (p *Player) RatingDeviation() float64      { return p.ratingDeviation }
func (p *Player) HomeRegion() string            { return p.homeRegion }
func (p *Player) BehaviorScore() float64        { return p.behaviorScore }
func (p *Player) CreatedAt() time.Time          { return p.createdAt }
func (p *Player) PreferredRoles() []Role        { return cloneRoles(p.preferredRoles) }
func (p *Player) RegionLatency() map[string]int { return cloneLatency(p.regionLatency) }

func (p *Player) Clone() *Player {
	if p == nil {
		return nil
	}
	clone := *p
	clone.preferredRoles = cloneRoles(p.preferredRoles)
	clone.regionLatency = cloneLatency(p.regionLatency)
	return &clone
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
