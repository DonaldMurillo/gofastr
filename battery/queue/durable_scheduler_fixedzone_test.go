package queue

import (
	"context"
	"testing"
	"time"
)

// A schedule registered in a zone tzName cannot persist (a fixed offset)
// takes its first watermark in the zone it is evaluated in, UTC. The
// watermark came from the registration zone, so "0 2 * * *" registered at
// 03:00 -05:00 waited for 07:00 UTC, which UTC evaluation does not treat
// as a firing, and the catch-up moved the first run to the next day.
func TestFixedZoneFirstRunIsNotSkipped(t *testing.T) {
	s := newTZScheduler(t, "fixed")
	base := time.Date(2026, 1, 1, 3, 0, 0, 0, time.FixedZone("UTC-05", -5*3600))
	if err := s.Cron("daily-fixed", "0 2 * * *").Job("job", nil).RegisterAt(base); err != nil {
		t.Fatalf("register: %v", err)
	}
	for _, tick := range []time.Time{
		time.Date(2026, 1, 2, 2, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 7, 0, 0, 0, time.UTC),
	} {
		if err := s.RunOnce(context.Background(), tick); err != nil {
			t.Fatalf("RunOnce at %s: %v", tick, err)
		}
	}
	if n := len(pendingJobs(t, s.queue)); n != 1 {
		t.Fatalf("Jan 2 enqueued %d jobs, want 1 (02:00 UTC)", n)
	}
}
