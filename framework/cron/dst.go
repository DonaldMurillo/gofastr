package cron

import "time"

// FiresAt reports whether the schedule fires at the minute holding t, for a
// walk that evaluated prev just before it. It is Matches with vixie cron's
// daylight-saving handling, in t's location:
//
//   - A fixed-time schedule (see FixedTime) whose time falls in a repeated
//     hour (a fall-back) runs in the first pass only.
//   - A fixed-time schedule whose time a spring-forward skipped runs once,
//     at the first minute after the jump. prev is what tells FiresAt a jump
//     happened: the in-process Scheduler passes the minute before t.
//   - Every other schedule fires by the wall clock as it runs, so a
//     wildcard minute or hour keeps real elapsed time across a change.
//
// battery/queue's DurableScheduler and in-memory Scheduler, and this
// package's Scheduler, all decide a firing here.
func (s Schedule) FiresAt(prev, t time.Time) bool {
	if !s.FixedTime() {
		return s.Matches(t)
	}
	if !s.Matches(t) && !s.skippedMatchInGap(prev, t) {
		return false
	}
	return !fallBackRepeat(t)
}

// fallBackRepeat reports whether t lies in the second pass of a repeated
// wall-clock interval: the zone period holding t began less than the
// fall-back's size ago, with an offset smaller than the one before it.
// The size is read from the zone, so a 30-minute shift (Lord Howe) works
// like an hour.
func fallBackRepeat(t time.Time) bool {
	start, _ := t.ZoneBounds()
	if start.IsZero() {
		return false // zone in effect since the beginning of time
	}
	_, prevOff := start.Add(-time.Nanosecond).Zone()
	_, off := t.Zone()
	back := time.Duration(prevOff-off) * time.Second
	return back > 0 && t.Before(start.Add(back))
}

// skippedMatchInGap reports whether a spring-forward between a and b
// skipped a wall-clock minute the schedule matches. The skipped minutes are
// walked in a fixed zone with a's offset, the wall clock that never came.
func (s Schedule) skippedMatchInGap(a, b time.Time) bool {
	loc := b.Location()
	aWall := a.In(loc)
	bWall := b
	_, aOff := aWall.Zone()
	_, bOff := bWall.Zone()
	if bOff <= aOff {
		return false // not a spring-forward (offset didn't increase)
	}
	fixed := time.FixedZone("dst-gap", aOff)
	start := time.Date(aWall.Year(), aWall.Month(), aWall.Day(),
		aWall.Hour(), aWall.Minute(), 0, 0, fixed).Add(time.Minute)
	end := time.Date(bWall.Year(), bWall.Month(), bWall.Day(),
		bWall.Hour(), bWall.Minute(), 0, 0, fixed)
	// Bounded: real gaps are at most two hours.
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
