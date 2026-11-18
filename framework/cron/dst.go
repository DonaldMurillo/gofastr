package cron

import "time"

// FiresAt is Matches with vixie cron's daylight-saving rules in t's location,
// for a walk that tested prev just before t: a fixed-time schedule fires only
// in the first pass of a repeated hour, and once at the first minute after a
// spring-forward that skipped its time; any other schedule follows the wall
// clock.
func (s Schedule) FiresAt(prev, t time.Time) bool {
	if !s.FixedTime() {
		return s.Matches(t)
	}
	if !s.Matches(t) && !s.skippedMatchInGap(prev, t) {
		return false
	}
	return !fallBackRepeat(t)
}

func fallBackRepeat(t time.Time) bool {
	start, _ := t.ZoneBounds()
	if start.IsZero() {
		return false
	}
	_, prevOff := start.Add(-time.Nanosecond).Zone()
	_, off := t.Zone()
	back := time.Duration(prevOff-off) * time.Second
	return back > 0 && t.Before(start.Add(back))
}

func (s Schedule) skippedMatchInGap(a, b time.Time) bool {
	loc := b.Location()
	aWall := a.In(loc)
	bWall := b
	_, aOff := aWall.Zone()
	_, bOff := bWall.Zone()
	if bOff <= aOff {
		return false
	}
	fixed := time.FixedZone("dst-gap", aOff)
	start := time.Date(aWall.Year(), aWall.Month(), aWall.Day(),
		aWall.Hour(), aWall.Minute(), 0, 0, fixed).Add(time.Minute)
	end := time.Date(bWall.Year(), bWall.Month(), bWall.Day(),
		bWall.Hour(), bWall.Minute(), 0, 0, fixed)
	const maxGapMinutes = 120
	for range maxGapMinutes {
		if !start.Before(end) {
			break
		}
		if s.Matches(start) {
			return true
		}
		start = start.Add(time.Minute)
	}
	return false
}
