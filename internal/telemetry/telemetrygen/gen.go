// Package telemetrygen writes a synthetic, repeatable set of log and
// trace files for the telemetry benchmarks: the same seed and load give
// the same bytes on every machine. Log lines go through slog's JSON
// handler, the one battery/log writes with, so they have the real shape.
package telemetrygen

import (
	"bufio"
	"container/heap"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
	"github.com/DonaldMurillo/gofastr/internal/telemetry"
)

// Load describes the traffic to synthesize.
type Load struct {
	// RPS is requests a second, held steady over every day.
	RPS float64
	// Days is how many whole UTC days to write, ending the day before
	// End's.
	Days int
	// End anchors the data in time; the last day written is the one
	// before End's UTC day.
	End time.Time
	// Seed picks the random stream.
	Seed uint64
}

// Sample is one kept trace the benchmarks can ask for by name.
type Sample struct {
	TraceID string    `json:"trace_id"`
	Route   string    `json:"route"`
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
}

// Stats reports what Generate wrote.
type Stats struct {
	RPS        float64   `json:"rps"`
	Days       int       `json:"days"`
	Seed       uint64    `json:"seed"`
	First      time.Time `json:"first"`
	Last       time.Time `json:"last"`
	Requests   int64     `json:"requests"`
	LogLines   int64     `json:"log_lines"`
	LogBytes   int64     `json:"log_bytes"`
	Traces     int64     `json:"traces"`
	TraceBytes int64     `json:"trace_bytes"`
	// Samples holds one kept trace from the middle of each day, oldest
	// day first.
	Samples []Sample `json:"samples"`
}

// Needle is a word Generate puts in the detail of about one request in
// 20,000, for a search with few hits.
const Needle = "zebrafinch"

const manifest = "manifest.json"

var routes = []string{
	"GET /", "GET /dashboard", "GET /orders", "GET /orders/{id}", "POST /orders",
	"GET /customers", "GET /customers/{id}", "PATCH /customers/{id}", "GET /invoices",
	"GET /invoices/{id}", "POST /invoices", "GET /reports/sales", "GET /settings",
	"POST /login", "POST /logout", "GET /search", "GET /api/orders", "GET /api/customers",
	"POST /api/webhooks/stripe", "GET /healthz",
}

var words = []string{
	"loaded", "cache", "miss", "hit", "row", "rows", "tenant", "render", "island", "form",
	"validated", "queued", "job", "email", "sent", "retry", "upstream", "token", "refreshed",
	"session", "page", "filter", "sort", "export", "invoice", "order", "customer", "saved",
}

// Existing returns the Stats of a data set already in dir, if it was
// written for this load.
func Existing(dir string, load Load) (Stats, bool) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Stats{}, false
	}
	defer root.Close()
	b, err := root.ReadFile(manifest)
	if err != nil {
		return Stats{}, false
	}
	var st Stats
	if json.Unmarshal(b, &st) != nil {
		return Stats{}, false
	}
	same := st.RPS == load.RPS && st.Days == load.Days && st.Seed == load.Seed &&
		st.Last.Equal(lastDay(load).Add(24*time.Hour))
	return st, same
}

func lastDay(load Load) time.Time {
	return load.End.UTC().Truncate(24 * time.Hour).Add(-24 * time.Hour)
}

// Generate writes load's files into cfg.Dir, creating it if needed and
// replacing any day files already there. cfg's SampleRate and
// SlowThreshold decide which traces are kept, as the writer will.
func Generate(cfg telemetry.Config, load Load) (Stats, error) {
	cfg, err := cfg.Resolve()
	if err != nil {
		return Stats{}, err
	}
	if math.IsNaN(load.RPS) || load.RPS <= 0 || load.RPS > 1e5 || load.Days < 1 || load.Days > 366 {
		return Stats{}, errors.New("telemetrygen: RPS must be in (0, 100000] and Days in [1, 366]")
	}
	if err := fileperm.MkdirPrivate(cfg.Dir); err != nil {
		return Stats{}, fmt.Errorf("telemetrygen: mkdir: %w", err)
	}
	root, err := os.OpenRoot(cfg.Dir)
	if err != nil {
		return Stats{}, fmt.Errorf("telemetrygen: open: %w", err)
	}
	defer root.Close()

	g := &gen{
		cfg:  cfg,
		rng:  rand.New(rand.NewPCG(load.Seed, 0x9e3779b97f4a7c15)),
		step: time.Duration(float64(time.Second) / load.RPS),
	}
	g.stats = Stats{RPS: load.RPS, Days: load.Days, Seed: load.Seed}
	last := lastDay(load)
	g.stats.First = last.Add(-time.Duration(load.Days-1) * 24 * time.Hour)
	g.stats.Last = last.Add(24 * time.Hour)
	for d := range load.Days {
		if err := g.day(root, g.stats.First.Add(time.Duration(d)*24*time.Hour)); err != nil {
			return Stats{}, err
		}
	}
	b, err := json.MarshalIndent(g.stats, "", "  ")
	if err != nil {
		return Stats{}, fmt.Errorf("telemetrygen: manifest: %w", err)
	}
	if err := root.WriteFile(manifest, b, 0o600); err != nil {
		return Stats{}, fmt.Errorf("telemetrygen: manifest: %w", err)
	}
	return g.stats, nil
}

type gen struct {
	cfg   telemetry.Config
	rng   *rand.Rand
	step  time.Duration
	stats Stats
	buf   []byte
}

// counter counts the bytes that reach a file.
type counter struct {
	w *bufio.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// pending is the traces whose root has not ended yet, soonest end
// first: a trace file is in end order, as the real writer leaves it.
type pending []telemetry.Trace

func (p pending) Len() int           { return len(p) }
func (p pending) Less(i, j int) bool { return p[i].Time.Before(p[j].Time) }
func (p pending) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p *pending) Push(x any)        { *p = append(*p, x.(telemetry.Trace)) }
func (p *pending) Pop() any {
	old := *p
	t := old[len(old)-1]
	*p = old[:len(old)-1]
	return t
}

func (g *gen) day(root *os.Root, day time.Time) (err error) {
	open := func(kind string) (*os.File, *counter, error) {
		f, err := root.OpenFile(telemetry.FileName(kind, day), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, nil, fmt.Errorf("telemetrygen: create: %w", err)
		}
		return f, &counter{w: bufio.NewWriterSize(f, 1<<20)}, nil
	}
	lf, lw, err := open(telemetry.KindLogs)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lf.Close()) }()
	tf, tw, err := open(telemetry.KindTraces)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, tf.Close()) }()

	logger := slog.NewJSONHandler(lw, &slog.HandlerOptions{Level: slog.LevelDebug})
	var open2 pending
	flush := func(until time.Time) error {
		for len(open2) > 0 && !open2[0].Time.After(until) {
			tr := heap.Pop(&open2).(telemetry.Trace)
			g.buf = telemetry.AppendTrace(g.buf[:0], tr)
			if _, err := tw.Write(g.buf); err != nil {
				return fmt.Errorf("telemetrygen: write: %w", err)
			}
			g.stats.Traces++
		}
		return nil
	}
	end := day.Add(24 * time.Hour)
	noon := day.Add(12 * time.Hour)
	sampled := false
	for t := day; t.Before(end); t = t.Add(g.step) {
		tr, err := g.request(logger, t.Add(time.Duration(g.rng.Int64N(int64(g.step)))))
		if err != nil {
			return err
		}
		g.stats.Requests++
		if tr == nil || !tr.Time.Before(end) {
			continue // not kept, or it ends in tomorrow's file
		}
		if !sampled && !t.Before(noon) {
			sampled = true
			g.stats.Samples = append(g.stats.Samples, Sample{
				TraceID: tr.TraceID, Route: tr.Route, Start: tr.Time.Add(-tr.Dur), End: tr.Time,
			})
		}
		heap.Push(&open2, *tr)
		if err := flush(t); err != nil {
			return err
		}
	}
	if err := flush(end); err != nil {
		return err
	}
	if err := errors.Join(lw.w.Flush(), tw.w.Flush()); err != nil {
		return fmt.Errorf("telemetrygen: flush: %w", err)
	}
	g.stats.LogBytes += lw.n
	g.stats.TraceBytes += tw.n
	return nil
}

func (g *gen) id(n int) string {
	b := make([]byte, n)
	for i := 0; i < n; i += 8 {
		v := g.rng.Uint64()
		for j := i; j < min(i+8, n); j++ {
			b[j] = byte(v)
			v >>= 8
		}
	}
	return hex.EncodeToString(b)
}

// request writes one request's three log lines and returns its trace if
// the sampling rules keep it.
func (g *gen) request(h slog.Handler, start time.Time) (*telemetry.Trace, error) {
	route := routes[int(math.Pow(g.rng.Float64(), 2)*float64(len(routes)))] // early routes are busier
	// Log-normal around 20 ms; about one request in 300 passes 500 ms.
	dur := time.Duration(20e6 * math.Exp(g.rng.NormFloat64()*1.2))
	status, level := 200, slog.LevelInfo
	switch p := g.rng.Float64(); {
	case p < 0.02:
		status, level = 500, slog.LevelError
	case p < 0.07:
		status, level = 404, slog.LevelWarn
	}
	traceID, rootID, reqID := g.id(16), g.id(8), g.id(16)
	ids := []slog.Attr{slog.String("request_id", reqID), slog.String("trace_id", traceID), slog.String("span_id", rootID)}

	detail := words[g.rng.IntN(len(words))] + " " + words[g.rng.IntN(len(words))] + " " + words[g.rng.IntN(len(words))]
	if g.rng.IntN(20000) == 0 {
		detail += " " + Needle
	}
	emit := func(at time.Time, lv slog.Level, msg string, attrs ...slog.Attr) error {
		r := slog.NewRecord(at, lv, msg, 0)
		r.AddAttrs(attrs...)
		r.AddAttrs(ids...)
		g.stats.LogLines++
		if err := h.Handle(context.Background(), r); err != nil {
			return fmt.Errorf("telemetrygen: write: %w", err)
		}
		return nil
	}
	user := g.rng.IntN(5000)
	method, path, _ := strings.Cut(route, " ")
	err := errors.Join(
		emit(start, slog.LevelDebug, "handler.start", slog.String("route", route), slog.Int("user_id", user), slog.String("tenant", "acme-"+g.id(2))),
		emit(start.Add(time.Microsecond), slog.LevelInfo, "app.step", slog.String("detail", detail), slog.Int("rows", g.rng.IntN(200)), slog.Int("user_id", user)),
		emit(start.Add(2*time.Microsecond), level, "http.access", slog.String("method", method), slog.String("path", path),
			slog.Int("status", status), slog.Int("bytes", 200+g.rng.IntN(40000)), slog.Float64("dur_ms", float64(dur.Microseconds())/1000),
			slog.String("remote", fmt.Sprintf("203.0.113.%d", g.rng.IntN(255)))),
	)
	if err != nil {
		return nil, err
	}
	if status < 500 && dur < g.cfg.SlowThreshold && g.rng.Float64() >= g.cfg.SampleRate {
		return nil, nil
	}
	// A failed request fails in one of a few ways, each with a detail
	// that changes from one failure to the next.
	var errText string
	if status >= 500 {
		errText = []string{
			fmt.Sprintf("upstream timed out after %dms", 3000+g.rng.IntN(900)),
			fmt.Sprintf("dial tcp 10.0.0.%d:5432: connect: connection refused", g.rng.IntN(255)),
			"pq: deadlock detected",
		}[g.rng.IntN(3)]
	}
	tr := &telemetry.Trace{
		Time: start.Add(dur), TraceID: traceID, Route: route, Status: status, Dur: dur, Error: status >= 500, Err: errText,
		Spans: []telemetry.Span{{SpanID: rootID, Name: "HTTP " + route, Dur: dur,
			Attrs: []telemetry.Attr{{Key: "http.status_code", Value: fmt.Sprint(status)}, {Key: "request_id", Value: reqID}}}},
	}
	for i := range 7 { // seven children: queries and one outbound call
		s := telemetry.Span{SpanID: g.id(8), ParentID: rootID, Offset: dur * time.Duration(i) / 8, Dur: dur / 9}
		if i == 5 {
			s.Name = "HTTP POST api.example.com"
			s.Attrs = []telemetry.Attr{{Key: "http.url", Value: "https://api.example.com/v1/charges"}, {Key: "http.status_code", Value: "200"}}
		} else {
			s.Name = "db.query"
			s.Attrs = []telemetry.Attr{
				{Key: "db.query.text", Value: "SELECT id, name, status, total, created_at FROM orders WHERE tenant_id = ? AND status = ? ORDER BY created_at DESC LIMIT ?"},
				{Key: "db.rows", Value: fmt.Sprint(g.rng.IntN(200))},
			}
		}
		tr.Spans = append(tr.Spans, s)
	}
	return tr, nil
}
