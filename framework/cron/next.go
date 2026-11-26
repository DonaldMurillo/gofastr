package cron

import "time"

// Schedule is a parsed cron spec, immutable and safe for concurrent use.
type Schedule struct {
	expr cronExpr
}

// Parse parses spec as ParseCron does and returns it as a Schedule.
func Parse(spec string) (Schedule, error) {
	expr, err := ParseCron(spec)
	if err != nil {
		return Schedule{}, err
	}
	return Schedule{expr: expr}, nil
}

// Matches reports whether the schedule fires in the minute holding t.
func (s Schedule) Matches(t time.Time) bool {
	return s.expr.matches(t)
}

// MatchesDay reports whether t's date passes the month and day fields.
func (s Schedule) MatchesDay(t time.Time) bool {
	return s.expr.matchesDay(t)
}

// FixedTime reports whether neither minute nor hour begins with "*".
func (s Schedule) FixedTime() bool {
	return !s.expr.minuteAny && !s.expr.hourAny
}

// Satisfiable reports whether any real date passes the month and day fields.
func (s Schedule) Satisfiable() bool {
	return s.expr.satisfiable()
}

// HorizonYears bounds a firing search; the calendar repeats every 400 years.
const HorizonYears = 401

// Next returns the first firing minute strictly after after, in after's
// location, or the zero Time for a schedule that never fires.
func (s Schedule) Next(after time.Time) time.Time {
	if !s.Satisfiable() {
		return time.Time{}
	}
	t := nextLocalMinute(after)
	end := t.AddDate(HorizonYears, 0, 0)
	for t.Before(end) {
		if s.expr.matches(t) {
			return t
		}
		t = s.SkipDay(t)
	}
	return time.Time{}
}

// SkipDay returns the next minute a firing search from t must test: 21:00 of
// t's date when that date cannot fire and t is earlier, else the next minute.
func (s Schedule) SkipDay(t time.Time) time.Time {
	if !s.MatchesDay(t) {
		y, m, d := t.Date()
		if j := time.Date(y, m, d, 21, 0, 0, 0, t.Location()); j.After(t) {
			return j
		}
	}
	return nextLocalMinute(t)
}

// nextLocalMinute finds the next wall-clock minute, including when a
// zone offset contains seconds or changes before the next boundary.
func nextLocalMinute(after time.Time) time.Time {
	for {
		elapsed := time.Duration(after.Second())*time.Second + time.Duration(after.Nanosecond())
		next := after.Add(time.Minute - elapsed)
		_, zoneEnd := after.ZoneBounds()
		if zoneEnd.IsZero() || next.Before(zoneEnd) {
			return next
		}
		after = zoneEnd.In(after.Location())
		if after.Second() == 0 {
			return after
		}
	}
}
