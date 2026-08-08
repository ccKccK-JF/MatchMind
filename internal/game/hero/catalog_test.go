package hero

import "testing"

func TestCatalogCoversEveryRoleAndSelectsHighestProficiency(t *testing.T) {
	for _, role := range []string{"vanguard", "roamer", "core", "ranged", "support"} {
		selected, score, err := BestForRole(nil, role)
		if err != nil || selected.Role != role || score != 50 {
			t.Fatalf("BestForRole(%s) = %+v/%v/%v", role, selected, score, err)
		}
	}
	selected, score, err := BestForRole(map[string]float64{"starblade": 35, "emberlord": 91}, "core")
	if err != nil || selected.ID != "emberlord" || score != 91 {
		t.Fatalf("proficiency selection = %+v/%v/%v", selected, score, err)
	}
}

func TestCatalogIsImmutableAndRejectsUnknownRole(t *testing.T) {
	entries := All()
	entries[0].ID = "changed"
	if _, exists := Get("ironwall"); !exists {
		t.Fatal("All returned mutable catalog storage")
	}
	if _, _, err := BestForRole(nil, "mage"); err == nil {
		t.Fatal("unknown role was accepted")
	}
}
