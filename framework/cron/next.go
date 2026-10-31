package cron

import "time"

// Schedule is a parsed cron expression with a public surface. It wraps the
// internal bitmask form so callers outside this package (notably the queue
// battery's Scheduler) can compute firing times without re-parsing or
// re-implementing a cron parser.
//
// Schedule is immutable and safe for concurrent use.
type Schedule struct {
	expr cronExpr
}

// Parse parses a cron spec into a Schedule. It accepts the same syntax as
// ParseCron (standard 5-field plus @shortcuts, ranges, lists and steps).
// Unlike ParseCron, which returns the unexported internal form used by the
// in-process Scheduler. Parse returns a value other packages can hold and
// query via Next and Matches.
func Parse(spec string) (Schedule, error) {
	expr, err := ParseCron(spec)
	if err != nil {
		return Schedule{}, err
	}
	return Schedule{expr: expr}, nil
}

// Matches reports whether the schedule fires during the minute containing t.
func (s Schedule) Matches(t time.Time) bool {
	return s.expr.matches(t)
}

// MatchesDay reports whether t's date can hold a firing: its month and
// day fields match, whatever the minute and hour fields say.
func (s Schedule) MatchesDay(t time.Time) bool {
	return s.expr.matchesDay(t)
}

// FixedTime reports whether the schedule names fixed wall-clock times:
// neither its minute nor its hour field begins with "*". Vixie cron
// gives such a job its once-only handling across a DST change and runs
// every other job by the new wall clock.
func (s Schedule) FixedTime() bool {
	return !s.expr.minuteAny && !s.expr.hourAny
}

// Satisfiable reports whether the schedule ever fires. A spec whose
// month and day-of-month fields name no real date (Feb 30) never does.
func (s Schedule) Satisfiable() bool {
	return s.expr.satisfiable()
}

// HorizonYears bounds every search for a firing. The Gregorian calendar
// repeats every 400 years, weekdays included, so a satisfiable spec fires
// within one cycle of any instant.
const HorizonYears = 401

// Next returns the earliest firing time strictly after after. The result is
// minute-aligned (cron resolution is one minute) and carries after's location.
// It returns the zero Time for a spec that never fires (Feb 30).
func (s Schedule) Next(after time.Time) time.Time {
	if !s.Satisfiable() {
		return time.Time{}
	}
	// Start at the next whole minute strictly after `after`: truncate to the
	// minute then add one minute. This makes Next strictly-after even when
	// `after` lands exactly on a firing boundary.
	t := after.Truncate(time.Minute).Add(time.Minute)
	end := t.AddDate(HorizonYears, 0, 0)
	for t.Before(end) {
		if s.expr.matches(t) {
			return t
		}
		t = s.SkipDay(t)
	}
	return time.Time{}
}

// SkipDay returns the next minute a walk from t must test. Within a date
// MatchesDay refuses, it jumps to 21:00 local of the same date: no firing
// can fall before then, and the walk still crosses midnight a minute at a
// time, so a zone transition near midnight is seen as it happens.
func (s Schedule) SkipDay(t time.Time) time.Time {
	if !s.MatchesDay(t) {
		y, m, d := t.Date()
		if j := time.Date(y, m, d, 21, 0, 0, 0, t.Location()); j.After(t) {
			return j
		}
	}
	return t.Add(time.Minute)
}
