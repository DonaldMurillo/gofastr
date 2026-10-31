package entityui

import (
	"slices"
	"testing"
)

// Selected ids are keys: " x " and "x" are different rows, so dedupe
// is exact and only the empty id is dropped (2026-10-27 U9).
func TestDedupeIDsKeepsPaddedKeys(t *testing.T) {
	got := dedupeIDs([]string{" x ", "x", "", "x", " x "})
	if want := []string{" x ", "x"}; !slices.Equal(got, want) {
		t.Fatalf("dedupeIDs = %q, want %q", got, want)
	}
}
