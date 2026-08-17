package pagination

import (
	"math"
	"testing"
)

// TestOffsetForPage pins the page→offset arithmetic including the
// overflow guard. A client-supplied page multiplied by hand wraps int
// arithmetic: MaxInt64 with limit 2 yields -4 (negative OFFSET — an
// error on Postgres, silently page 1 on SQLite), and 2^62+2 with limit
// 4 yields +4 (the wrong window served without any error). The guard
// clamps both to the first window; ordinary pages must stay exact.
func TestOffsetForPage(t *testing.T) {
	cases := []struct {
		name        string
		page, limit int
		want        int
	}{
		{"page1", 1, 20, 0},
		{"page2", 2, 20, 20},
		{"page3-limit1", 3, 1, 2},
		{"limit1-huge-exact", math.MaxInt, 1, math.MaxInt - 1}, // ×1 cannot wrap; a huge positive offset just yields an empty window
		{"maxint-limit2", math.MaxInt, 2, 0},                   // wraps to -4 unguarded
		{"wrap-positive", 1<<62 + 2, 4, 0},                     // wraps to +4 unguarded
		{"just-over-boundary", math.MaxInt/100 + 2, 100, 0},    // product exceeds MaxInt
		{"at-boundary-exact", math.MaxInt / 100, 100, (math.MaxInt/100 - 1) * 100},
		{"negative-page", -5, 20, 0},
	}
	for _, c := range cases {
		if got := OffsetForPage(c.page, c.limit); got != c.want {
			t.Errorf("%s: OffsetForPage(%d, %d) = %d, want %d", c.name, c.page, c.limit, got, c.want)
		}
	}
}
