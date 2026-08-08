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
	bannedAt := createdAt.Add(-time.Hour)
	latencies, _ := json.Marshal(map[string]int{"hongkong": 32})
	proficiency, _ := json.Marshal(map[string]float64{"emberlord": 91})
	player, err := scanPlayer(scanFunc(func(dest ...any) error {
		*dest[0].(*string) = "player-1"
		*dest[1].(*string) = "Nova"
		*dest[2].(*float64) = 1650
		*dest[3].(*float64) = 90
		*dest[4].(*float64) = 0.05
		*dest[5].(*bool) = true
		*dest[6].(*string) = "abusive behavior"
		*dest[7].(**time.Time) = &bannedAt
		*dest[8].(*string) = "admin-1"
		*dest[9].(*[]string) = []string{"core", "support"}
		*dest[10].(*string) = "hongkong"
		*dest[11].(*[]byte) = latencies
		*dest[12].(*float64) = 98
		*dest[13].(*[]byte) = proficiency
		*dest[14].(*time.Time) = createdAt
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if player.ID() != "player-1" || player.Rating() != 1650 || player.RatingDeviation() != 90 || player.RatingVolatility() != 0.05 {
		t.Fatalf("player = %+v", player)
	}
	if !player.Banned() || player.BanReason() != "abusive behavior" || player.BannedBy() != "admin-1" || !player.BannedAt().Equal(bannedAt) {
		t.Fatalf("player ban state = %v/%q/%q/%v", player.Banned(), player.BanReason(), player.BannedBy(), player.BannedAt())
	}
	if player.PreferredRoles()[1] != domain.RoleSupport || player.RegionLatency()["hongkong"] != 32 {
		t.Fatalf("player roles/latency = %v/%v", player.PreferredRoles(), player.RegionLatency())
	}
	if player.HeroProficiency()["emberlord"] != 91 {
		t.Fatalf("player hero proficiency = %v", player.HeroProficiency())
	}
}

func TestScanPlayerRejectsCorruptPersistentData(t *testing.T) {
	_, err := scanPlayer(scanFunc(func(dest ...any) error {
		*dest[0].(*string) = "player-1"
		*dest[1].(*string) = "Nova"
		*dest[2].(*float64) = 1650
		*dest[3].(*float64) = 90
		*dest[4].(*float64) = 0.06
		*dest[5].(*bool) = false
		*dest[6].(*string) = ""
		*dest[8].(*string) = ""
		*dest[9].(*[]string) = []string{"unknown"}
		*dest[10].(*string) = "hongkong"
		*dest[11].(*[]byte) = []byte(`{"hongkong":32}`)
		*dest[12].(*float64) = 98
		*dest[13].(*[]byte) = []byte(`{}`)
		*dest[14].(*time.Time) = time.Now()
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
