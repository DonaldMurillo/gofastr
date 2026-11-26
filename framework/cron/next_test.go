package cron

import (
	"testing"
	"time"
)

// Parse exposes the cron schedule for callers outside the scheduler
// (e.g. the queue battery) and Next computes the next firing strictly
// after a given instant.

func TestCron_Next(t *testing.T) {
	for _, c := range []struct {
		name, expr string
		base, want time.Time
	}{
		{"daily", "0 2 * * *", // every day at 02:00
			// 2026-06-08 01:30 -> next fire 02:00 same day.
			time.Date(2026, 6, 8, 1, 30, 0, 0, time.UTC),
			time.Date(2026, 6, 8, 2, 0, 0, 0, time.UTC)},
		{"rolls to tomorrow", "0 2 * * *",
			// 03:00 is past 02:00 today, so next is tomorrow 02:00.
			time.Date(2026, 6, 8, 3, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 9, 2, 0, 0, 0, time.UTC)},
		{"strictly after", "*/15 * * * *", // every 15 minutes
			// Exactly on a fire boundary -> Next returns the *following* one.
			time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC),
			time.Date(2026, 6, 8, 10, 15, 0, 0, time.UTC)},
	} {
		t.Run(c.name, func(t *testing.T) {
			sc, err := Parse(c.expr)
			if err != nil {
				t.Fatalf("Parse(%q): %v", c.expr, err)
			}
			if got := sc.Next(c.base); !got.Equal(c.want) {
				t.Errorf("Next(%v) = %v, want %v", c.base, got, c.want)
			}
		})
	}
}

func TestScheduleNextAlignsWithLocalMinute(t *testing.T) {
	s, err := Parse("0 12 * * *")
	if err != nil {
		t.Fatal(err)
	}
	local := time.FixedZone("offset with seconds", 25)
	after := time.Date(2026, 11, 26, 11, 59, 50, 0, local)
	want := time.Date(2026, 11, 26, 12, 0, 0, 0, local)
	if got := s.Next(after); !got.Equal(want) {
		t.Fatalf("Next(%s) = %s, want %s", after, got, want)
	}
}
