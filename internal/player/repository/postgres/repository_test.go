package postgres

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ccKccK-JF/MatchMind/internal/player/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

type scanFunc func(...any) error

func (function scanFunc) Scan(dest ...any) error {
	return function(dest...)
}

func TestScanPlayerRestoresSnapshot(t *testing.T) {
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	latencies, _ := json.Marshal(map[string]int{"hongkong": 32})
	player, err := scanPlayer(scanFunc(func(dest ...any) error {
		*dest[0].(*string) = "player-1"
		*dest[1].(*string) = "Nova"
		*dest[2].(*float64) = 1650
		*dest[3].(*float64) = 90
		*dest[4].(*[]string) = []string{"core", "support"}
		*dest[5].(*string) = "hongkong"
		*dest[6].(*[]byte) = latencies
		*dest[7].(*float64) = 98
		*dest[8].(*time.Time) = createdAt
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if player.ID() != "player-1" || player.Rating() != 1650 || player.RatingDeviation() != 90 {
		t.Fatalf("player = %+v", player)
	}
	if player.PreferredRoles()[1] != domain.RoleSupport || player.RegionLatency()["hongkong"] != 32 {
		t.Fatalf("player roles/latency = %v/%v", player.PreferredRoles(), player.RegionLatency())
	}
}

func TestScanPlayerRejectsCorruptPersistentData(t *testing.T) {
	_, err := scanPlayer(scanFunc(func(dest ...any) error {
		*dest[0].(*string) = "player-1"
		*dest[1].(*string) = "Nova"
		*dest[2].(*float64) = 1650
		*dest[3].(*float64) = 90
		*dest[4].(*[]string) = []string{"unknown"}
		*dest[5].(*string) = "hongkong"
		*dest[6].(*[]byte) = []byte(`{"hongkong":32}`)
		*dest[7].(*float64) = 98
		*dest[8].(*time.Time) = time.Now()
		return nil
	}))
	if err == nil {
		t.Fatal("scanPlayer accepted a corrupt role")
	}
}

func TestUniqueViolation(t *testing.T) {
	if !uniqueViolation(&pgconn.PgError{Code: "23505"}) {
		t.Fatal("uniqueViolation did not recognize PostgreSQL 23505")
	}
	if uniqueViolation(errors.New("different error")) {
		t.Fatal("uniqueViolation accepted a different error")
	}
}
