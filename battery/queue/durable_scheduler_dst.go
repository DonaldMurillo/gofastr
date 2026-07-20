package queue

import (
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/cron"
)

// This file implements vixie/cronie-parity DST semantics for location-aware
// cron schedules, as a thin wrapper around framework/cron's location-blind
// bitmask matching. The framework parser/matcher is unchanged: it evaluates
// cron fields against a time.Time's wall-clock fields (.Minute(), .Hour(),
// .Day(), .Month(), .Weekday()) regardless of zone, so the only thing it
// gets wrong on its own is the DST transition itself — a minute-by-minute
// absolute-time walk skips over (spring-forward) or double-counts
// (fall-back) the transition's wall-clock minutes. The two helpers below
// wrap that walk to recover vixie semantics:
//
//   - Fall-back (wall clock repeats): a wall-clock match in the REPEATED
//     hour fires only at its EARLIER absolute instant (the EDT side for US
//     Eastern). Detected via the zone-offset change between two absolute
//     minutes that render the same wall-clock Y-M-D H:M.
//   - Spring-forward (wall clock skips): a wall-clock match inside the
//     transition's skipped window fires once AT the transition instant (the
//     first absolute minute after the skip). Detected by an absolute-minute
//     step that advances the wall clock by more than one minute; each
//     skipped wall minute is tested against the cron fields.
//
// These rules implement what queue.md's "including across DST shifts" claim
// promises. All logic lives here (no framework/cron change) because the
// semantics belong to the durable scheduler's location-aware catch-up, not
// to the location-agnostic in-process scheduler that shares the parser.

// wallMinuteKey is the Y-M-D H:M tuple of a time rendered in its location —
// the identity of a "wall-clock minute" independent of the zone offset it
// was reached through. Used to dedupe fall-back repeats: two absolute
// instants with the same wallMinuteKey but different offsets are the same
// vixie fire, and only the earlier absolute instant is kept.
type wallMinuteKey struct {
	year              int
	month             time.Month
	day, hour, minute int
}

func wallKey(t time.Time) wallMinuteKey {
	return wallMinuteKey{t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute()}
}

// nextFire returns the earliest cron firing strictly after `after` (evaluated
// in `loc`), with vixie DST semantics. prevFire, when non-zero, is the
// previous fire whose wall-minute must not be repeated in a fall-back window
// (used by boundedDueTicks when advancing the watermark after a fire landed
// inside a repeated hour). Returns the zero Time if no match exists within
// the cron search horizon.
//
// The walk is forward in absolute minutes starting one minute after `after`.
// At each step:
//
//  1. If the step crossed a spring-forward gap whose skipped window contains
//     a cron match, the transition instant (the post-step minute) is the
//     next fire.
//  2. Otherwise if the post-step minute's wall clock matches the cron fields
//     AND its wall-minute has not already fired (prevFire fall-back repeat),
//     it is the next fire.
//
// Spring-forward detection runs before the direct match so a fire inside the
// skipped window lands at the transition rather than at the next ordinary
// match (which could be a day later for a daily spec).
func nextFire(parsed cron.Schedule, loc *time.Location, after, prevFire time.Time) time.Time {
	t := after.UTC().Truncate(time.Minute).Add(time.Minute).In(loc)
	var seen map[wallMinuteKey]bool
	if !prevFire.IsZero() {
		seen = map[wallMinuteKey]bool{wallKey(prevFire.In(loc)): true}
	}
	prevWall := t.Add(-time.Minute).In(loc)
	for range maxCronCatchUpSearchMinutes {
		if skippedMatchInGap(parsed, loc, prevWall, t) {
			if seen == nil || !seen[wallKey(t)] {
				return t.UTC()
			}
		}
		if parsed.Matches(t) {
			k := wallKey(t)
			if seen == nil || !seen[k] {
				return t.UTC()
			}
		}
		prevWall = t
		t = t.Add(time.Minute).In(loc)
	}
	return time.Time{}
}

// firesInRange enumerates cron matches in the closed interval [cursor, now]
// (both truncated to the minute, evaluated in `loc`), with vixie DST
// semantics. At most `limit` ticks are returned; when more are due the NEWEST
// `limit` are kept, matching the original backwards-walk retention so a long
// outage costs a bounded amount of memory and retains the newest audit rows.
//
// The walk is forward in absolute minutes. Fall-back dedup uses a per-day
// wall-minute set: a match whose wall-minute was already emitted (at an
// earlier absolute instant) is skipped. The set is reset on every calendar
// date change so it cannot grow unbounded for a minute-resolution spec
// evaluated over a multi-year outage (DST repeats are intra-day, so a
// same-date bound is correct).
//
// Spring-forward: when the absolute-minute step skipped wall minutes, each
// skipped wall minute is tested; if any matches, a single tick is emitted at
// the transition instant (the post-step minute). At most one transition tick
// per transition — matching vixie cron, which fires skipped-window jobs once
// at the transition rather than once per matching skipped minute.
func firesInRange(parsed cron.Schedule, loc *time.Location, cursor, now time.Time, limit int) ([]time.Time, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("queue: cron catch-up limit must be positive, got %d", limit)
	}
	start := cursor.UTC().Truncate(time.Minute).In(loc)
	end := now.UTC().Truncate(time.Minute).In(loc)
	if end.Before(start) {
		return nil, nil
	}
	var all []time.Time
	seen := make(map[wallMinuteKey]bool)
	var seenDate wallMinuteKey // date component only (hour/minute zeroed)
	t := start
	prevWall := t.Add(-time.Minute).In(loc)
	for scanned := 0; scanned < maxCronCatchUpSearchMinutes && !t.After(end); scanned++ {
		// Reset the fall-back dedup set on every calendar date change. DST
		// repeats happen inside a single day; bounding by date keeps the set
		// small even for minute-resolution specs over long outages.
		day := wallMinuteKey{t.Year(), t.Month(), t.Day(), 0, 0}
		if day != seenDate {
			seen = make(map[wallMinuteKey]bool)
			seenDate = day
		}

		// Spring-forward: a gap-crossing step whose skipped window contains a
		// cron match emits exactly one tick at the transition (t).
		if skippedMatchInGap(parsed, loc, prevWall, t) {
			k := wallKey(t)
			if !seen[k] {
				seen[k] = true
				all = append(all, t.UTC())
			}
		}

		// Direct match at t. Fall-back dedup against any earlier absolute
		// instant that already fired at this wall-minute.
		if parsed.Matches(t) {
			k := wallKey(t)
			if !seen[k] {
				seen[k] = true
				all = append(all, t.UTC())
			}
		}

		prevWall = t
		t = t.Add(time.Minute).In(loc)
	}

	// Retain the newest `limit` ticks (matches the original backwards-walk
	// memory-bounded retention so audit history keeps the most recent rows).
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	return all, nil
}

// skippedMatchInGap reports whether the absolute-minute step from `a` to `b`
// (where b = a.Add(time.Minute)) crossed a spring-forward DST transition
// whose skipped wall-clock window contains a cron match. Both `a` and `b`
// are interpreted in `loc` (the schedule's registration zone).
//
// A spring-forward is detected by the zone offset INCREASING from a to b
// (e.g., US Eastern EST→EDT shifts the offset from -5 to -4). Real-world DST
// is ≤ 1h so the skipped window is at most 60 minutes — but the loop is
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
