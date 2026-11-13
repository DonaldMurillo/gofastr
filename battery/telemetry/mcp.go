package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/dev"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

// Most a tool returns in one call, whatever limit asks for.
const (
	mcpMaxRequests = 200
	mcpMaxLogs     = 500
	mcpLogBudget   = 10 * time.Second
)

type mcpTool struct {
	name, description string
	schema            map[string]any
	handler           func(ctx context.Context, params map[string]any) (any, error)
}

// registerMCPTools adds telemetry_stats, telemetry_issues, telemetry_requests,
// telemetry_request and telemetry_logs to the app's MCP server when Config.EnableMCP is set,
// or under `gofastr dev`. Each hands out request paths, client
// addresses and log lines, so each runs behind Config.MCPGate, the
// admin role by default. Under the dev loop there is no auth to
// satisfy: the tools are ungated and marked dev-implied, so the
// framework withdraws them when the listener is not loopback.
func (p *Plugin) registerMCPTools(app *framework.App) error {
	devImplied := !p.cfg.EnableMCP && dev.DevMCPEnabled()
	if !p.cfg.EnableMCP && !devImplied {
		return nil
	}
	gate := p.cfg.MCPGate
	if gate == nil {
		gate = framework.MCPRequireRole("admin")
	}
	ts := map[string]any{"type": "string", "description": "RFC 3339 timestamp."}
	tools := []mcpTool{
		{
			name:        "telemetry_requests",
			description: "List kept requests (traces), newest first: every failed and slow one and a sample of the rest. Filters: `failed`, `slow`, `route` (exact, e.g. \"GET /orders/{id}\"), `since_ts`/`until_ts`, `limit` (default 50, max 200). Each has a trace_id for telemetry_request.",
			schema: map[string]any{"type": "object", "properties": map[string]any{
				"failed":   map[string]any{"type": "boolean", "description": "Only requests that failed."},
				"slow":     map[string]any{"type": "boolean", "description": "Only requests at or over the slow threshold."},
				"route":    map[string]any{"type": "string", "description": "Exact route."},
				"since_ts": ts, "until_ts": ts,
				"limit": map[string]any{"type": "integer", "minimum": 1},
			}},
			handler: p.toolRequests,
		},
		{
			name:        "telemetry_issues",
			description: "Group the failed requests of a time range by what went wrong (route, status and error with its numbers and IDs set aside), latest first: count, first and last seen, an example trace_id. Filters: `since_ts`/`until_ts`, `limit` (default 50, max 200). The first tool to call when asked what is breaking.",
			schema: map[string]any{"type": "object", "properties": map[string]any{
				"since_ts": ts, "until_ts": ts,
				"limit": map[string]any{"type": "integer", "minimum": 1},
			}},
			handler: p.toolIssues,
		},
		{
			name:        "telemetry_stats",
			description: "Count every request of a time range, not only the kept ones: requests, failed, slow, and the durations half and 95% of them finished within (microseconds, each right to within a bucket), in total, by route (busiest first) and as a series of up to 60 steps. Filters: `since_ts` (default 24 hours ago), `until_ts` (default now), `route` (exact), `limit` on routes (default 50, max 200). `jobs` is background work (queue jobs, scheduled runs) by name with `runs`, `failed` and the same durations; it is not in the totals. `machine` is the latest reading of CPU (share of all cores, 0 to 1), memory, disk and this process, in bytes, with the peaks of the range. Counts are written each minute, so the last minute or so is missing. The tool to call for how busy, how failing and how slow the app is.",
			schema: map[string]any{"type": "object", "properties": map[string]any{
				"route":    map[string]any{"type": "string", "description": "Exact route."},
				"since_ts": ts, "until_ts": ts,
				"limit": map[string]any{"type": "integer", "minimum": 1},
			}},
			handler: p.toolStats,
		},
		{
			name:        "telemetry_request",
			description: "Return one request by `trace_id` (32 hex digits): its spans with start offsets and durations in microseconds, `err` (what went wrong) and `stack` (where, for a panic) when it failed, and the log lines written while it ran.",
			schema: map[string]any{"type": "object", "required": []string{"trace_id"}, "properties": map[string]any{
				"trace_id": map[string]any{"type": "string", "description": "The trace ID, from telemetry_requests or a log line's trace_id."},
			}},
			handler: p.toolRequest,
		},
		{
			name:        "telemetry_logs",
			description: "List log lines from the day files, newest first. Filters: `level` (minimum: DEBUG/INFO/WARN/ERROR), `text` (case-sensitive substring of the line), `since_ts`/`until_ts`, `limit` (default 100, max 500). A search that runs past ten seconds returns what it found with `stopped_at`: pass that as `until_ts` to carry on.",
			schema: map[string]any{"type": "object", "properties": map[string]any{
				"level":    map[string]any{"type": "string", "enum": []string{"DEBUG", "INFO", "WARN", "ERROR"}},
				"text":     map[string]any{"type": "string"},
				"since_ts": ts, "until_ts": ts,
				"limit": map[string]any{"type": "integer", "minimum": 1},
			}},
			handler: p.toolLogs,
		},
	}
	for _, t := range tools {
		opts := []mcp.ToolOption{mcp.WithToolGate(gate)}
		if devImplied {
			opts = []mcp.ToolOption{mcp.WithDevImplied()}
		}
		if err := app.MCP.RegisterTool(t.name, t.description, t.schema, t.handler, opts...); err != nil {
			return fmt.Errorf("telemetry: register %s: %w", t.name, err)
		}
	}
	return nil
}

// errBadParam is what a tool returns for an argument of the wrong type
// or shape: a filter that cannot be read is refused, never ignored.
var errBadParam = errors.New("telemetry: bad argument")

func strArg(params map[string]any, name string) (string, error) {
	v, ok := params[name]
	if !ok || v == nil {
		return "", nil
	}
	s, ok := v.(string)
	if !ok || len(s) > 1024 {
		return "", fmt.Errorf("%w: %s must be a string of at most 1024 bytes", errBadParam, name)
	}
	return s, nil
}

func boolArg(params map[string]any, name string) (bool, error) {
	v, ok := params[name]
	if !ok || v == nil {
		return false, nil
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%w: %s must be true or false", errBadParam, name)
	}
	return b, nil
}

func timeArg(params map[string]any, name string) (time.Time, error) {
	s, err := strArg(params, name)
	if err != nil || s == "" {
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %s must be an RFC 3339 timestamp", errBadParam, name)
	}
	return t, nil
}

// limitArg reads limit, a whole number of at least 1, capped at most.
func limitArg(params map[string]any, def, most int) (int, error) {
	v, ok := params["limit"]
	if !ok || v == nil {
		return def, nil
	}
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case int:
		n = float64(x)
	case int64:
		n = float64(x)
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return 0, fmt.Errorf("%w: limit must be a whole number", errBadParam)
		}
		n = f
	default:
		return 0, fmt.Errorf("%w: limit must be a whole number", errBadParam)
	}
	if math.IsNaN(n) || n < 1 || n != math.Trunc(n) {
		return 0, fmt.Errorf("%w: limit must be a whole number of at least 1", errBadParam)
	}
	return int(math.Min(n, float64(most))), nil
}

func (p *Plugin) reader() (*itel.Store, error) {
	if s := p.store.Load(); s != nil {
		return s, nil
	}
	return nil, errors.New("telemetry: not started")
}

func (p *Plugin) toolRequests(_ context.Context, params map[string]any) (any, error) {
	store, err := p.reader()
	if err != nil {
		return nil, err
	}
	var q itel.TraceQuery
	var slow bool
	if q.Failed, err = boolArg(params, "failed"); err != nil {
		return nil, err
	}
	if slow, err = boolArg(params, "slow"); err != nil {
		return nil, err
	}
	if slow {
		q.MinDur = p.SlowThreshold()
	}
	if q.Route, err = strArg(params, "route"); err != nil {
		return nil, err
	}
	if q.From, err = timeArg(params, "since_ts"); err != nil {
		return nil, err
	}
	if q.To, err = timeArg(params, "until_ts"); err != nil {
		return nil, err
	}
	limit, err := limitArg(params, 50, mcpMaxRequests)
	if err != nil {
		return nil, err
	}
	sums, err := store.Traces(q, limit)
	if err != nil {
		return nil, errors.New("telemetry: the trace files could not be read")
	}
	out := make([]map[string]any, len(sums))
	for i, s := range sums {
		out[i] = map[string]any{
			"time": s.Time.UTC().Format(time.RFC3339Nano), "trace_id": s.TraceID, "route": s.Route, "path": s.Path,
			"status": s.Status, "dur_us": s.Dur.Microseconds(), "error": s.Error, "failed_steps": s.FailedSteps,
		}
	}
	return map[string]any{"requests": out, "count": len(out)}, nil
}

func (p *Plugin) toolIssues(_ context.Context, params map[string]any) (any, error) {
	store, err := p.reader()
	if err != nil {
		return nil, err
	}
	from, err := timeArg(params, "since_ts")
	if err != nil {
		return nil, err
	}
	to, err := timeArg(params, "until_ts")
	if err != nil {
		return nil, err
	}
	limit, err := limitArg(params, 50, mcpMaxRequests)
	if err != nil {
		return nil, err
	}
	issues, leftOut, err := store.Issues(from, to, limit)
	if err != nil {
		return nil, errors.New("telemetry: the trace files could not be read")
	}
	out := make([]map[string]any, len(issues))
	for i, is := range issues {
		out[i] = map[string]any{
			"id": is.ID, "route": is.Route, "status": is.Status, "error": is.Err, "example": is.Example, "count": is.Count,
			"first": is.First.UTC().Format(time.RFC3339Nano), "last": is.Last.UTC().Format(time.RFC3339Nano), "last_trace_id": is.LastTraceID,
		}
	}
	firing := []string{}
	for _, a := range p.Firing() {
		firing = append(firing, a.Text())
	}
	return map[string]any{"issues": out, "count": len(out), "failures_not_listed": leftOut, "alerts_firing": firing}, nil
}

// statsSteps is the most points telemetry_stats returns as a series.
const statsSteps = 60

func (p *Plugin) toolStats(_ context.Context, params map[string]any) (any, error) {
	store, err := p.reader()
	if err != nil {
		return nil, err
	}
	from, err := timeArg(params, "since_ts")
	if err != nil {
		return nil, err
	}
	to, err := timeArg(params, "until_ts")
	if err != nil {
		return nil, err
	}
	route, err := strArg(params, "route")
	if err != nil {
		return nil, err
	}
	limit, err := limitArg(params, 50, mcpMaxRequests)
	if err != nil {
		return nil, err
	}
	if to.IsZero() {
		to = time.Now()
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	if !to.After(from) || to.Sub(from) > 366*24*time.Hour {
		return nil, fmt.Errorf("%w: since_ts must be before until_ts, and within a year of it", errBadParam)
	}
	// Whole minutes, and few enough steps to read.
	step := max((to.Sub(from)/statsSteps + time.Minute - 1).Truncate(time.Minute), time.Minute)
	got, err := store.Stats(itel.StatsQuery{From: from, To: to, Step: step, Route: route})
	if err != nil {
		return nil, errors.New("telemetry: the request counts could not be read")
	}
	count := func(c itel.StatsCount) map[string]any {
		return map[string]any{"requests": c.N, "failed": c.Failed, "slow": c.Slow,
			"p50_us": c.P50.Microseconds(), "p95_us": c.P95.Microseconds(), "mean_us": c.Mean.Microseconds()}
	}
	routes := make([]map[string]any, 0, min(len(got.Routes), limit))
	for _, r := range got.Routes[:min(len(got.Routes), limit)] {
		m := count(r.StatsCount)
		m["route"] = r.Route
		routes = append(routes, m)
	}
	// Background work by name: its "requests" are runs, and "slow" is
	// always zero, since nobody waits on it.
	jobs := make([]map[string]any, 0, min(len(got.Jobs), limit))
	for _, j := range got.Jobs[:min(len(got.Jobs), limit)] {
		m := count(j.StatsCount)
		m["name"] = j.Route
		delete(m, "slow")
		m["runs"] = m["requests"]
		delete(m, "requests")
		jobs = append(jobs, m)
	}
	series := make([]map[string]any, len(got.Points))
	for i, pt := range got.Points {
		series[i] = map[string]any{"time": pt.Time.UTC().Format(time.RFC3339), "requests": pt.N, "failed": pt.Failed, "slow": pt.Slow, "p95_us": pt.P95.Microseconds()}
	}
	// The machine as last read, and the most its CPU and memory reached
	// in the window: nil where the platform does not say.
	var machine map[string]any
	if host, err := store.Host(from, to, step); err == nil && !host.Latest.Time.IsZero() {
		l := host.Latest
		machine = map[string]any{"time": l.Time.UTC().Format(time.RFC3339),
			"mem_used": l.MemUsed, "mem_total": l.MemTotal, "disk_used": l.DiskUsed, "disk_total": l.DiskTotal,
			"disk_percent": l.DiskPercent(), "goroutines": l.Goroutines, "heap": l.HeapBytes, "rss": l.RSSBytes}
		if l.CPU >= 0 {
			machine["cpu"] = l.CPU
		}
		peakCPU, peakMem := -1.0, int64(0)
		for _, pt := range host.Points {
			peakCPU, peakMem = max(peakCPU, pt.CPUMax), max(peakMem, pt.MemUsed)
		}
		if peakCPU >= 0 {
			machine["cpu_peak"] = peakCPU
		}
		machine["mem_used_peak"] = peakMem
	}
	return map[string]any{
		"machine": machine,
		"jobs":    jobs, "jobs_not_listed": len(got.Jobs) - len(jobs),
		"total": count(got.Total), "routes": routes, "routes_not_listed": len(got.Routes) - len(routes),
		"series": series, "step_seconds": int(step / time.Second),
		"slow_at_us": p.SlowThreshold().Microseconds(), "duration_ceiling_us": itel.StatsCeiling.Microseconds(),
	}, nil
}

func (p *Plugin) toolRequest(_ context.Context, params map[string]any) (any, error) {
	store, err := p.reader()
	if err != nil {
		return nil, err
	}
	id, err := strArg(params, "trace_id")
	if err != nil {
		return nil, err
	}
	line, ok, err := store.TraceByID(id)
	switch {
	case errors.Is(err, itel.ErrBadTraceID):
		return nil, fmt.Errorf("%w: trace_id must be 32 hex digits", errBadParam)
	case err != nil:
		return nil, errors.New("telemetry: the trace files could not be read")
	case !ok:
		return map[string]any{"found": false}, nil
	}
	tr, err := itel.ParseTrace(line)
	if err != nil {
		return nil, errors.New("telemetry: the trace line could not be read")
	}
	logs, err := store.LogsForTrace(id, tr.Time.Add(-tr.Dur-time.Second), tr.Time.Add(time.Second))
	if err != nil {
		return nil, errors.New("telemetry: the log files could not be read")
	}
	return map[string]any{"found": true, "request": json.RawMessage(line), "logs": rawLines(logs)}, nil
}

func (p *Plugin) toolLogs(_ context.Context, params map[string]any) (any, error) {
	store, err := p.reader()
	if err != nil {
		return nil, err
	}
	q := itel.LogQuery{MinLevel: slog.LevelDebug - 100}
	level, err := strArg(params, "level")
	if err != nil {
		return nil, err
	}
	if level != "" {
		if err := q.MinLevel.UnmarshalText([]byte(level)); err != nil {
			return nil, fmt.Errorf("%w: level must be DEBUG, INFO, WARN or ERROR", errBadParam)
		}
	}
	if q.Text, err = strArg(params, "text"); err != nil {
		return nil, err
	}
	if q.From, err = timeArg(params, "since_ts"); err != nil {
		return nil, err
	}
	if q.To, err = timeArg(params, "until_ts"); err != nil {
		return nil, err
	}
	limit, err := limitArg(params, 100, mcpMaxLogs)
	if err != nil {
		return nil, err
	}
	// A rare word means reading every kept line; an agent gets what ten
	// seconds found and where the search stopped.
	lines, stopped, err := store.LogsWithin(q, limit, mcpLogBudget)
	if err != nil {
		return nil, errors.New("telemetry: the log files could not be read")
	}
	out := map[string]any{"entries": rawLines(lines), "count": len(lines)}
	if !stopped.IsZero() {
		out["stopped_at"] = stopped.UTC().Format(time.RFC3339Nano)
		out["note"] = "The search ran out of time at stopped_at. Call again with until_ts set to it to read older lines."
	}
	return out, nil
}

// rawLines hands log lines back as the JSON objects they are. A line
// that is not valid JSON goes as a string, so the answer always encodes.
func rawLines(lines []itel.LogEntry) []any {
	out := make([]any, len(lines))
	for i, l := range lines {
		if json.Valid(l.Line) {
			out[i] = l.Line
		} else {
			out[i] = string(l.Line)
		}
	}
	return out
}
