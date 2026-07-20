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
	// Forward walk in absolute minutes, evaluating the cron fields against
	// each minute rendered in the schedule's registration location. Cron
	// field matching is location-sensitive (the spec "0 2 * * *" means 02:00
	// in the schedule's registration location, not 02:00 UTC). DST transition
	// semantics (vixie/cronie parity) are implemented by firesInRange: a
	// wall-clock match in a fall-back repeated hour fires only at its earlier
	// absolute instant, and a skipped wall-clock minute inside a spring-
	// forward gap fires once at the transition instant. The absolute instant
	// is what we store. See durable_scheduler_dst.go.
	ticks, err := firesInRange(parsed, loc, cursor, now, limit)
	if err != nil {
		return nil, time.Time{}, err
	}
	// Advance the watermark past `now`, fenced against the LAST fired tick so
	// a fall-back repeat immediately after a fire is skipped (prevFire is the
	// wall-minute already consumed).
	var next time.Time
	if len(ticks) > 0 {
		next = nextFire(parsed, loc, now, ticks[len(ticks)-1])
	} else {
		// Cadence-change recovery: the stored cursor is not an occurrence of
		// the current spec (typical after re-registering an existing schedule
		// with a different cron, or after switching interval→cron — `ON
		// CONFLICT ... DO UPDATE` deliberately preserves the old next_run to
		// fence concurrent claimants). Advance to the next valid occurrence
		// strictly after the cursor instead of erroring: the caller persists
		// the recovered watermark so the schedule keeps firing on the new
		// cadence without operator intervention. With DST-aware nextFire this
		// also self-corrects across a spring-forward inside the gap.
		next = nextFire(parsed, loc, cursor, time.Time{})
	}
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
