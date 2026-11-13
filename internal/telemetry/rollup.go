package telemetry

import (
	"slices"
	"strconv"
	"sync"
	"time"
)

// statBounds are the upper edges of the duration buckets a minute's
// requests are counted into; one more bucket holds everything slower
// than the last. Coarse on purpose: fourteen numbers a line say where
// the middle and the slow tail of a route are, to within a bucket.
var statBounds = [...]time.Duration{
	time.Millisecond, 2 * time.Millisecond, 5 * time.Millisecond,
	10 * time.Millisecond, 20 * time.Millisecond, 50 * time.Millisecond,
	100 * time.Millisecond, 200 * time.Millisecond, 500 * time.Millisecond,
	time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second,
}

const statBuckets = len(statBounds) + 1

// maxStatRoutes is the routes counted apart in one minute. Past it a
// request counts under OtherRoute: routes are patterns and few, and a
// router that hands out a new name per request must not grow the map.
const maxStatRoutes = 200

// OtherRoute is the route of requests past maxStatRoutes in a minute.
const OtherRoute = "(other)"

type statKey struct {
	minute int64 // minutes since the epoch
	route  string
}

type statCell struct {
	n, failed, slow int64
	sum             time.Duration
	h               [statBuckets]int64
}

// Rollup counts every finished request by minute and route: how many,
// how many failed, how many were slow, and how long they took. The
// traces on disk are a sample; these counts are not, so a rate or a
// percentile read from them is about all of the traffic.
type Rollup struct {
	mu     sync.Mutex
	cells  map[statKey]*statCell
	routes map[int64]int // routes seen, by minute
}

// NewRollup returns an empty Rollup.
func NewRollup() *Rollup {
	return &Rollup{cells: map[statKey]*statCell{}, routes: map[int64]int{}}
}

func statBucket(d time.Duration) int {
	for i, b := range statBounds {
		if d <= b {
			return i
		}
	}
	return len(statBounds)
}

// Observe counts one finished request.
func (r *Rollup) Observe(at time.Time, route string, dur time.Duration, failed, slow bool) {
	if dur < 0 {
		dur = 0
	}
	minute := at.Unix() / 60
	r.mu.Lock()
	defer r.mu.Unlock()
	k := statKey{minute, route}
	c := r.cells[k]
	if c == nil {
		if r.routes[minute] >= maxStatRoutes {
			k.route = OtherRoute
			c = r.cells[k]
		}
		if c == nil {
			c = &statCell{}
			r.cells[k] = c
			if k.route != OtherRoute {
				r.routes[minute]++
			}
		}
	}
	c.n++
	if failed {
		c.failed++
	}
	if slow {
		c.slow++
	}
	c.sum += dur
	c.h[statBucket(dur)]++
}

// Take removes the minutes that ended before now, or every minute when
// all is set, and returns them as lines in time order, each ending in a
// newline. A minute from before now's UTC day is stamped with the day's
// first minute: a day file holds its own day's lines only, and the few
// requests that ended in the seconds before midnight count in the
// minute after it.
func (r *Rollup) Take(now time.Time, all bool) []byte {
	current := now.Unix() / 60
	midnight := now.UTC().Truncate(24*time.Hour).Unix() / 60
	r.mu.Lock()
	var keys []statKey
	for k := range r.cells {
		if all || k.minute < current {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b statKey) int {
		if a.minute != b.minute {
			return int(a.minute - b.minute)
		}
		return cmpString(a.route, b.route)
	})
	cells := make([]statCell, len(keys))
	for i, k := range keys {
		cells[i] = *r.cells[k]
		delete(r.cells, k)
		delete(r.routes, k.minute)
	}
	r.mu.Unlock()

	var out []byte
	for i, k := range keys {
		out = appendStat(out, time.Unix(max(k.minute, midnight)*60, 0), k.route, &cells[i])
	}
	return out
}

// appendStat writes one line: "time" first, as every day file's lines
// have it, then the route and its counts. "h" is the duration buckets
// with the empty ones at its end left off.
func appendStat(b []byte, minute time.Time, route string, c *statCell) []byte {
	b = append(b, `{"time":"`...)
	b = minute.UTC().AppendFormat(b, time.RFC3339)
	b = append(b, `","route":`...)
	b = appendJSONString(b, route)
	b = append(b, `,"n":`...)
	b = strconv.AppendInt(b, c.n, 10)
	b = append(b, `,"failed":`...)
	b = strconv.AppendInt(b, c.failed, 10)
	b = append(b, `,"slow":`...)
	b = strconv.AppendInt(b, c.slow, 10)
	b = append(b, `,"sum_us":`...)
	b = strconv.AppendInt(b, c.sum.Microseconds(), 10)
	b = append(b, `,"h":[`...)
	last := statBuckets - 1
	for last > 0 && c.h[last] == 0 {
		last--
	}
	for i := 0; i <= last; i++ {
		if i > 0 {
			b = append(b, ',')
		}
		b = strconv.AppendInt(b, c.h[i], 10)
	}
	return append(b, "]}\n"...)
}
