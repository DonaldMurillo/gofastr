package queue

import (
	"fmt"
	"slices"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/cron"
)

// This file implements vixie/cronie-parity DST semantics for location-aware
// cron schedules, as a thin wrapper around framework/cron's location-blind
// bitmask matching. The framework parser/matcher is unchanged: it evaluates
// cron fields against a time.Time's wall-clock fields (.Minute(), .Hour(),
// .Day(), .Month(), .Weekday()) regardless of zone, so the only thing it
// gets wrong on its own is the DST transition itself, a minute-by-minute
// absolute-time walk skips over (spring-forward) or double-counts
// (fall-back) the transition's wall-clock minutes. The two helpers below
// wrap that walk to recover vixie semantics:
//
//   - Fall-back (wall clock repeats): a wall-clock match in the REPEATED
//     hour fires only at its EARLIER absolute instant (the EDT side for US
//     Eastern). Detected statelessly from the zone period an instant falls
//     in (fallBackRepeat), so the rule holds across heartbeat boundaries.
//   - Spring-forward (wall clock skips): a wall-clock match inside the
//     transition's skipped window fires once AT the transition instant (the
//     first absolute minute after the skip). Detected by an absolute-minute
//     step that advances the wall clock by more than one minute; each
//     skipped wall minute is tested against the cron fields.
//
// Both rules apply to fixed-time specs only, those whose minute and hour
// fields name values rather than begin with "*" (cron.Schedule.FixedTime).
// Vixie cron runs every other job by the new wall clock, so "*/5 * * * *"
// fires straight through the repeated hour and not at a transition.
//
// These rules implement what queue.md's "including across DST shifts" claim
// promises. All logic lives here (no framework/cron change) because the
// semantics belong to the durable scheduler's location-aware catch-up, not
// to the location-agnostic in-process scheduler that shares the parser.

// fallBackRepeat reports whether t's wall-clock minute already occurred at
// an earlier absolute instant: t sits in the first d of a zone period whose
// offset is d smaller than the previous period's (the clocks went back by
// d). Only zone data is consulted, so two heartbeats either side of a fire
// agree on every repeat without carrying any memory of what fired. Before
// this, the walk remembered only the LAST fire's wall minute, and a spec
// with two firing minutes in the repeated hour ("0,30 1") fired the repeat
// of the first one on the next heartbeat.
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

// fires reports whether the minute t (rendered in loc, reached by one
// absolute-minute step from prevWall) is a cron firing under vixie DST
// semantics: a direct wall-clock match, or the transition instant of a
// spring-forward gap whose skipped window holds a match, and in either case
// not a fall-back repeat.
func fires(parsed cron.Schedule, loc *time.Location, prevWall, t time.Time) bool {
	// Vixie cron keeps its once-only DST handling for fixed-time jobs. A
	// job with a wildcard minute or hour ("*/5 * * * *", "15 * * * *")
	// runs by the new wall clock: through the repeated hour, and not at the
	// transition for minutes the jump skipped.
	if !parsed.FixedTime() {
		return parsed.Matches(t)
	}
	if !parsed.Matches(t) && !skippedMatchInGap(parsed, loc, prevWall, t) {
		return false
	}
	return !fallBackRepeat(t)
}

// nextFire returns the earliest cron firing strictly after `after` (evaluated
// in `loc`), with vixie DST semantics. Returns the zero Time for a spec that
// never fires.
//
// The walk is forward in absolute minutes starting one minute after `after`.
// A minute inside a spring-forward gap's skipped window lands on the
// transition rather than on the next ordinary match (which could be a day
// later for a daily spec).
func nextFire(parsed cron.Schedule, loc *time.Location, after time.Time) time.Time {
	if !parsed.Satisfiable() {
		return time.Time{}
	}
	t := after.UTC().Truncate(time.Minute).Add(time.Minute).In(loc)
	prevWall := t.Add(-time.Minute).In(loc)
	// A date the day fields refuse holds no direct match and no gap whose
	// skipped minutes match, so the walk jumps through it (SkipDay), which
	// lets the search span a whole calendar cycle: leap days that are
	// Sundays recur up to 40 years apart.
	for end := t.AddDate(cron.HorizonYears, 0, 0); t.Before(end); {
		if fires(parsed, loc, prevWall, t) {
			return t.UTC()
		}
		prevWall = t
		t = parsed.SkipDay(t).In(loc)
	}
	return time.Time{}
}

// firesInRange enumerates cron matches in the closed interval [cursor, now]
// (both truncated to the minute, evaluated in `loc`), with vixie DST
// semantics. At most `limit` ticks are returned; when more are due the NEWEST
// `limit` are kept, matching the original backwards-walk retention so a long
// outage costs a bounded amount of memory and retains the newest audit rows.
//
// The walk is forward in absolute minutes, one tick per minute at most. A
// spring-forward transition whose skipped window holds several matching
// minutes still emits one tick, matching vixie cron, which fires
// skipped-window jobs once at the transition rather than once per matching
// skipped minute.
func firesInRange(parsed cron.Schedule, loc *time.Location, cursor, now time.Time, limit int) ([]time.Time, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("queue: cron catch-up limit must be positive, got %d", limit)
	}
	start := cursor.UTC().Truncate(time.Minute).In(loc)
	end := now.UTC().Truncate(time.Minute).In(loc)
	if end.Before(start) {
		return nil, nil
	}
	// The search bound counts back from now, not forward from the cursor:
	// the caller moves the watermark past now, so after downtime longer
	// than the bound a forward window kept the oldest ticks and skipped
	// every newer one, the tick now included.
	if floor := end.Add(-time.Duration(maxCronCatchUpSearchMinutes-1) * time.Minute); start.Before(floor) {
		start = floor
	}
	// Retain the newest `limit` ticks (matches the original backwards-walk
	// memory-bounded retention so audit history keeps the most recent rows).
	// The walk keeps them in a ring of `limit` slots, so an every-minute
	// spec over the whole search bound never holds every missed minute.
	var ring []time.Time
	seen := 0
	t := start
	prevWall := t.Add(-time.Minute).In(loc)
	for scanned := 0; scanned < maxCronCatchUpSearchMinutes && !t.After(end); scanned++ {
		if fires(parsed, loc, prevWall, t) {
			if len(ring) < limit {
				ring = append(ring, t.UTC())
			} else {
				ring[seen%limit] = t.UTC()
			}
			seen++
		}
		prevWall = t
		t = t.Add(time.Minute).In(loc)
	}
	// At seen == limit the ring never wrapped (head is 0), so either
	// branch returns the same ticks.
	if seen <= limit {
		return slices.Clip(ring), nil
	}
	// The ring is full and wrapped: the oldest kept tick sits at the next
	// write position.
	head := seen % limit
	out := make([]time.Time, limit)
	copy(out, ring[head:])
	copy(out[limit-head:], ring[:head])
	return out, nil
}

// skippedMatchInGap reports whether the absolute-minute step from `a` to `b`
// (where b = a.Add(time.Minute)) crossed a spring-forward DST transition
// whose skipped wall-clock window contains a cron match. Both `a` and `b`
// are interpreted in `loc` (the schedule's registration zone).
//
// A spring-forward is detected by the zone offset INCREASING from a to b
// (e.g., US Eastern EST→EDT shifts the offset from -5 to -4). Real-world DST
// is ≤ 1h so the skipped window is at most 60 minutes, but the loop is
// bounded by the wall-clock gap regardless, so a pathological shift would
// still terminate.
//
// Each skipped wall-minute is tested by constructing a synthetic time.Time
// in a FixedZone matching a's offset. The fixed offset means time.Date does
// not scale the wall fields (the way it would if `loc` were used, since Go
// pre-normalizes nonexistent wall times inside `loc`). cron.Matches inspects
// only wall fields (.Minute/.Hour/.Day/.Month/.Weekday), so a synthetic time
// with the desired wall fields matches correctly even though the absolute
// instant it represents never occurs in `loc`.
func skippedMatchInGap(parsed cron.Schedule, loc *time.Location, a, b time.Time) bool {
	aWall := a.In(loc)
	bWall := b.In(loc)
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
	// Safety cap (real DST shifts are ≤ 60 minutes); the bound also keeps
	// this O(1) per transition regardless of the gap size.
	const maxGapMinutes = 120
	for range maxGapMinutes {
		if !start.Before(end) {
			break
		}
		if parsed.Matches(start) {
			return true
		}
		start = start.Add(time.Minute)
	}
	return false
}

// earliestTime returns the earlier of a or b. Used by commitOccurrences to
// pin a cron-fired job's scheduled_at to no later than the process wall
// clock so the job is dequeue-eligible immediately. Both inputs are taken
// as-is (no UTC normalization); callers normalize if needed.
func earliestTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
