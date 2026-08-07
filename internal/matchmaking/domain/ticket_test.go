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
