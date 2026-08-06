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
