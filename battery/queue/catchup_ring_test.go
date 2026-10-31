package queue

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/cron"
)

// A long outage on an every-minute spec keeps only the newest `limit`
// ticks, oldest first, and the result holds no backing array sized to
// every missed minute (2026-10-27 B21).
func TestCatchUpKeepsOnlyLimitTicks(t *testing.T) {
	parsed, err := cron.Parse("* * * * *")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	for _, limit := range []int{1, 3, 5} {
		ticks, err := firesInRange(parsed, time.UTC, now.Add(-10*24*time.Hour), now, limit)
		if err != nil {
			t.Fatal(err)
		}
		if len(ticks) != limit {
			t.Fatalf("limit %d: %d ticks", limit, len(ticks))
		}
		if cap(ticks) > limit {
			t.Fatalf("limit %d: cap %d keeps every missed tick", limit, cap(ticks))
		}
		for i, tk := range ticks {
			want := now.Add(-time.Duration(limit-1-i) * time.Minute)
			if !tk.Equal(want) {
				t.Fatalf("limit %d: tick %d = %v, want %v", limit, i, tk, want)
			}
		}
	}
	// Fewer due ticks than the limit come back whole, in order.
	ticks, err := firesInRange(parsed, time.UTC, now.Add(-2*time.Minute), now, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ticks) != 3 || !ticks[0].Equal(now.Add(-2*time.Minute)) || !ticks[2].Equal(now) {
		t.Fatalf("short range: %v", ticks)
	}
}
