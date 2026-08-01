package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/application"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/repository/memory"
)

type fakePlayerReader struct {
	player application.PlayerSnapshot
	err    error
}

func (f fakePlayerReader) GetPlayer(context.Context, string) (application.PlayerSnapshot, error) {
	return f.player, f.err
}

func TestTicketServiceCreateGetCancelFlow(t *testing.T) {
	now := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	store := memory.NewTicketStore()
	generated := 0
	service := application.NewTicketService(
		store,
		fakePlayerReader{player: testPlayerSnapshot()},
		func() (string, error) {
			generated++
			return "ticket-generated", nil
		},
		func() time.Time { return now },
	)
	command := application.CreateTicketCommand{
		PlayerID:       "player-1",
		Mode:           "ranked_5v5",
		ClientVersion:  "1.0.0",
		PreferredRoles: []domain.Role{domain.RoleCore, domain.RoleSupport},
		RegionLatency:  map[string]int{"singapore": 40, "hongkong": 30},
		IdempotencyKey: "create-1",
	}

	created, err := service.CreateTicket(context.Background(), command)
	if err != nil {
		t.Fatalf("CreateTicket() error = %v", err)
	}
	if created.State() != domain.TicketStateQueued || created.Region() != "hongkong" {
		t.Fatalf("created ticket state/region = %s/%s", created.State(), created.Region())
	}

	idempotent, err := service.CreateTicket(context.Background(), command)
	if err != nil {
		t.Fatalf("idempotent CreateTicket() error = %v", err)
	}
	if idempotent.ID() != created.ID() {
		t.Fatalf("idempotent ticket id = %q, want %q", idempotent.ID(), created.ID())
	}
	if generated != 2 {
		// ID generation happens before the store resolves idempotency; the unused
		// second ID is harmless and storage remains the authority.
		t.Fatalf("id generator calls = %d, want 2", generated)
	}

	_, err = service.CreateTicket(context.Background(), application.CreateTicketCommand{
		PlayerID:       command.PlayerID,
		Mode:           command.Mode,
		ClientVersion:  command.ClientVersion,
		PreferredRoles: command.PreferredRoles,
		RegionLatency:  command.RegionLatency,
		IdempotencyKey: "create-2",
	})
	if !errors.Is(err, application.ErrActiveTicketExists) {
		t.Fatalf("second active CreateTicket() error = %v, want ErrActiveTicketExists", err)
	}

	got, err := service.GetTicket(context.Background(), created.ID())
	if err != nil || got.ID() != created.ID() {
		t.Fatalf("GetTicket() = %v, %v", got, err)
	}
	cancelled, err := service.CancelTicket(context.Background(), created.ID(), command.PlayerID, "cancel-1")
	if err != nil {
		t.Fatalf("CancelTicket() error = %v", err)
	}
	if cancelled.State() != domain.TicketStateCancelled {
		t.Fatalf("cancelled state = %s, want CANCELLED", cancelled.State())
	}
	idempotentCancel, err := service.CancelTicket(context.Background(), created.ID(), command.PlayerID, "cancel-1")
	if err != nil || idempotentCancel.State() != domain.TicketStateCancelled {
		t.Fatalf("idempotent CancelTicket() = %v, %v", idempotentCancel, err)
	}
}

func TestTicketServiceUsesPlayerDefaults(t *testing.T) {
	service := application.NewTicketService(
		memory.NewTicketStore(),
		fakePlayerReader{player: testPlayerSnapshot()},
		func() (string, error) { return "ticket-1", nil },
		time.Now,
	)
	ticket, err := service.CreateTicket(context.Background(), application.CreateTicketCommand{
		PlayerID:       "player-1",
		Mode:           "normal_5v5",
		ClientVersion:  "1.0.0",
		IdempotencyKey: "create-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ticket.PreferredRoles()) != 1 || ticket.Region() != "hongkong" {
		t.Fatalf("ticket defaults roles/region = %v/%s", ticket.PreferredRoles(), ticket.Region())
	}
}

func TestTicketServiceRequiresIdempotencyKey(t *testing.T) {
	service := application.NewTicketService(memory.NewTicketStore(), fakePlayerReader{player: testPlayerSnapshot()}, nil, nil)
	_, err := service.CreateTicket(context.Background(), application.CreateTicketCommand{})
	if !errors.Is(err, application.ErrIdempotencyKeyRequired) {
		t.Fatalf("CreateTicket() error = %v, want ErrIdempotencyKeyRequired", err)
	}
}

func testPlayerSnapshot() application.PlayerSnapshot {
	return application.PlayerSnapshot{
		ID:             "player-1",
		Rating:         1500,
		PreferredRoles: []domain.Role{domain.RoleCore},
		RegionLatency:  map[string]int{"hongkong": 30},
	}
}
