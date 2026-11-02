package queue

import (
	"fmt"
	"slices"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/cron"
)

// This file implements the durable scheduler's location-aware walks over a
// cron spec: the next firing after an instant (nextFire) and every firing
// in a catch-up range (firesInRange). Whether a given minute fires is
// cron.Schedule.FiresAt, the vixie/cronie DST handling framework/cron's
// in-process Scheduler and this package's in-memory Scheduler share:
//
//   - Fall-back (wall clock repeats): a fixed-time match in the REPEATED
//     hour fires only at its EARLIER absolute instant (the EDT side for US
//     Eastern), read statelessly from the zone period the instant falls in.
//   - Spring-forward (wall clock skips): a fixed-time match inside the
//     skipped window fires once AT the transition instant (the first
//     absolute minute after the skip).
//
// Both rules apply to fixed-time specs only, those whose minute and hour
// fields name values rather than begin with "*" (cron.Schedule.FixedTime).
// Vixie cron runs every other job by the new wall clock, so "*/5 * * * *"
// fires straight through the repeated hour and not at a transition.

// fires reports whether a walk at t, having evaluated prevWall just before
// it, fires: cron.Schedule.FiresAt's vixie handling (a fixed-time spec runs
// once across a fall-back and once after a spring-forward skipped its
// time), shared with framework/cron's Scheduler. loc is the zone both
// times are already in.
func fires(parsed cron.Schedule, loc *time.Location, prevWall, t time.Time) bool {
	return parsed.FiresAt(prevWall.In(loc), t.In(loc))
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
