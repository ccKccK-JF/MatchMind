package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

type scanFunc func(...any) error

func (function scanFunc) Scan(dest ...any) error {
	return function(dest...)
}

func TestScanTicketRestoresReservedSnapshot(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	ticket, err := scanTicket(scanFunc(func(dest ...any) error {
		*dest[0].(*string) = "ticket-1"
		*dest[1].(*string) = "player-1"
		*dest[2].(*string) = ""
		*dest[3].(*string) = "ranked_5v5"
		*dest[4].(*string) = "1.0.0"
		*dest[5].(*string) = "hongkong"
		*dest[6].(*float64) = 1500
		*dest[7].(*[]string) = []string{"core"}
		*dest[8].(*[]byte) = []byte(`{"hongkong":30}`)
		*dest[9].(*string) = "RESERVED"
		*dest[10].(*time.Time) = now
		*dest[11].(*time.Time) = now.Add(time.Second)
		*dest[12].(*pgtype.Text) = pgtype.Text{String: "reservation-1", Valid: true}
		*dest[13].(*pgtype.Timestamptz) = pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true}
		*dest[14].(*pgtype.Text) = pgtype.Text{}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if ticket.State() != domain.TicketStateReserved || ticket.ReservationID() != "reservation-1" {
		t.Fatalf("ticket state/reservation = %s/%q", ticket.State(), ticket.ReservationID())
	}
}

func TestTicketOrderingAndIdentityHelpers(t *testing.T) {
	now := time.Now()
	first := restoredQueuedTicket(t, "ticket-1", "player-1", now)
	second := restoredQueuedTicket(t, "ticket-2", "player-2", now)
	ordered, err := orderTickets([]string{"ticket-2", "ticket-1"}, []*domain.MatchTicket{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].ID() != "ticket-2" || !sameTicketIDs([]string{"ticket-1", "ticket-2"}, ordered) {
		t.Fatalf("ordered tickets = %s/%s", ordered[0].ID(), ordered[1].ID())
	}
}

func TestReservationErrorMapsPostgreSQLConcurrencyFailures(t *testing.T) {
	for _, code := range []string{"40001", "40P01"} {
		if !errors.Is(reservationError(&pgconn.PgError{Code: code}), application.ErrReservationConflict) {
			t.Fatalf("code %s did not map to reservation conflict", code)
		}
	}
}

func restoredQueuedTicket(t *testing.T, ticketID, playerID string, now time.Time) *domain.MatchTicket {
	t.Helper()
	ticket, err := domain.RestoreTicket(domain.TicketSnapshot{
		ID: ticketID, PlayerID: playerID, Mode: "ranked_5v5",
		ClientVersion: "1.0.0", Region: "hongkong", Rating: 1500,
		PreferredRoles: []domain.Role{domain.RoleCore},
		RegionLatency:  map[string]int{"hongkong": 30},
		State:          domain.TicketStateQueued, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}
