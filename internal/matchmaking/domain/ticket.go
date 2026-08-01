package domain

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
)

var (
	ErrInvalidTicket          = errors.New("invalid ticket")
	ErrIllegalStateTransition = errors.New("illegal ticket state transition")
	ErrReservationMismatch    = errors.New("reservation mismatch")
	ErrReservationNotExpired  = errors.New("reservation has not expired")
)

type TicketState string

const (
	TicketStateCreated   TicketState = "CREATED"
	TicketStateQueued    TicketState = "QUEUED"
	TicketStateReserved  TicketState = "RESERVED"
	TicketStateAssigned  TicketState = "ASSIGNED"
	TicketStateCancelled TicketState = "CANCELLED"
	TicketStateExpired   TicketState = "EXPIRED"
	TicketStateFailed    TicketState = "FAILED"
)

type Role string

const (
	RoleVanguard Role = "vanguard"
	RoleRoamer   Role = "roamer"
	RoleCore     Role = "core"
	RoleRanged   Role = "ranged"
	RoleSupport  Role = "support"
)

type NewTicketParams struct {
	ID             string
	PlayerID       string
	PartyID        string
	Mode           string
	ClientVersion  string
	Region         string
	Rating         float64
	PreferredRoles []Role
	RegionLatency  map[string]int
	CreatedAt      time.Time
}

type MatchTicket struct {
	id                   string
	playerID             string
	partyID              string
	mode                 string
	clientVersion        string
	region               string
	rating               float64
	preferredRoles       []Role
	regionLatency        map[string]int
	state                TicketState
	createdAt            time.Time
	updatedAt            time.Time
	reservationID        string
	reservationExpiresAt time.Time
	matchID              string
}

func NewTicket(params NewTicketParams) (*MatchTicket, error) {
	params.ID = strings.TrimSpace(params.ID)
	params.PlayerID = strings.TrimSpace(params.PlayerID)
	params.PartyID = strings.TrimSpace(params.PartyID)
	params.Mode = strings.TrimSpace(params.Mode)
	params.ClientVersion = strings.TrimSpace(params.ClientVersion)
	params.Region = strings.ToLower(strings.TrimSpace(params.Region))

	switch {
	case params.ID == "":
		return nil, invalidTicket("id is required")
	case params.PlayerID == "":
		return nil, invalidTicket("player id is required")
	case params.Mode == "":
		return nil, invalidTicket("mode is required")
	case params.ClientVersion == "":
		return nil, invalidTicket("client version is required")
	case params.Region == "":
		return nil, invalidTicket("region is required")
	case params.Rating <= 0 || math.IsNaN(params.Rating) || math.IsInf(params.Rating, 0):
		return nil, invalidTicket("rating must be finite and greater than zero")
	case params.CreatedAt.IsZero():
		return nil, invalidTicket("created time is required")
	}
	if err := validateTicketRoles(params.PreferredRoles); err != nil {
		return nil, err
	}
	if err := validateTicketLatency(params.RegionLatency); err != nil {
		return nil, err
	}

	createdAt := params.CreatedAt.UTC()
	return &MatchTicket{
		id:             params.ID,
		playerID:       params.PlayerID,
		partyID:        params.PartyID,
		mode:           params.Mode,
		clientVersion:  params.ClientVersion,
		region:         params.Region,
		rating:         params.Rating,
		preferredRoles: cloneRoles(params.PreferredRoles),
		regionLatency:  cloneLatency(params.RegionLatency),
		state:          TicketStateCreated,
		createdAt:      createdAt,
		updatedAt:      createdAt,
	}, nil
}

func (t *MatchTicket) Queue(now time.Time) error {
	if t.state != TicketStateCreated {
		return transitionError(t.state, TicketStateQueued)
	}
	t.transition(TicketStateQueued, now)
	return nil
}

func (t *MatchTicket) Cancel(now time.Time) error {
	if t.state == TicketStateCancelled {
		return nil
	}
	if t.state != TicketStateCreated && t.state != TicketStateQueued {
		return transitionError(t.state, TicketStateCancelled)
	}
	t.clearReservation()
	t.transition(TicketStateCancelled, now)
	return nil
}

func (t *MatchTicket) Expire(now time.Time) error {
	if t.state != TicketStateQueued {
		return transitionError(t.state, TicketStateExpired)
	}
	t.transition(TicketStateExpired, now)
	return nil
}

func (t *MatchTicket) Fail(now time.Time) error {
	if t.state != TicketStateCreated && t.state != TicketStateQueued && t.state != TicketStateReserved {
		return transitionError(t.state, TicketStateFailed)
	}
	t.clearReservation()
	t.transition(TicketStateFailed, now)
	return nil
}

func (t *MatchTicket) Reserve(reservationID string, expiresAt, now time.Time) error {
	reservationID = strings.TrimSpace(reservationID)
	if t.state != TicketStateQueued {
		return transitionError(t.state, TicketStateReserved)
	}
	if reservationID == "" || expiresAt.IsZero() || !expiresAt.After(now) {
		return ErrInvalidTicket
	}
	t.reservationID = reservationID
	t.reservationExpiresAt = expiresAt.UTC()
	t.transition(TicketStateReserved, now)
	return nil
}

func (t *MatchTicket) Assign(reservationID, matchID string, now time.Time) error {
	if t.state != TicketStateReserved {
		return transitionError(t.state, TicketStateAssigned)
	}
	if reservationID == "" || reservationID != t.reservationID {
		return ErrReservationMismatch
	}
	matchID = strings.TrimSpace(matchID)
	if matchID == "" {
		return ErrInvalidTicket
	}
	t.matchID = matchID
	t.transition(TicketStateAssigned, now)
	return nil
}

func (t *MatchTicket) ReleaseReservation(reservationID string, now time.Time) error {
	if t.state != TicketStateReserved {
		return transitionError(t.state, TicketStateQueued)
	}
	if reservationID == "" || reservationID != t.reservationID {
		return ErrReservationMismatch
	}
	t.clearReservation()
	t.transition(TicketStateQueued, now)
	return nil
}

func (t *MatchTicket) ReleaseExpiredReservation(now time.Time) error {
	if t.state != TicketStateReserved {
		return transitionError(t.state, TicketStateQueued)
	}
	if now.Before(t.reservationExpiresAt) {
		return ErrReservationNotExpired
	}
	t.clearReservation()
	t.transition(TicketStateQueued, now)
	return nil
}

func (t *MatchTicket) IsQueueCandidate() bool {
	return t.state == TicketStateQueued
}

func (t *MatchTicket) IsActive() bool {
	switch t.state {
	case TicketStateCreated, TicketStateQueued, TicketStateReserved, TicketStateAssigned:
		return true
	default:
		return false
	}
}

func (t *MatchTicket) ID() string                      { return t.id }
func (t *MatchTicket) PlayerID() string                { return t.playerID }
func (t *MatchTicket) PartyID() string                 { return t.partyID }
func (t *MatchTicket) Mode() string                    { return t.mode }
func (t *MatchTicket) ClientVersion() string           { return t.clientVersion }
func (t *MatchTicket) Region() string                  { return t.region }
func (t *MatchTicket) Rating() float64                 { return t.rating }
func (t *MatchTicket) State() TicketState              { return t.state }
func (t *MatchTicket) CreatedAt() time.Time            { return t.createdAt }
func (t *MatchTicket) UpdatedAt() time.Time            { return t.updatedAt }
func (t *MatchTicket) ReservationID() string           { return t.reservationID }
func (t *MatchTicket) ReservationExpiresAt() time.Time { return t.reservationExpiresAt }
func (t *MatchTicket) MatchID() string                 { return t.matchID }
func (t *MatchTicket) PreferredRoles() []Role          { return cloneRoles(t.preferredRoles) }
func (t *MatchTicket) RegionLatency() map[string]int   { return cloneLatency(t.regionLatency) }

func (t *MatchTicket) Clone() *MatchTicket {
	if t == nil {
		return nil
	}
	clone := *t
	clone.preferredRoles = cloneRoles(t.preferredRoles)
	clone.regionLatency = cloneLatency(t.regionLatency)
	return &clone
}

func (t *MatchTicket) transition(state TicketState, now time.Time) {
	t.state = state
	t.updatedAt = now.UTC()
}

func (t *MatchTicket) clearReservation() {
	t.reservationID = ""
	t.reservationExpiresAt = time.Time{}
}

func validateTicketRoles(roles []Role) error {
	if len(roles) == 0 || len(roles) > 5 {
		return invalidTicket("between one and five preferred roles are required")
	}
	seen := make(map[Role]struct{}, len(roles))
	for _, role := range roles {
		switch role {
		case RoleVanguard, RoleRoamer, RoleCore, RoleRanged, RoleSupport:
		default:
			return invalidTicket(fmt.Sprintf("unsupported role %q", role))
		}
		if _, exists := seen[role]; exists {
			return invalidTicket(fmt.Sprintf("duplicate role %q", role))
		}
		seen[role] = struct{}{}
	}
	return nil
}

func validateTicketLatency(latencies map[string]int) error {
	if len(latencies) == 0 {
		return invalidTicket("at least one region latency is required")
	}
	for region, latency := range latencies {
		if strings.TrimSpace(region) == "" || latency < 0 || latency > 1000 {
			return invalidTicket("region latency must be between 0 and 1000 ms")
		}
	}
	return nil
}

func invalidTicket(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidTicket, message)
}

func transitionError(from, to TicketState) error {
	return fmt.Errorf("%w: %s -> %s", ErrIllegalStateTransition, from, to)
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
