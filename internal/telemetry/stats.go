package telemetry

import (
	"bytes"
	"cmp"
	"errors"
	"maps"
	"slices"
	"time"
)

// StatsQuery asks for request counts over [From, To), both required.
type StatsQuery struct {
	From, To time.Time
	// Step is the width of one point of the series, a whole number of
	// minutes. From is moved back to a multiple of it.
	Step time.Duration
	// Route, when set, counts that route only.
	Route string
}

// StatsCount is the requests counted in one place: a point in time, a
// route, or the whole window.
type StatsCount struct {
	N, Failed, Slow int64
	// P50 and P95 are read from duration buckets, so each is right to
	// within its bucket: 180 ms may be any time from 100 to 200 ms. A
	// request slower than the last bucket's edge reads as that edge.
	P50, P95 time.Duration
	// Mean is exact.
	Mean time.Duration
}

// StatsPoint is one step of the series.
type StatsPoint struct {
	Time time.Time
	StatsCount
}

// RouteStats is one route's counts over the window, or one kind of
// background work's, whose Route is its name.
type RouteStats struct {
	Route string
	StatsCount
}

// Stats is the answer to a StatsQuery.
type Stats struct {
	Total StatsCount
	// Points covers the window with no gaps: a step with no requests
	// is a point of zeros.
	Points []StatsPoint
	// Routes is every route seen, busiest first.
	Routes []RouteStats
	// Jobs is the background work of the window (queue jobs, scheduled
	// runs), by name, busiest first, and JobsTotal all of it. It is not
	// in Total, Points or Routes: those are the requests people made.
	Jobs      []RouteStats
	JobsTotal StatsCount
}

// StatsCeiling is the longest duration a percentile reads as: a request
// that took longer is counted as this long.
const StatsCeiling = 10 * time.Second

// maxStatsPoints bounds a series: a chart has no use for more, and a
// one-minute step over a year must not be asked for.
const maxStatsPoints = 2000

// maxStatsRoutes bounds the routes of one answer. A minute holds at
// most maxStatRoutes; a window holds the union, which is the same few
// patterns unless a router names its routes by request.
const maxStatsRoutes = 2000

type statSum struct {
	n, failed, slow int64
	sum             int64 // microseconds
	h               [statBuckets]int64
}

func (a *statSum) add(n, failed, slow, sum int64, h *[statBuckets]int64) {
	a.n += n
	a.failed += failed
	a.slow += slow
	a.sum += sum
	for i, v := range h {
		a.h[i] += v
	}
}

func (a *statSum) count() StatsCount {
	c := StatsCount{N: a.n, Failed: a.failed, Slow: a.slow}
	if a.n > 0 {
		c.Mean = micros(a.sum / a.n)
		c.P50, c.P95 = quantile(&a.h, 50), quantile(&a.h, 95)
	}
	return c
}

// quantile reads the pct-th percentile from duration buckets: the
// bucket holding that request, and a place inside it by how far into
// the bucket's count the request falls.
func quantile(h *[statBuckets]int64, pct int64) time.Duration {
	var total int64
	for _, v := range h {
		total += v
	}
	if total <= 0 {
		return 0
	}
	rank := (total*pct + 99) / 100 // the request at this place, from 1
	var before int64
	for i, v := range h {
		if v <= 0 || before+v < rank {
			before += max(v, 0)
			continue
		}
		if i == len(statBounds) {
			return statBounds[len(statBounds)-1]
		}
		var lower time.Duration
		if i > 0 {
			lower = statBounds[i-1]
		}
		// The bucket's requests are taken to be spread evenly across
		// it, each at the middle of its own share: one request alone
		// reads as the middle of the bucket, not its far edge.
		return lower + (statBounds[i]-lower)*time.Duration(2*(rank-before)-1)/time.Duration(2*v)
	}
	return statBounds[len(statBounds)-1]
}

// parseStat reads one stats line. A count that is negative, or a line
// with no "n", is not a line this package wrote and is passed over.
func parseStat(line []byte) (route string, job bool, n, failed, slow, sum int64, h [statBuckets]int64, ok bool) {
	route, ok = stringField(line, `"route":`)
	if !ok {
		return
	}
	// The counts follow the route, whose text cannot hold a bare quote.
	rest := line[bytes.Index(line, []byte(`"route":`)):]
	// A route that spells the flag is written with its quotes escaped,
	// so these bytes can only be the flag itself.
	job = bytes.Contains(rest, []byte(`,"job":true`))
	n, ok = intField(rest, `"n":`)
	failed, _ = intField(rest, `"failed":`)
	slow, _ = intField(rest, `"slow":`)
	sum, _ = intField(rest, `"sum_us":`)
	if !ok || n <= 0 || failed < 0 || slow < 0 || sum < 0 {
		return route, job, 0, 0, 0, 0, h, false
	}
	i := bytes.Index(rest, []byte(`"h":[`))
	if i < 0 {
		return route, job, n, failed, slow, sum, h, true
	}
	var v int64
	slot, digits := 0, false
	for _, c := range rest[i+len(`"h":[`):] {
		switch {
		case c >= '0' && c <= '9':
			if v < 1<<50 {
				v = v*10 + int64(c-'0')
			}
			digits = true
		case c == ',' || c == ']':
			if digits && slot < statBuckets {
				h[slot] = v
			}
			slot++
			v, digits = 0, false
			if c == ']' {
				return route, job, n, failed, slow, sum, h, true
			}
		default:
			return route, job, n, failed, slow, sum, h, true
		}
	}
	return route, job, n, failed, slow, sum, h, true
}

// Stats counts the requests of a window from the per-minute lines the
// app wrote: all of them, where the traces are a sample.
func (s *Store) Stats(q StatsQuery) (Stats, error) {
	if q.From.IsZero() || q.To.IsZero() || !q.To.After(q.From) {
		return Stats{}, errors.New("telemetry: stats need a From before a To")
	}
	if q.Step < time.Minute || q.Step%time.Minute != 0 {
		return Stats{}, errors.New("telemetry: stats step must be whole minutes")
	}
	from := q.From.UTC().Truncate(q.Step)
	steps := q.To.Sub(from) / q.Step
	if q.To.Sub(from)%q.Step != 0 {
		steps++
	}
	if steps > maxStatsPoints {
		return Stats{}, errors.New("telemetry: stats window has too many steps")
	}
	points := make([]statSum, steps)
	routes, jobs := map[string]*statSum{}, map[string]*statSum{}
	var total, jobsTotal statSum
	err := s.scan(KindStats, from, q.To, false, false, func(line []byte) bool {
		at, ok := lineTime(line)
		if !ok || at.Before(from) || !at.Before(q.To) {
			return true
		}
		route, job, n, failed, slow, sum, h, ok := parseStat(line)
		if !ok || (q.Route != "" && route != q.Route) {
			return true
		}
		into := routes
		if job {
			into = jobs
			jobsTotal.add(n, failed, slow, sum, &h)
		} else {
			points[at.Sub(from)/q.Step].add(n, failed, slow, sum, &h)
			total.add(n, failed, slow, sum, &h)
		}
		r := into[route]
		if r == nil {
			if len(into) >= maxStatsRoutes {
				route = OtherRoute
				r = into[route]
			}
			if r == nil {
				r = &statSum{}
				into[route] = r
			}
		}
		r.add(n, failed, slow, sum, &h)
		return true
	})
	if err != nil {
		return Stats{}, err
	}
	out := Stats{Total: total.count(), Points: make([]StatsPoint, steps)}
	for i := range points {
		out.Points[i] = StatsPoint{Time: from.Add(time.Duration(i) * q.Step), StatsCount: points[i].count()}
	}
	list := func(m map[string]*statSum) []RouteStats {
		l := make([]RouteStats, 0, len(m))
		for _, route := range slices.Sorted(maps.Keys(m)) {
			l = append(l, RouteStats{Route: route, StatsCount: m[route].count()})
		}
		slices.SortStableFunc(l, func(a, b RouteStats) int { return cmp.Compare(b.N, a.N) })
		return l
	}
	out.Routes, out.Jobs, out.JobsTotal = list(routes), list(jobs), jobsTotal.count()
	return out, nil
}
