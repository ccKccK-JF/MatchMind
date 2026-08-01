package id

import (
	"regexp"
	"testing"
)

func TestUUID(t *testing.T) {
	first, err := UUID()
	if err != nil {
		t.Fatal(err)
	}
	second, err := UUID()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("UUID() returned duplicate identifier %q", first)
	}
	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(first) {
		t.Fatalf("UUID() = %q, want RFC 4122 version 4 format", first)
	}
}
