package migrations

import (
	"strings"
	"testing"
)

func TestPlayerMigrationIsEmbedded(t *testing.T) {
	body, err := Files.ReadFile("001_players.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"CREATE TABLE IF NOT EXISTS players", "CREATE TABLE IF NOT EXISTS rating_changes", "UNIQUE (match_id, sequence)"} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("migration does not contain %q", expected)
		}
	}
}

func TestTicketMigrationIsEmbedded(t *testing.T) {
	body, err := Files.ReadFile("002_tickets.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"CREATE TABLE IF NOT EXISTS tickets", "tickets_one_active_per_player_idx", "ticket_cancel_idempotency"} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("migration does not contain %q", expected)
		}
	}
}

func TestMatchMigrationIsEmbedded(t *testing.T) {
	body, err := Files.ReadFile("003_matches.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"CREATE TABLE IF NOT EXISTS matches", "revision BIGINT", "matches_state_updated_idx"} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("migration does not contain %q", expected)
		}
	}
}

func TestTicketActivityMigrationIsEmbedded(t *testing.T) {
	body, err := Files.ReadFile("004_ticket_activity.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"ADD COLUMN IF NOT EXISTS active", "WHERE active", "tickets_match_active_idx"} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("migration does not contain %q", expected)
		}
	}
}

func TestAgentMigrationIsEmbedded(t *testing.T) {
	body, err := Files.ReadFile("005_agent.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"CREATE TABLE IF NOT EXISTS agent_runs", "CREATE TABLE IF NOT EXISTS policy_proposals", "PENDING_APPROVAL", "ROLLING_BACK", "treatment_basis_points", "assignment_salt"} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("migration does not contain %q", expected)
		}
	}
}

func TestGlickoMigrationIsEmbedded(t *testing.T) {
	body, err := Files.ReadFile("006_glicko2.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"rating_volatility", "deviation_before", "volatility_after", "rating_system", "glicko2"} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("migration does not contain %q", expected)
		}
	}
}

func TestPlayerBanMigrationIsEmbedded(t *testing.T) {
	body, err := Files.ReadFile("007_player_bans.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"banned", "ban_reason", "banned_at", "banned_by", "players_ban_state_check"} {
		if !strings.Contains(string(body), expected) {
			t.Fatalf("migration does not contain %q", expected)
		}
	}
}
