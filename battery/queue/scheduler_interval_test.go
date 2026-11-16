package queue

import (
	"testing"
	"testing/synctest"
	"time"
)

// Start polled at the smallest interval (at most a minute) and set the next
// run from the poll time, so Every(90s) fired every 2m, Every(61s) every 2m,
// and Every(1m) beside Every(45s) every 90s.
func TestSchedulerFiresEveryIntervalOnTime(t *testing.T) {
	for _, tc := range []struct {
		name      string
		intervals []time.Duration
		span      time.Duration
	}{
		{"90s alone", []time.Duration{90 * time.Second}, 30 * time.Minute},
		{"61s alone", []time.Duration{61 * time.Second}, 61 * time.Minute},
		{"60s beside 45s", []time.Duration{time.Minute, 45 * time.Second}, 30 * time.Minute},
		{"5m alone", []time.Duration{5 * time.Minute}, time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rq := &recordQueue{}
				s := NewScheduler(rq)
				defer s.Close()
				types := []string{"watched", "other"}
				for i, iv := range tc.intervals {
					if err := s.Every(iv).Job(types[i], nil).Register(); err != nil {
						t.Fatal(err)
					}
				}
				go s.Start(t.Context())
				time.Sleep(tc.span + time.Second)
				synctest.Wait()
				got := 0
				rq.mu.Lock()
				for _, j := range rq.jobs {
					if j.Type == "watched" {
						got++
					}
				}
				rq.mu.Unlock()
				if want := int(tc.span / tc.intervals[0]); got != want {
					t.Fatalf("Every(%s) over %s fired %d times, want %d", tc.intervals[0], tc.span, got, want)
				}
			})
		})
	}
}

// A process that slept through several runs fires once and keeps its phase.
func TestNextIntervalRunSkipsMissedRuns(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		late time.Duration
		want time.Duration
	}{
		{0, 90 * time.Second},
		{time.Second, 90 * time.Second},
		{90 * time.Second, 180 * time.Second},
		{200 * time.Second, 270 * time.Second},
	} {
		if got := nextIntervalRun(base, 90*time.Second, base.Add(tc.late)).Sub(base); got != tc.want {
			t.Errorf("late %s: next run at +%s, want +%s", tc.late, got, tc.want)
		}
	}
	now := base.Add(time.Hour)
	if got := nextIntervalRun(base, 0, now); !got.Equal(now) {
		t.Errorf("zero interval: next run %s, want now", got)
	}
}
