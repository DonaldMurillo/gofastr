package queue

import (
	"fmt"
	"slices"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/cron"
)

func fires(parsed cron.Schedule, loc *time.Location, prevWall, t time.Time) bool {
	return parsed.FiresAt(prevWall.In(loc), t.In(loc))
}

func nextFire(parsed cron.Schedule, loc *time.Location, after time.Time) time.Time {
	if !parsed.Satisfiable() {
		return time.Time{}
	}
	t := after.UTC().Truncate(time.Minute).Add(time.Minute).In(loc)
	prevWall := t.Add(-time.Minute).In(loc)
	for end := t.AddDate(cron.HorizonYears, 0, 0); t.Before(end); {
		if fires(parsed, loc, prevWall, t) {
			return t.UTC()
		}
		prevWall = t
		t = parsed.SkipDay(t).In(loc)
	}
	return time.Time{}
}

func firesInRange(parsed cron.Schedule, loc *time.Location, cursor, now time.Time, limit int) ([]time.Time, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("queue: cron catch-up limit must be positive, got %d", limit)
	}
	start := cursor.UTC().Truncate(time.Minute).In(loc)
	end := now.UTC().Truncate(time.Minute).In(loc)
	if end.Before(start) {
		return nil, nil
	}
	if floor := end.Add(-time.Duration(maxCronCatchUpSearchMinutes-1) * time.Minute); start.Before(floor) {
		start = floor
	}
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
	if seen <= limit {
		return slices.Clip(ring), nil
	}
	head := seen % limit
	out := make([]time.Time, limit)
	copy(out, ring[head:])
	copy(out[limit-head:], ring[:head])
	return out, nil
}

func earliestTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
