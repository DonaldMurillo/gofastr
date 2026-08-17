package filter

import (
	"strings"
	"testing"
)

// TestSplitINValuesBoundedStopsAtCap pins the allocation bound: an
// over-cap ?field_in= value (here 100_000 comma entries against a 1000
// cap) must cost at most max+1 materialized entries, not the whole list,
// while total still reports the exact count the over-cap error names.
// SplitINValues built the full slice before the caller compared against
// MaxINListEntries, so a hostile comma bomb allocated 500k strings to
// earn a 400.
func TestSplitINValuesBoundedStopsAtCap(t *testing.T) {
	huge := strings.Repeat("v,", 100_000-1) + "v" // exactly 100_000 entries
	parts, total := SplitINValuesBounded([]string{huge}, MaxINListEntries)
	if total != 100_000 {
		t.Fatalf("total = %d, want the exact count 100000", total)
	}
	if len(parts) != MaxINListEntries+1 {
		t.Fatalf("len(parts) = %d, want max+1 = %d — the full list must not be built", len(parts), MaxINListEntries+1)
	}

	// Under the cap the bounded result is exactly SplitINValues' list.
	in := []string{"a,b", "c"}
	parts, total = SplitINValuesBounded(in, MaxINListEntries)
	want, wantTotal := SplitINValues(in), 3
	if total != wantTotal || strings.Join(parts, ",") != strings.Join(want, ",") {
		t.Fatalf("under-cap split = %v (total %d), want %v / %d", parts, total, want, wantTotal)
	}

	// Repeated keys union into the same total, bounded the same way.
	parts, total = SplitINValuesBounded([]string{strings.Repeat("a,", MaxINListEntries-1) + "a", "b,c"}, MaxINListEntries)
	if total != MaxINListEntries+2 || len(parts) != MaxINListEntries+1 {
		t.Fatalf("repeated keys: total = %d len(parts) = %d, want %d / %d",
			total, len(parts), MaxINListEntries+2, MaxINListEntries+1)
	}
}
