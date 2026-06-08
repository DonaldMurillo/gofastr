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
// Unlike ParseCron — which returns the unexported internal form used by the
// in-process Scheduler — Parse returns a value other packages can hold and
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

// Next returns the earliest firing time strictly after after. The result is
// minute-aligned (cron resolution is one minute) and carries after's location.
//
// The search is bounded: cron fields cover at most the next few years, so a
// scan of a fixed horizon of minutes is guaranteed to find a match for any
// satisfiable expression. If no match exists within the horizon (only possible
// for an unsatisfiable spec such as Feb 30), Next returns the zero Time.
func (s Schedule) Next(after time.Time) time.Time {
	// Start at the next whole minute strictly after `after`: truncate to the
	// minute then add one minute. This makes Next strictly-after even when
	// `after` lands exactly on a firing boundary.
	t := after.Truncate(time.Minute).Add(time.Minute)

	// 5-year horizon in minutes is an ample bound for any satisfiable spec
	// (the rarest, e.g. "Feb 29", recurs within four years).
	const horizon = 5 * 366 * 24 * 60
	for i := 0; i < horizon; i++ {
		if s.expr.matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}
}
