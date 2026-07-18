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
	// Search backwards so a long outage costs a fixed amount of memory and
	// retains the newest audit rows instead of the oldest ones.
	candidate := now.Truncate(time.Minute)
	reversed := make([]time.Time, 0, limit)
	for scanned := 0; scanned < maxCronCatchUpSearchMinutes && !candidate.Before(cursor); scanned++ {
		if parsed.Matches(candidate) {
			reversed = append(reversed, candidate)
			if len(reversed) == limit {
				break
			}
		}
		candidate = candidate.Add(-time.Minute)
	}
	if len(reversed) == 0 {
		return nil, time.Time{}, fmt.Errorf("queue: schedule %q did not produce a due cron tick", schedule.id)
	}
	ticks := make([]time.Time, len(reversed))
	for i := range reversed {
		ticks[len(reversed)-1-i] = reversed[i]
	}
	next := parsed.Next(now)
	if next.IsZero() || !next.After(now) {
		return nil, time.Time{}, fmt.Errorf("queue: schedule %q did not advance", schedule.id)
	}
	return ticks, next.UTC(), nil
}
