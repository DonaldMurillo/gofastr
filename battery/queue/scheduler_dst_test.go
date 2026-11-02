package queue

import (
	"context"
	"testing"
	"time"
)

// The in-memory Scheduler computed each cron firing with cron.Schedule.Next,
// a plain wall-clock walk: on a fall-back night "30 1 * * *" found the
// standard-time 01:30 right after the daylight one and enqueued twice, and
// on a spring-forward day "30 2 * * *" found no 02:30 and slept to the next
// day. It now shares the vixie handling the DurableScheduler and
// framework/cron use (cron.Schedule.FiresAt), in the zone of the time it
// was registered at.
func TestInMemorySchedulerCronAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	lh, err := time.LoadLocation("Australia/Lord_Howe")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	cases := []struct {
		name, spec string
		loc        *time.Location
		start, end time.Time // UTC
		want       []time.Time
	}{
		{"NY fall-back", "30 1 * * *", ny,
			time.Date(2026, 11, 1, 4, 0, 0, 0, time.UTC), time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC),
			[]time.Time{time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)}},
		{"NY spring-forward", "30 2 * * *", ny,
			time.Date(2026, 3, 8, 5, 0, 0, 0, time.UTC), time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC),
			[]time.Time{time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)}}, // 03:00 EDT
		{"Lord Howe fall-back", "45 1 * * *", lh,
			time.Date(2026, 4, 4, 13, 0, 0, 0, time.UTC), time.Date(2026, 4, 4, 17, 0, 0, 0, time.UTC),
			[]time.Time{time.Date(2026, 4, 4, 14, 45, 0, 0, time.UTC)}}, // 01:45 +11
		{"Lord Howe spring-forward", "15 2 * * *", lh,
			time.Date(2026, 10, 3, 14, 0, 0, 0, time.UTC), time.Date(2026, 10, 3, 17, 0, 0, 0, time.UTC),
			[]time.Time{time.Date(2026, 10, 3, 15, 30, 0, 0, time.UTC)}}, // 02:30 +11
		{"UTC", "30 1 * * *", time.UTC,
			time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC),
			[]time.Time{time.Date(2026, 11, 1, 1, 30, 0, 0, time.UTC)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rq := &recordQueue{}
			s := NewScheduler(rq)
			t.Cleanup(func() { _ = s.Close() })
			if err := s.Cron(c.spec).Job("j", nil).RegisterAt(c.start.In(c.loc)); err != nil {
				t.Fatal(err)
			}
			var fired []time.Time
			for now := c.start; now.Before(c.end); now = now.Add(time.Minute) {
				before := rq.count()
				s.dispatchDue(context.Background(), now.In(c.loc))
				if rq.count() != before {
					fired = append(fired, now)
				}
			}
			ok := len(fired) == len(c.want)
			for i := 0; ok && i < len(fired); i++ {
				ok = fired[i].Equal(c.want[i])
			}
			if !ok {
				t.Errorf("%s fired at %v, want %v", c.spec, fired, c.want)
			}
		})
	}
}
