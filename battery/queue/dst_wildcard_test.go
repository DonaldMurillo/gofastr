package queue

import (
	"slices"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/cron"
)

// A job with a wildcard minute or hour runs by the new wall clock across
// a DST change, as vixie cron runs it (B43): "*/30 * * * *" and
// "*/30 1 * * *" in New York fire through the repeated 01:00 hour on
// 2026-11-01, and "15 * * * *" does not fire at the spring-forward
// transition for the 02:15 the jump skipped on 2026-03-08. Every wildcard
// spec skipped the repeated hour.
func TestWildcardCronRunsThroughDST(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("zone: %v", err)
	}
	utc := func(mo time.Month, d, h, m int) time.Time { return time.Date(2026, mo, d, h, m, 0, 0, time.UTC) }
	for _, c := range []struct {
		spec     string
		from, to time.Time
		want     []time.Time
	}{
		{"*/30 * * * *", utc(11, 1, 4, 0), utc(11, 1, 7, 30), []time.Time{
			utc(11, 1, 4, 0), utc(11, 1, 4, 30), utc(11, 1, 5, 0), utc(11, 1, 5, 30),
			utc(11, 1, 6, 0), utc(11, 1, 6, 30), utc(11, 1, 7, 0), utc(11, 1, 7, 30),
		}},
		{"*/30 1 * * *", utc(11, 1, 4, 0), utc(11, 1, 7, 30), []time.Time{
			utc(11, 1, 5, 0), utc(11, 1, 5, 30), utc(11, 1, 6, 0), utc(11, 1, 6, 30),
		}},
		{"15 * * * *", utc(3, 8, 6, 0), utc(3, 8, 8, 0), []time.Time{
			utc(3, 8, 6, 15), utc(3, 8, 7, 15),
		}},
	} {
		parsed, err := cron.Parse(c.spec)
		if err != nil {
			t.Fatal(err)
		}
		got, err := firesInRange(parsed, loc, c.from, c.to, 100)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.EqualFunc(got, c.want, time.Time.Equal) {
			t.Errorf("%q fires %v, want %v", c.spec, got, c.want)
		}
	}
}

// A spec whose next firing is more than five years out registers and
// lands on it (S33): leap days that are Sundays next fall on 2032-02-29.
// Registration refused it as having no firing.
func TestDurableCronRareSpecRegisters(t *testing.T) {
	s := dstScheduler(t, "rare")
	base := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	if err := s.Cron("leap-sunday", "0 0 29 2 */7").Job("job", nil).RegisterAt(base); err != nil {
		t.Fatalf("register: %v", err)
	}
	var next time.Time
	if err := s.queue.db.QueryRow("SELECT next_run FROM " + s.queue.schedulerSchedulesTable()).Scan(&next); err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2032, 2, 29, 0, 0, 0, 0, time.UTC); !next.Equal(want) {
		t.Fatalf("next_run = %s, want %s", next, want)
	}
}
