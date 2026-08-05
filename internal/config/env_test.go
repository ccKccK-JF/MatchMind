package config

import "testing"

func TestInt(t *testing.T) {
	t.Setenv("MATCHMIND_TEST_INT", "10")
	value, err := Int("MATCHMIND_TEST_INT", 1)
	if err != nil || value != 10 {
		t.Fatalf("Int() = %d, %v", value, err)
	}
}

func TestIntFallbackAndInvalidValue(t *testing.T) {
	t.Setenv("MATCHMIND_TEST_INT", "")
	value, err := Int("MATCHMIND_TEST_INT", 7)
	if err != nil || value != 7 {
		t.Fatalf("Int() fallback = %d, %v", value, err)
	}
	t.Setenv("MATCHMIND_TEST_INT", "not-a-number")
	if _, err := Int("MATCHMIND_TEST_INT", 7); err == nil {
		t.Fatal("Int() accepted an invalid value")
	}
}
