package queue

import (
	"fmt"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/cron"
)

const (
	defaultMaxCatchUpOccurrences = 1000
	maxCronCatchUpSearchMinutes  = 5 * 366 * 24 * 60
	minDurableInterval           = time.Millisecond
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
		if schedule.interval < minDurableInterval {
			return nil, time.Time{}, fmt.Errorf("queue: schedule %q interval %s is below the %s floor", schedule.id, schedule.interval, minDurableInterval)
		}
		elapsed := now.Sub(cursor)
		total := int64(elapsed/schedule.interval) + 1
		kept := min(total, int64(limit))
		first := cursor.Add(time.Duration(total-kept) * schedule.interval)
		ticks := make([]time.Time, 0, int(kept))
		for i := range kept {
			ticks = append(ticks, first.Add(time.Duration(i)*schedule.interval))
		}
		return ticks, ticks[len(ticks)-1].Add(schedule.interval), nil
	}

	parsed, err := cron.Parse(schedule.cronSpec)
	if err != nil {
		return nil, time.Time{}, err
	}
	loc := schedule.location()
	ticks, err := firesInRange(parsed, loc, cursor, now, limit)
	if err != nil {
		return nil, time.Time{}, err
	}
	var next time.Time
	if len(ticks) > 0 {
		next = nextFire(parsed, loc, now)
	} else {
		next = nextFire(parsed, loc, cursor)
	}
	if next.IsZero() || !next.After(now) {
		return nil, time.Time{}, fmt.Errorf("queue: schedule %q did not advance", schedule.id)
	}
	return ticks, next.UTC(), nil
}

func tzName(loc *time.Location) string {
	if loc == nil || loc == time.UTC {
		return ""
	}
	name := loc.String()
	if name == "" || name == "Local" {
		return ""
	}
	if _, err := time.LoadLocation(name); err != nil {
		return ""
	}
	return name
}

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
