package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/matchmaking/domain"
)

func TestGenerateCandidatesUsesOldestAnchorAndDynamicWindow(t *testing.T) {
	now := time.Date(2026, time.August, 1, 12, 10, 0, 0, time.UTC)
	policy := domain.DefaultPolicy()
	policy.RatingExpansionPerSecond = 1
	policy.MaxRatingRange = 300

	tickets := []*domain.MatchTicket{
		engineTicket(t, "newer", "player-2", "", 1680, now.Add(-time.Minute)),
		engineTicket(t, "anchor", "player-1", "", 1500, now.Add(-2*time.Minute)),
		engineTicket(t, "outside", "player-3", "", 1800, now.Add(-30*time.Second)),
	}
	result, err := GenerateCandidates(tickets, now, policy)
	if err != nil {
		t.Fatal(err)
	}
	if result.Anchor.ID() != "anchor" {
		t.Fatalf("anchor = %s, want anchor", result.Anchor.ID())
	}
	if result.RatingRange != 220 {
		t.Fatalf("rating range = %v, want 220", result.RatingRange)
	}
	if len(result.Tickets) != 2 {
		t.Fatalf("accepted tickets = %d, want 2", len(result.Tickets))
	}
	if reasonFor(result.Decisions, "outside") != "rating_outside_window" {
		t.Fatalf("outside reason = %q", reasonFor(result.Decisions, "outside"))
	}
}

func TestGenerateCandidatesDoesNotSplitParty(t *testing.T) {
	now := time.Now()
	policy := domain.DefaultPolicy()
	tickets := []*domain.MatchTicket{
		engineTicket(t, "anchor", "player-1", "", 1500, now.Add(-time.Minute)),
		engineTicket(t, "party-1", "player-2", "party-x", 1500, now),
		engineTicket(t, "party-2", "player-3", "party-x", 1900, now),
	}
	result, err := GenerateCandidates(tickets, now, policy)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tickets) != 1 {
		t.Fatalf("accepted tickets = %d, want anchor only", len(result.Tickets))
	}
	for _, id := range []string{"party-1", "party-2"} {
		if reasonFor(result.Decisions, id) != "rating_outside_window" {
			t.Fatalf("%s reason = %q", id, reasonFor(result.Decisions, id))
		}
	}
}

func reasonFor(decisions []CandidateDecision, ticketID string) string {
	for _, decision := range decisions {
		if decision.TicketID == ticketID {
			return decision.Reason
		}
	}
	return ""
}

func engineTicket(t *testing.T, ticketID, playerID, partyID string, rating float64, createdAt time.Time) *domain.MatchTicket {
	t.Helper()
	ticket, err := domain.NewTicket(domain.NewTicketParams{
		ID:             ticketID,
		PlayerID:       playerID,
		PartyID:        partyID,
		Mode:           "ranked_5v5",
		ClientVersion:  "1.0.0",
		Region:         "hongkong",
		Rating:         rating,
		PreferredRoles: []domain.Role{domain.RoleCore, domain.RoleSupport},
		RegionLatency:  map[string]int{"hongkong": 30},
		CreatedAt:      createdAt,
	})
	if err != nil {
		t.Fatalf("NewTicket(%s) error = %v", ticketID, err)
	}
	if err := ticket.Queue(createdAt); err != nil {
		t.Fatalf("Queue(%s) error = %v", ticketID, err)
	}
	return ticket
}

func makeEngineTickets(t *testing.T, count int, now time.Time) []*domain.MatchTicket {
	t.Helper()
	tickets := make([]*domain.MatchTicket, 0, count)
	for index := range count {
		tickets = append(tickets, engineTicket(
			t,
			fmt.Sprintf("ticket-%02d", index),
			fmt.Sprintf("player-%02d", index),
			"",
			1500+float64(index%2)*20,
			now.Add(time.Duration(index)*time.Second),
		))
	}
	return tickets
}
