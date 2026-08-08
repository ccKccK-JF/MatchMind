package domain

import (
	"errors"
	"testing"
	"time"
)

func TestTicketHappyPath(t *testing.T) {
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	ticket := newTestTicket(t, now)
	if ticket.State() != TicketStateCreated {
		t.Fatalf("initial state = %s, want CREATED", ticket.State())
	}
	if err := ticket.Queue(now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := ticket.Reserve("reservation-1", now.Add(time.Minute), now.Add(2*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := ticket.Assign("reservation-1", "match-1", now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if ticket.State() != TicketStateAssigned || ticket.MatchID() != "match-1" {
		t.Fatalf("final state = %s, want ASSIGNED", ticket.State())
	}
	if err := ticket.Cancel(now.Add(4 * time.Second)); !errors.Is(err, ErrIllegalStateTransition) {
		t.Fatalf("Cancel() error = %v, want ErrIllegalStateTransition", err)
	}
}

func TestTicketCancelIsIdempotent(t *testing.T) {
	now := time.Now()
	ticket := newTestTicket(t, now)
	if err := ticket.Queue(now); err != nil {
		t.Fatal(err)
	}
	if err := ticket.Cancel(now); err != nil {
		t.Fatal(err)
	}
	if err := ticket.Cancel(now.Add(time.Second)); err != nil {
		t.Fatalf("second Cancel() error = %v", err)
	}
}

func TestTicketReservationRelease(t *testing.T) {
	now := time.Now()
	ticket := newTestTicket(t, now)
	_ = ticket.Queue(now)
	_ = ticket.Reserve("reservation-1", now.Add(time.Minute), now)

	if err := ticket.ReleaseReservation("wrong", now); !errors.Is(err, ErrReservationMismatch) {
		t.Fatalf("ReleaseReservation() error = %v, want ErrReservationMismatch", err)
	}
	if err := ticket.ReleaseExpiredReservation(now.Add(30 * time.Second)); !errors.Is(err, ErrReservationNotExpired) {
		t.Fatalf("early ReleaseExpiredReservation() error = %v, want ErrReservationNotExpired", err)
	}
	if err := ticket.ReleaseExpiredReservation(now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if ticket.State() != TicketStateQueued || ticket.ReservationID() != "" {
		t.Fatalf("released ticket state/id = %s/%q", ticket.State(), ticket.ReservationID())
	}
}

func TestTicketRejectsIllegalTransitions(t *testing.T) {
	now := time.Now()
	ticket := newTestTicket(t, now)
	if err := ticket.Reserve("reservation-1", now.Add(time.Minute), now); !errors.Is(err, ErrIllegalStateTransition) {
		t.Fatalf("Reserve() error = %v, want ErrIllegalStateTransition", err)
	}
	if err := ticket.Expire(now); !errors.Is(err, ErrIllegalStateTransition) {
		t.Fatalf("Expire() error = %v, want ErrIllegalStateTransition", err)
	}
}

func TestNewTicketRejectsInvalidInput(t *testing.T) {
	_, err := NewTicket(NewTicketParams{})
	if !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("NewTicket() error = %v, want ErrInvalidTicket", err)
	}
}

func TestTicketValidatesAndNormalizesGameMode(t *testing.T) {
	now := time.Now()
	params := NewTicketParams{
		ID: "ticket-1", PlayerID: "player-1", Mode: " TRAINING_5V5 ", ClientVersion: "1.0.0",
		Region: "hongkong", Rating: 1500, PreferredRoles: []Role{RoleCore},
		RegionLatency: map[string]int{"hongkong": 30}, CreatedAt: now,
	}
	ticket, err := NewTicket(params)
	if err != nil {
		t.Fatal(err)
	}
	if ticket.Mode() != "training_5v5" {
		t.Fatalf("normalized mode = %q", ticket.Mode())
	}
	params.ID = "ticket-2"
	params.Mode = "custom_5v5"
	if _, err := NewTicket(params); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("unsupported mode error = %v", err)
	}
}

func TestTicketCarriesImmutableSimulationFactors(t *testing.T) {
	now := time.Now().UTC()
	proficiency := map[string]float64{"starblade": 93}
	ticket, err := NewTicket(NewTicketParams{
		ID: "ticket-1", PlayerID: "player-1", Mode: "ranked_5v5", ClientVersion: "1.0.0",
		Region: "hongkong", Rating: 1500, PreferredRoles: []Role{RoleCore},
		RegionLatency: map[string]int{"hongkong": 30}, BehaviorScore: 97,
		HeroProficiency: proficiency, CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	proficiency["starblade"] = 0
	copy := ticket.HeroProficiency()
	copy["starblade"] = 1
	if ticket.BehaviorScore() != 97 || ticket.HeroProficiency()["starblade"] != 93 {
		t.Fatalf("ticket simulation factors = %v/%v", ticket.BehaviorScore(), ticket.HeroProficiency())
	}
	if _, err := NewTicket(NewTicketParams{
		ID: "ticket-2", PlayerID: "player-2", Mode: "ranked_5v5", ClientVersion: "1.0.0",
		Region: "hongkong", Rating: 1500, PreferredRoles: []Role{RoleCore},
		RegionLatency: map[string]int{"hongkong": 30}, BehaviorScore: 97,
		HeroProficiency: map[string]float64{"unknown": 90}, CreatedAt: now,
	}); !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("unknown hero error = %v", err)
	}
}

func TestRestoreAssignedTicket(t *testing.T) {
	now := time.Now().UTC()
	ticket, err := RestoreTicket(TicketSnapshot{
		ID: "ticket-1", PlayerID: "player-1", Mode: "ranked_5v5",
		ClientVersion: "1.0.0", Region: "hongkong", Rating: 1500,
		PreferredRoles: []Role{RoleCore}, RegionLatency: map[string]int{"hongkong": 30},
		State: TicketStateAssigned, CreatedAt: now, UpdatedAt: now.Add(time.Second),
		ReservationID: "reservation-1", ReservationExpiresAt: now.Add(time.Minute), MatchID: "match-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ticket.State() != TicketStateAssigned || ticket.ReservationID() != "reservation-1" || ticket.MatchID() != "match-1" {
		t.Fatalf("restored ticket = state %s, reservation %q, match %q", ticket.State(), ticket.ReservationID(), ticket.MatchID())
	}
}

func TestRestoreTicketRejectsInconsistentSnapshot(t *testing.T) {
	now := time.Now().UTC()
	_, err := RestoreTicket(TicketSnapshot{
		ID: "ticket-1", PlayerID: "player-1", Mode: "ranked_5v5",
		ClientVersion: "1.0.0", Region: "hongkong", Rating: 1500,
		PreferredRoles: []Role{RoleCore}, RegionLatency: map[string]int{"hongkong": 30},
		State: TicketStateReserved, CreatedAt: now, UpdatedAt: now,
	})
	if !errors.Is(err, ErrInvalidTicket) {
		t.Fatalf("RestoreTicket() error = %v, want ErrInvalidTicket", err)
	}
}

func newTestTicket(t *testing.T, now time.Time) *MatchTicket {
	t.Helper()
	ticket, err := NewTicket(NewTicketParams{
		ID:             "ticket-1",
		PlayerID:       "player-1",
		Mode:           "ranked_5v5",
		ClientVersion:  "1.0.0",
		Region:         "hongkong",
		Rating:         1500,
		PreferredRoles: []Role{RoleCore, RoleSupport},
		RegionLatency:  map[string]int{"hongkong": 30},
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatalf("NewTicket() error = %v", err)
	}
	return ticket
}
