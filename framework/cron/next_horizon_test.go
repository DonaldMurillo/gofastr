package cron

import (
	"testing"
	"time"
)

// A spec rarer than five years still has a next firing (S33): leap days
// that are Sundays fire on 2032-02-29, and across the 2100 non-leap
// century the gap is 40 years. Next used to give up after five years and
// return zero, which callers read as "never fires".
func TestNextFindsRareSpec(t *testing.T) {
	s, err := Parse("0 0 29 2 */7")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ after, want time.Time }{
		{time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), time.Date(2032, 2, 29, 0, 0, 0, 0, time.UTC)},
		{time.Date(2088, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2128, 2, 29, 0, 0, 0, 0, time.UTC)},
	} {
		if got := s.Next(c.after); !got.Equal(c.want) {
			t.Errorf("Next(%s) = %s, want %s", c.after, got, c.want)
		}
	}
}

// Satisfiable reads the fields alone: a month and day-of-month pair that
// names no date never fires, and either restricted day field (OR) or any
// real date (AND, every date meets every weekday within 400 years) does.
func TestSatisfiableFromFields(t *testing.T) {
	for spec, want := range map[string]bool{
		"0 0 30 2 *":    false,
		"0 0 31 4,6 *":  false,
		"0 0 31 4,6 1":  true,
		"0 0 29 2 */7":  true,
		"0 0 31 * *":    true,
		"*/5 * * * *":   true,
		"0 0 30 2 *\t ": false,
	} {
		s, err := Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.Satisfiable(); got != want {
			t.Errorf("%q Satisfiable = %v, want %v", spec, got, want)
		}
	}
}

// FixedTime is vixie cron's test: neither the minute nor the hour field
// begins with "*". @hourly is "0 * * * *", so it is not fixed.
func TestFixedTimeFromFields(t *testing.T) {
	for spec, want := range map[string]bool{
		"30 1 * * *":     true,
		"0,30 1-3 * * *": true,
		"*/30 1 * * *":   false,
		"15 * * * *":     false,
		"* * * * *":      false,
		"@hourly":        false,
		"@daily":         true,
	} {
		s, err := Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.FixedTime(); got != want {
			t.Errorf("%q FixedTime = %v, want %v", spec, got, want)
		}
	}
}

// SkipDay never passes a matching minute: a walk with it finds what a
// minute-by-minute walk finds, across DST changes in both directions and
// a zone whose transition falls at midnight.
func TestSkipDayMatchesMinuteWalk(t *testing.T) {
	zones := []string{"America/New_York", "America/Santiago", "America/Havana", "Australia/Lord_Howe", "Asia/Tehran"}
	specs := []string{"30 0 * * *", "0 23 * * 1", "15 0 1 * *", "*/20 0-1 8-14 3 0", "0 0 * * 0", "30 0 * * 0", "30 23 * * 6"}
	for _, z := range zones {
		loc, err := time.LoadLocation(z)
		if err != nil {
			t.Skipf("zone %s: %v", z, err)
		}
		for _, spec := range specs {
			s, err := Parse(spec)
			if err != nil {
				t.Fatal(err)
			}
			from := time.Date(2026, 1, 1, 0, 0, 0, 0, loc)
			slow := from
			fast := from
			for slow.Before(from.AddDate(1, 0, 0)) {
				slow = minuteWalk(s, slow)
				fast = s.Next(fast)
				if !slow.Equal(fast) {
					t.Fatalf("%s %q: Next = %s, minute walk = %s", z, spec, fast, slow)
				}
			}
		}
	}
}

func minuteWalk(s Schedule, after time.Time) time.Time {
	t := after.Truncate(time.Minute).Add(time.Minute)
	for !s.Matches(t) {
		t = t.Add(time.Minute)
	}
	return t
}
