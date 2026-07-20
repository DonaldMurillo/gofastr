package queue

import (
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/cron"
)

const (
	defaultMaxCatchUpOccurrences = 1000
	maxCronCatchUpSearchMinutes  = 5 * 366 * 24 * 60
)

func boundedDueTicks(schedule durableSchedule, now time.Time, limit int) ([]time.Time, time.Time, error) {
	cursor := schedule.nextRun.UTC()
	now = now.UTC()
	if cursor.After(now) {
		return nil, cursor, nil
	}
	if limit <= 0 {
		return nil, time.Time{}, fmt.Errorf("queue: schedule %q has invalid catch-up limit %d", schedule.id, limit)
	}
	if schedule.cronSpec == "" {
		if schedule.interval <= 0 {
			return nil, time.Time{}, fmt.Errorf("queue: schedule %q did not advance", schedule.id)
		}
		elapsed := now.Sub(cursor)
		total := int64(elapsed/schedule.interval) + 1
		kept := total
		if kept > int64(limit) {
			kept = int64(limit)
		}
		first := cursor.Add(time.Duration(total-kept) * schedule.interval)
		ticks := make([]time.Time, 0, int(kept))
		for i := int64(0); i < kept; i++ {
			ticks = append(ticks, first.Add(time.Duration(i)*schedule.interval))
		}
		return ticks, ticks[len(ticks)-1].Add(schedule.interval), nil
	}

	parsed, err := cron.Parse(schedule.cronSpec)
	if err != nil {
		return nil, time.Time{}, err
	}
	loc := schedule.location()
	// Search backwards so a long outage costs a fixed amount of memory and
	// retains the newest audit rows instead of the oldest ones. Cron field
	// matching is location-sensitive (the spec "0 2 * * *" means 02:00 in
	// the schedule's registration location, not 02:00 UTC), so the cursor
	// we walk minute-by-minute is rendered in that location before the
	// field comparison. The absolute instant is what we store.
	candidate := now.Truncate(time.Minute).In(loc)
	reversed := make([]time.Time, 0, limit)
	for scanned := 0; scanned < maxCronCatchUpSearchMinutes && !candidate.Before(cursor); scanned++ {
		if parsed.Matches(candidate) {
			reversed = append(reversed, candidate.UTC())
			if len(reversed) == limit {
				break
			}
		}
		candidate = candidate.Add(-time.Minute)
	}
	if len(reversed) == 0 {
		// Cadence-change recovery: the stored cursor is not an occurrence
		// of the current spec (typical after re-registering an existing
		// schedule with a different cron, or after switching interval→
		// cron — `ON CONFLICT ... DO UPDATE` deliberately preserves the
		// old next_run to fence concurrent claimants). Advance to the
		// next valid occurrence strictly after the cursor instead of
		// erroring: the caller persists the recovered watermark so the
		// schedule keeps firing on the new cadence without operator
		// intervention.
		next := parsed.Next(cursor.In(loc))
		if next.IsZero() {
			return nil, time.Time{}, fmt.Errorf("queue: schedule %q did not advance", schedule.id)
		}
		nextUTC := next.UTC()
		if nextUTC.After(now) {
			return nil, nextUTC, nil
		}
		nextNext := parsed.Next(next)
		if nextNext.IsZero() || !nextNext.After(next) {
			return nil, time.Time{}, fmt.Errorf("queue: schedule %q did not advance", schedule.id)
		}
		return []time.Time{nextUTC}, nextNext.UTC(), nil
	}
	ticks := make([]time.Time, len(reversed))
	for i := range reversed {
		ticks[len(reversed)-1-i] = reversed[i]
	}
	next := parsed.Next(now.In(loc))
	if next.IsZero() || !next.After(now) {
		return nil, time.Time{}, fmt.Errorf("queue: schedule %q did not advance", schedule.id)
	}
	return ticks, next.UTC(), nil
}

// tzName returns the IANA location name to persist for a schedule registered
// in loc. Empty means "evaluate in UTC" — the column default, which keeps
// pre-existing rows backward-compatible with the original UTC-only behaviour.
// A fixed offset zone (time.FixedZone) has no IANA name and cannot represent
// DST transitions, so it collapses to the empty default rather than persisting
// an un-loadable label.
func tzName(loc *time.Location) string {
	if loc == nil || loc == time.UTC {
		return ""
	}
	name := loc.String()
	if name == "" || name == "Local" {
		// "Local" round-trips through LoadLocation but resolves to
		// whatever zone the READING process runs in — replica-dependent
		// evaluation. Persist the UTC default instead; a schedule that
		// wants local-time semantics must register with a named IANA
		// zone.
		return ""
	}
	// FixedZone produces names like "UTC-05" that LoadLocation cannot resolve
	// back; round-trip fidelity requires a loadable IANA name.
	if _, err := time.LoadLocation(name); err != nil {
		return ""
	}
	return name
}

// location rebuilds the schedule's registration *time.Location from the
// persisted tz column. Empty (legacy rows, interval schedules, or un-loadable
// names recorded before tzName guarded them) falls back to UTC, preserving
// the pre-fix evaluation semantics.
func (s durableSchedule) location() *time.Location {
	if s.tz == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(s.tz)
	if err != nil || loc == nil {
		return time.UTC
	}
	return loc
}
