package telemetry

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// bucketOf is the test's own reading of which bucket holds d.
func bucketEdges(d time.Duration) (lo, hi time.Duration) {
	edges := []time.Duration{0, time.Millisecond, 2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond,
		20 * time.Millisecond, 50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond,
		500 * time.Millisecond, time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second}
	for i := 1; i < len(edges); i++ {
		if d <= edges[i] {
			return edges[i-1], edges[i]
		}
	}
	return 10 * time.Second, 10 * time.Second
}

type obs struct {
	at           time.Time
	route        string
	dur          time.Duration
	failed, slow bool
}

// refCount counts a set of requests the plain way.
func refCount(t *testing.T, got StatsCount, in []obs, what string) {
	t.Helper()
	var n, failed, slow int64
	var sum time.Duration
	durs := make([]time.Duration, 0, len(in))
	for _, o := range in {
		n++
		if o.failed {
			failed++
		}
		if o.slow {
			slow++
		}
		sum += o.dur
		durs = append(durs, o.dur)
	}
	if got.N != n || got.Failed != failed || got.Slow != slow {
		t.Fatalf("%s: got n=%d failed=%d slow=%d, want %d %d %d", what, got.N, got.Failed, got.Slow, n, failed, slow)
	}
	if n == 0 {
		if got.P50 != 0 || got.P95 != 0 || got.Mean != 0 {
			t.Fatalf("%s: durations for no requests: %+v", what, got)
		}
		return
	}
	// The mean is exact to the microsecond each line's sum is cut to;
	// one line a route a minute, so at most a microsecond a request.
	if d := got.Mean - sum/time.Duration(n); d < -time.Microsecond || d > time.Microsecond {
		t.Fatalf("%s: mean %v, want %v", what, got.Mean, sum/time.Duration(n))
	}
	slices.Sort(durs)
	for _, p := range []struct {
		pct int64
		got time.Duration
	}{{50, got.P50}, {95, got.P95}} {
		exact := durs[(n*p.pct+99)/100-1]
		lo, hi := bucketEdges(exact)
		if p.got < lo || p.got > hi {
			t.Fatalf("%s: p%d is %v; the request at that place took %v, in the bucket %v to %v", what, p.pct, p.got, exact, lo, hi)
		}
	}
}

func statsStore(t *testing.T, dir string) *Store {
	t.Helper()
	s, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// What Stats reports is what counting the requests one by one reports,
// for the whole window, for each step and for each route, whether the
// day is still a plain file or has been sealed.
func TestStatsMatchCountingByHand(t *testing.T) {
	dir := t.TempDir()
	rng := rand.New(rand.NewPCG(7, 11))
	routes := []string{"GET /orders/{id}", "POST /checkout", `GET /quote"d\path`, "GET /"}
	r := NewRollup()
	var all []obs
	var file []byte
	for minute := range 180 {
		for range rng.IntN(40) {
			o := obs{
				at:    day0.Add(time.Duration(minute)*time.Minute + time.Duration(rng.IntN(60))*time.Second),
				route: routes[rng.IntN(len(routes))],
				dur:   time.Duration(rng.ExpFloat64() * float64(40*time.Millisecond)),
			}
			if rng.IntN(50) == 0 {
				o.dur = 12 * time.Second // past the last bucket's edge
			}
			o.failed, o.slow = rng.IntN(20) == 0, o.dur >= 500*time.Millisecond
			all = append(all, o)
			r.Observe(o.at, o.route, o.dur, o.failed, o.slow)
		}
		// The app flushes as it goes: finished minutes only.
		file = append(file, r.Take(day0.Add(time.Duration(minute)*time.Minute+15*time.Second), false)...)
	}
	file = append(file, r.Take(day0.Add(180*time.Minute), true)...)
	if err := os.WriteFile(filepath.Join(dir, FileName(KindStats, day0)), file, 0o600); err != nil {
		t.Fatal(err)
	}

	check := func(t *testing.T) {
		s := statsStore(t, dir)
		from, to, step := day0.Add(30*time.Minute), day0.Add(150*time.Minute), 15*time.Minute
		in := func(o obs) bool { return !o.at.Before(from) && o.at.Before(to) }
		got, err := s.Stats(StatsQuery{From: from, To: to, Step: step})
		if err != nil {
			t.Fatal(err)
		}
		var window []obs
		for _, o := range all {
			if in(o) {
				window = append(window, o)
			}
		}
		refCount(t, got.Total, window, "the window")
		if len(got.Points) != 8 {
			t.Fatalf("%d points, want 8", len(got.Points))
		}
		for i, p := range got.Points {
			start := from.Add(time.Duration(i) * step)
			if !p.Time.Equal(start) {
				t.Fatalf("point %d is at %v, want %v", i, p.Time, start)
			}
			var part []obs
			for _, o := range window {
				if !o.at.Before(start) && o.at.Before(start.Add(step)) {
					part = append(part, o)
				}
			}
			refCount(t, p.StatsCount, part, fmt.Sprintf("point %d", i))
		}
		if len(got.Routes) != len(routes) {
			t.Fatalf("%d routes, want %d", len(got.Routes), len(routes))
		}
		for i, rs := range got.Routes {
			if i > 0 && rs.N > got.Routes[i-1].N {
				t.Fatalf("routes are not busiest first: %d after %d", rs.N, got.Routes[i-1].N)
			}
			var part []obs
			for _, o := range window {
				if o.route == rs.Route {
					part = append(part, o)
				}
			}
			refCount(t, rs.StatsCount, part, rs.Route)
		}
		// One route only.
		one, err := s.Stats(StatsQuery{From: from, To: to, Step: step, Route: "POST /checkout"})
		if err != nil {
			t.Fatal(err)
		}
		var part []obs
		for _, o := range window {
			if o.route == "POST /checkout" {
				part = append(part, o)
			}
		}
		refCount(t, one.Total, part, "one route")
		if len(one.Routes) != 1 || one.Routes[0].Route != "POST /checkout" {
			t.Fatalf("one route asked for, got %+v", one.Routes)
		}
	}
	t.Run("plain", check)
	if err := SealDay(dir, KindStats, day0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName(KindStats, day0))); !os.IsNotExist(err) {
		t.Fatalf("the plain file is still there after sealing: %v", err)
	}
	t.Run("sealed", check)
}

// A flush leaves the minute still being counted alone, so a minute is
// one line a route unless the app stops in the middle of it.
func TestTakeLeavesTheMinuteInProgress(t *testing.T) {
	r := NewRollup()
	r.Observe(day0.Add(5*time.Minute+10*time.Second), "GET /a", time.Millisecond, false, false)
	r.Observe(day0.Add(6*time.Minute+1*time.Second), "GET /a", time.Millisecond, false, false)
	lines := string(r.Take(day0.Add(6*time.Minute+2*time.Second), false))
	if strings.Count(lines, "\n") != 1 || !strings.Contains(lines, `"time":"2026-11-10T00:05:00Z"`) {
		t.Fatalf("the flush at 00:06:02 wrote %q; want minute 00:05 alone", lines)
	}
	if again := r.Take(day0.Add(6*time.Minute+30*time.Second), false); len(again) != 0 {
		t.Fatalf("minute 00:06 was written while still in progress: %q", again)
	}
	rest := string(r.Take(day0.Add(6*time.Minute+40*time.Second), true))
	if strings.Count(rest, "\n") != 1 || !strings.Contains(rest, `"time":"2026-11-10T00:06:00Z"`) {
		t.Fatalf("stopping wrote %q; want minute 00:06", rest)
	}
	if left := r.Take(day0.Add(time.Hour), true); len(left) != 0 {
		t.Fatalf("a taken minute was written twice: %q", left)
	}
}

// A request that ended in the last seconds of a day is written after
// midnight, into the new day's file. Its line is stamped with the new
// day's first minute: a line from yesterday in today's file would sit
// before every line the reader expects there.
func TestTakeKeepsALineInsideItsDay(t *testing.T) {
	r := NewRollup()
	r.Observe(day0.Add(-10*time.Second), "GET /a", time.Millisecond, false, false)
	lines := string(r.Take(day0.Add(5*time.Second), false))
	if !strings.Contains(lines, `"time":"2026-11-10T00:00:00Z"`) {
		t.Fatalf("written just after midnight: %q; want the day's first minute", lines)
	}
}

// A router that names a route per request cannot grow a minute past its
// cap: the rest count together, and none is lost.
func TestRollupBoundsRoutesInAMinute(t *testing.T) {
	r := NewRollup()
	const extra = 37
	for i := range maxStatRoutes + extra {
		r.Observe(day0, fmt.Sprintf("GET /r%04d", i), time.Millisecond, false, false)
	}
	// A route already counted keeps its own line.
	r.Observe(day0, "GET /r0000", time.Millisecond, false, false)
	lines := string(r.Take(day0.Add(time.Hour), false))
	if n := strings.Count(lines, "\n"); n != maxStatRoutes+1 {
		t.Fatalf("%d lines, want %d routes and one for the rest", n, maxStatRoutes)
	}
	if !strings.Contains(lines, fmt.Sprintf(`"route":"(other)","n":%d,`, extra)) {
		t.Fatalf("the rest are not counted together as %d", extra)
	}
	if !strings.Contains(lines, `"route":"GET /r0000","n":2,`) {
		t.Fatal("a route already counted lost its line once the minute was full")
	}
	// The next minute starts its own count.
	r.Observe(day0.Add(time.Minute), "GET /fresh", time.Millisecond, false, false)
	if next := string(r.Take(day0.Add(time.Hour), false)); !strings.Contains(next, `"route":"GET /fresh"`) {
		t.Fatalf("the minute after a full one: %q", next)
	}
}

// The collector counts every request, sampled out or not, and leaves
// out the two kinds that are not traffic: a quiet path and a lone call.
func TestCollectorCountsWhatItDoesNotKeep(t *testing.T) {
	c, err := NewCollector(Config{Dir: "unused", KeepNoOrdinary: true, SlowThreshold: 100 * time.Millisecond}, func(Trace) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	r := NewRollup()
	c.WatchStats(r)
	end := func(id string, e SpanEnd) {
		e.TraceID, e.Root, e.Start = fmt.Sprintf("%032s", id), true, day0
		e.Span.SpanID = "01"
		c.SpanEnded(e)
	}
	end("1", SpanEnd{Route: "GET /a", Span: Span{Dur: 5 * time.Millisecond}})
	end("2", SpanEnd{Route: "GET /a", Span: Span{Dur: 300 * time.Millisecond}})
	end("3", SpanEnd{Route: "GET /a", Error: true, Span: Span{Dur: 5 * time.Millisecond}})
	end("4", SpanEnd{Route: "GET /core-ui/x.js", Quiet: true, Span: Span{Dur: 5 * time.Millisecond}})
	end("5", SpanEnd{Route: "SELECT", Stray: true, Span: Span{Dur: 900 * time.Millisecond}})
	if st := c.Stats(); st.SampledOut != 2 {
		t.Fatalf("sampled out %d; the test needs the ordinary and the quiet request not kept", st.SampledOut)
	}
	lines := string(r.Take(day0.Add(time.Hour), true))
	want := `"route":"GET /a","n":3,"failed":1,"slow":1,"sum_us":310000,"h":[0,0,2,0,0,0,0,0,1]}` + "\n"
	if !strings.HasSuffix(lines, want) || strings.Count(lines, "\n") != 1 {
		t.Fatalf("counted %q\nwant one line ending %q", lines, want)
	}
}

// A line this package did not write is passed over, never counted as
// a guess.
func TestStatsPassOverLinesTheyCannotRead(t *testing.T) {
	dir := t.TempDir()
	at := day0.Add(time.Minute).Format(time.RFC3339)
	file := strings.Join([]string{
		`{"time":"` + at + `","route":"GET /ok","n":4,"failed":1,"slow":0,"sum_us":4000,"h":[4]}`,
		`{"time":"` + at + `","route":"GET /neg","n":-3,"failed":0,"slow":0,"sum_us":1,"h":[1]}`,
		`{"time":"` + at + `","n":9}`,
		`{"time":"` + at + `","route":"GET /nocount"}`,
		`{"time":"` + at + `","route":"GET /badh","n":1,"failed":0,"slow":0,"sum_us":1,"h":[x`,
		`not json at all`,
		`{"time":"` + at + `","route":"GET /long","n":2,"failed":0,"slow":0,"sum_us":2,"h":[0,0,0,0,0,0,0,0,0,0,0,0,0,2,7,7,7]}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, FileName(KindStats, day0)), []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := statsStore(t, dir).Stats(StatsQuery{From: day0, To: day0.Add(time.Hour), Step: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range got.Routes {
		names = append(names, fmt.Sprintf("%s=%d", r.Route, r.N))
	}
	slices.Sort(names)
	if strings.Join(names, " ") != "GET /badh=1 GET /long=2 GET /ok=4" {
		t.Fatalf("counted %v", names)
	}
	if got.Total.N != 7 || got.Total.Failed != 1 {
		t.Fatalf("total %+v", got.Total)
	}
	// Buckets past the last are not read into the ones before them.
	for _, r := range got.Routes {
		if r.Route == "GET /long" && r.P95 != 10*time.Second {
			t.Fatalf("p95 of two requests in the last bucket: %v", r.P95)
		}
	}
}

func TestStatsRefuseAWindowTheyCannotAnswer(t *testing.T) {
	s := statsStore(t, t.TempDir())
	for name, q := range map[string]StatsQuery{
		"no from":         {To: day0, Step: time.Minute},
		"no to":           {From: day0, Step: time.Minute},
		"to before from":  {From: day0, To: day0.Add(-time.Minute), Step: time.Minute},
		"empty":           {From: day0, To: day0, Step: time.Minute},
		"no step":         {From: day0, To: day0.Add(time.Hour)},
		"step of seconds": {From: day0, To: day0.Add(time.Hour), Step: 90 * time.Second},
		"too many steps":  {From: day0, To: day0.Add((maxStatsPoints + 1) * time.Minute), Step: time.Minute},
	} {
		if _, err := s.Stats(q); err == nil {
			t.Errorf("%s: answered", name)
		}
	}
	// The most steps allowed, and a window that does not end on a step.
	got, err := s.Stats(StatsQuery{From: day0, To: day0.Add(maxStatsPoints * time.Minute), Step: time.Minute})
	if err != nil || len(got.Points) != maxStatsPoints {
		t.Fatalf("%d points, %v", len(got.Points), err)
	}
	got, err = s.Stats(StatsQuery{From: day0.Add(7 * time.Minute), To: day0.Add(22 * time.Minute), Step: 5 * time.Minute})
	if err != nil || len(got.Points) != 4 || !got.Points[0].Time.Equal(day0.Add(5*time.Minute)) {
		t.Fatalf("00:07 to 00:22 by 5 minutes: %d points from %v, %v", len(got.Points), got.Points, err)
	}
}

// The writer puts the counts in the day's own file, and removes it with
// the day.
func TestWriterFlushesStatsIntoTheDayFile(t *testing.T) {
	dir := t.TempDir()
	now := day0.Add(10 * time.Minute)
	w, err := newWriter(Config{Dir: dir, Retention: 48 * time.Hour}, func() time.Time { return now }, false)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRollup()
	r.Observe(day0.Add(9*time.Minute), "GET /a", time.Millisecond, false, false)
	r.Observe(day0.Add(10*time.Minute), "GET /a", time.Millisecond, true, false)
	if err := w.FlushStats(r, false); err != nil {
		t.Fatal(err)
	}
	got, err := statsStore(t, dir).Stats(StatsQuery{From: day0, To: day0.Add(time.Hour), Step: time.Hour})
	if err != nil || got.Total.N != 1 {
		t.Fatalf("after a flush in minute 10: n=%d, %v; want the finished minute's one request", got.Total.N, err)
	}
	if err := w.FlushStats(r, true); err != nil {
		t.Fatal(err)
	}
	got, _ = statsStore(t, dir).Stats(StatsQuery{From: day0, To: day0.Add(time.Hour), Step: time.Hour})
	if got.Total.N != 2 || got.Total.Failed != 1 {
		t.Fatalf("after stopping: %+v", got.Total)
	}
	// Three days on, the file is past Retention and goes with its day.
	now = day0.Add(72 * time.Hour)
	if err := w.prune(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName(KindStats, day0))); !os.IsNotExist(err) {
		t.Fatalf("the stats file outlived its day: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

// The percentile is the request at that place, counted from the fast
// end and rounded up: of three requests the middle one is the second.
func TestQuantileIsTheRequestAtThatPlace(t *testing.T) {
	var h [statBuckets]int64
	if got := quantile(&h, 95); got != 0 {
		t.Fatalf("no requests: %v", got)
	}
	h[0], h[3], h[8] = 1, 1, 1 // up to 1 ms, 5 to 10 ms, 200 to 500 ms
	// Alone in its bucket, a request reads as the bucket's middle.
	if got := quantile(&h, 50); got != 7500*time.Microsecond {
		t.Fatalf("the middle of three is %v, want the middle of the second one's bucket, 7.5ms", got)
	}
	if got := quantile(&h, 95); got != 350*time.Millisecond {
		t.Fatalf("p95 of three is %v, want the middle of the slowest one's bucket, 350ms", got)
	}
	// Inside a bucket, by how far into its count the place falls.
	h = [statBuckets]int64{}
	h[6] = 100 // 50 to 100 ms
	// The fiftieth of a hundred spread across 50-100ms sits at the
	// middle of its own half-millisecond share: 49.5 shares in.
	if got := quantile(&h, 50); got != 74750*time.Microsecond {
		t.Fatalf("half way into 50-100ms: %v", got)
	}
	// Slower than the last edge reads as that edge, which is the
	// ceiling the screens say "or more" at.
	if StatsCeiling != statBounds[len(statBounds)-1] {
		t.Fatalf("StatsCeiling %v is not the last bucket's edge %v", StatsCeiling, statBounds[len(statBounds)-1])
	}
	h = [statBuckets]int64{}
	h[statBuckets-1] = 5
	if got := quantile(&h, 50); got != 10*time.Second {
		t.Fatalf("past the last edge: %v", got)
	}
}

// What counting adds to a request: one lock and one map lookup.
func BenchmarkRollupObserve(b *testing.B) {
	r := NewRollup()
	routes := []string{"GET /orders/{id}", "POST /checkout", "GET /", "GET /search"}
	b.ReportAllocs()
	for i := 0; b.Loop(); i++ {
		r.Observe(day0.Add(time.Duration(i%60)*time.Second), routes[i%len(routes)], time.Duration(i%900)*time.Millisecond, i%50 == 0, false)
	}
}

// Background work is counted apart from the requests people made: its
// own lines, its own list and total, and none of it in the request
// totals, the series or the routes. A route whose text merely spells
// the flag is still a route.
func TestJobsAreCountedApartFromRequests(t *testing.T) {
	dir := t.TempDir()
	r := NewRollup()
	at := day0.Add(time.Minute)
	sneaky := `GET /x","job":true,"n":999`
	for i := range 10 {
		r.Observe(at, "GET /orders/{id}", 5*time.Millisecond, i == 0, false)
	}
	r.Observe(at, sneaky, time.Millisecond, false, false)
	for i := range 6 {
		r.ObserveJob(at, "process welcome-email", 40*time.Millisecond, i < 2)
	}
	r.ObserveJob(at, "cron nightly", 3*time.Second, false)
	// A request and a job may share a name without sharing a count.
	r.ObserveJob(at, "GET /orders/{id}", time.Millisecond, true)
	lines := string(r.Take(day0.Add(time.Hour), false))
	if strings.Count(lines, "\n") != 5 || strings.Count(lines, `","job":true,"n":`) != 3 {
		t.Fatalf("lines:\n%s", lines)
	}
	if !strings.Contains(lines, `"route":"cron nightly","job":true,"n":1,"failed":0,"slow":0,`) {
		t.Fatalf("a job's line, which is never slow:\n%s", lines)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName(KindStats, day0)), []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	s := statsStore(t, dir)
	got, err := s.Stats(StatsQuery{From: day0, To: day0.Add(time.Hour), Step: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if got.Total.N != 11 || got.Total.Failed != 1 || got.Points[0].N != 11 {
		t.Fatalf("requests: total %+v, series %+v", got.Total, got.Points[0].StatsCount)
	}
	if len(got.Routes) != 2 || got.Routes[0].Route != "GET /orders/{id}" || got.Routes[0].N != 10 || got.Routes[1].Route != sneaky || got.Routes[1].N != 1 {
		t.Fatalf("routes: %+v", got.Routes)
	}
	if got.JobsTotal.N != 8 || got.JobsTotal.Failed != 3 || got.JobsTotal.Slow != 0 {
		t.Fatalf("jobs total: %+v", got.JobsTotal)
	}
	if len(got.Jobs) != 3 || got.Jobs[0].Route != "process welcome-email" || got.Jobs[0].N != 6 || got.Jobs[0].Failed != 2 {
		t.Fatalf("jobs: %+v", got.Jobs)
	}
	// One name asked for: the request and the job of that name, each
	// in its own place.
	one, err := s.Stats(StatsQuery{From: day0, To: day0.Add(time.Hour), Step: time.Hour, Route: "GET /orders/{id}"})
	if err != nil || one.Total.N != 10 || one.JobsTotal.N != 1 || len(one.Routes) != 1 || len(one.Jobs) != 1 {
		t.Fatalf("one name: %+v, %v", one, err)
	}

	// The collector tells them apart by what the span says it is.
	c, err := NewCollector(Config{Dir: "unused", KeepNoOrdinary: true, SlowThreshold: 100 * time.Millisecond}, func(Trace) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	r = NewRollup()
	c.WatchStats(r)
	c.SpanEnded(SpanEnd{TraceID: fmt.Sprintf("%032x", 1), Root: true, Start: day0, Route: "GET /a", Span: Span{SpanID: "01", Dur: 300 * time.Millisecond}})
	c.SpanEnded(SpanEnd{TraceID: fmt.Sprintf("%032x", 2), Root: true, Start: day0, Route: "cron nightly", Job: true, Span: Span{SpanID: "01", Dur: 300 * time.Millisecond}})
	lines = string(r.Take(day0.Add(time.Hour), true))
	if !strings.Contains(lines, `"route":"GET /a","n":1,"failed":0,"slow":1,`) || !strings.Contains(lines, `"route":"cron nightly","job":true,"n":1,"failed":0,"slow":0,`) {
		t.Fatalf("from the collector:\n%s", lines)
	}
}
