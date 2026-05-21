package log

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// registerMCPTools wires the four log query/control tools onto the
// App's MCP server. Called from Init when Config.EnableMCP is set.
//
// Tool names use a `log_` prefix to namespace cleanly alongside other
// plugins that may register their own MCP surface.
func (p *Plugin) registerMCPTools(app *framework.App) error {
	tools := []struct {
		name        string
		description string
		schema      map[string]any
		handler     func(ctx context.Context, params map[string]any) (any, error)
	}{
		{
			name:        "log_recent",
			description: "Return the most recent log entries from the in-memory ring buffer. Optional `limit` (default 50, max ring size) and `level` (DEBUG/INFO/WARN/ERROR) filters.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "minimum": 1, "description": "Max entries to return; defaults to 50."},
					"level": map[string]any{"type": "string", "enum": []string{"DEBUG", "INFO", "WARN", "ERROR"}, "description": "Minimum level to include."},
				},
			},
			handler: p.toolRecent,
		},
		{
			name:        "log_filter",
			description: "Query log entries by structured field match. Filters: `msg` (substring), `path` (substring), `request_id` (exact), `since_ts` / `until_ts` (RFC3339 timestamps), `level`, `limit`. When `historical=true` and a file sink is configured, also reads the persistent log file for entries older than the ring window.",
			schema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"msg":        map[string]any{"type": "string", "description": "Substring match on the entry's `msg` field."},
					"path":       map[string]any{"type": "string", "description": "Substring match on `path` (http.access / http.panic entries)."},
					"request_id": map[string]any{"type": "string", "description": "Exact match on `request_id`."},
					"since_ts":   map[string]any{"type": "string", "description": "RFC3339 timestamp; only entries at or after this time."},
					"until_ts":   map[string]any{"type": "string", "description": "RFC3339 timestamp; only entries at or before this time."},
					"level":      map[string]any{"type": "string", "enum": []string{"DEBUG", "INFO", "WARN", "ERROR"}},
					"limit":      map[string]any{"type": "integer", "minimum": 1, "description": "Max entries to return; defaults to 100."},
					"historical": map[string]any{"type": "boolean", "description": "Include entries from the file sink (older than the ring window). Default false."},
				},
			},
			handler: p.toolFilter,
		},
		{
			name:        "log_metrics",
			description: "Return a snapshot of the log plugin's counters: PostStopDrops, SinkWriteFailures, WebhookDropped, WebhookGaveUp. Use to gauge whether the logging system itself is healthy.",
			schema:      map[string]any{"type": "object"},
			handler:     p.toolMetrics,
		},
	}
	if p.cfg.AllowMCPMutation {
		tools = append(tools, struct {
			name        string
			description string
			schema      map[string]any
			handler     func(ctx context.Context, params map[string]any) (any, error)
		}{
			name:        "log_set_level",
			description: "Change the minimum log level emitted by the fan-out handler. Useful for temporarily switching to DEBUG during an investigation, then restoring INFO. Returns the previous level. Only registered when log.Config.AllowMCPMutation is true.",
			schema: map[string]any{
				"type":     "object",
				"required": []string{"level"},
				"properties": map[string]any{
					"level": map[string]any{"type": "string", "enum": []string{"DEBUG", "INFO", "WARN", "ERROR"}, "description": "New minimum level."},
				},
			},
			handler: p.toolSetLevel,
		})
	}
	for _, t := range tools {
		if err := app.MCP.RegisterTool(t.name, t.description, t.schema, t.handler); err != nil {
			return fmt.Errorf("register %s: %w", t.name, err)
		}
	}
	return nil
}

// ---- tool handlers -------------------------------------------------------

func (p *Plugin) toolRecent(_ context.Context, params map[string]any) (any, error) {
	limit := p.clampLimit(intParam(params, "limit", 50))
	minLevel, hasLevel := levelParam(params, "level")

	all := p.ring.SnapshotDecoded()
	// Walk newest→oldest so the limit cap takes the most recent.
	out := make([]map[string]any, 0, limit)
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		e := all[i]
		if hasLevel && !levelAtLeast(e, minLevel) {
			continue
		}
		out = append(out, e)
	}
	// Reverse so the response is chronological (oldest first).
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return map[string]any{
		"entries": out,
		"count":   len(out),
	}, nil
}

func (p *Plugin) toolFilter(_ context.Context, params map[string]any) (any, error) {
	limit := p.clampLimit(intParam(params, "limit", 100))
	minLevel, hasLevel := levelParam(params, "level")
	msgFilter := strParam(params, "msg", "")
	pathFilter := strParam(params, "path", "")
	requestID := strParam(params, "request_id", "")
	historical := boolParam(params, "historical", false)

	since, hasSince, err := timeParam(params, "since_ts")
	if err != nil {
		return nil, err
	}
	until, hasUntil, err := timeParam(params, "until_ts")
	if err != nil {
		return nil, err
	}
	if hasSince && hasUntil && since.After(until) {
		return nil, fmt.Errorf("since_ts is after until_ts")
	}

	pool := p.ring.SnapshotDecoded()
	if historical && p.resolvedFilePath != "" {
		// Tail the file for older entries beyond the ring window. Dedup
		// against the ring on (time, msg, request_id) so the steady-state
		// case (file overlaps ring) doesn't double-count.
		extra, err := tailFile(p.resolvedFilePath, limit*4)
		if err == nil && len(extra) > 0 {
			seen := make(map[string]struct{}, len(pool))
			key := func(e map[string]any) string {
				t, _ := e["time"].(string)
				m, _ := e["msg"].(string)
				r, _ := e["request_id"].(string)
				return t + "\x00" + m + "\x00" + r
			}
			for _, e := range pool {
				seen[key(e)] = struct{}{}
			}
			deduped := make([]map[string]any, 0, len(extra))
			for _, e := range extra {
				if _, dup := seen[key(e)]; !dup {
					deduped = append(deduped, e)
				}
			}
			pool = append(deduped, pool...)
		}
	}

	// Walk newest→oldest so the limit cap takes the most recent matches
	// — matches log_recent's chronology contract.
	out := make([]map[string]any, 0, limit)
	for i := len(pool) - 1; i >= 0 && len(out) < limit; i-- {
		e := pool[i]
		if hasLevel && !levelAtLeast(e, minLevel) {
			continue
		}
		if msgFilter != "" {
			s, _ := e["msg"].(string)
			if !strings.Contains(s, msgFilter) {
				continue
			}
		}
		if pathFilter != "" {
			s, _ := e["path"].(string)
			if !strings.Contains(s, pathFilter) {
				continue
			}
		}
		if requestID != "" {
			s, _ := e["request_id"].(string)
			if s != requestID {
				continue
			}
		}
		if hasSince || hasUntil {
			ts, _ := e["time"].(string)
			t, err := time.Parse(time.RFC3339Nano, ts)
			if err != nil {
				t, err = time.Parse(time.RFC3339, ts)
			}
			if err != nil {
				continue
			}
			if hasSince && t.Before(since) {
				continue
			}
			if hasUntil && t.After(until) {
				continue
			}
		}
		out = append(out, e)
	}
	// Reverse for chronological response.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return map[string]any{
		"entries": out,
		"count":   len(out),
	}, nil
}

func (p *Plugin) toolMetrics(_ context.Context, _ map[string]any) (any, error) {
	m := p.Metrics()
	return map[string]any{
		"post_stop_drops":     m.PostStopDrops,
		"sink_write_failures": m.SinkWriteFailures,
		"webhook_dropped":     m.WebhookDropped,
		"webhook_gave_up":     m.WebhookGaveUp,
	}, nil
}

func (p *Plugin) toolSetLevel(_ context.Context, params map[string]any) (any, error) {
	level, ok := levelParam(params, "level")
	if !ok {
		return nil, fmt.Errorf("level is required (one of DEBUG/INFO/WARN/ERROR)")
	}
	prev := p.level.Level()
	p.level.Set(level)
	return map[string]any{
		"previous_level": prev.String(),
		"current_level":  level.String(),
	}, nil
}

// clampLimit caps `n` at the ring's capacity so an adversarial or typo'd
// `limit: 10_000_000` can't allocate a multi-million-slot slice. The
// ring is the source of truth for "how much history is available."
func (p *Plugin) clampLimit(n int) int {
	max := p.ring.Cap()
	if n > max {
		return max
	}
	return n
}

// ---- param helpers -------------------------------------------------------

func intParam(params map[string]any, name string, def int) int {
	switch v := params[name].(type) {
	case float64:
		if v > 0 {
			return int(v)
		}
	case int:
		if v > 0 {
			return v
		}
	}
	return def
}

func strParam(params map[string]any, name, def string) string {
	if v, ok := params[name].(string); ok {
		return v
	}
	return def
}

func boolParam(params map[string]any, name string, def bool) bool {
	if v, ok := params[name].(bool); ok {
		return v
	}
	return def
}

// timeParam parses an RFC3339 (with or without sub-second precision)
// timestamp from `params[name]`. Returns hasValue=false when the field
// is absent. Returns an error on a present-but-malformed value so the
// caller surfaces it back to the agent instead of silently filtering
// nothing.
func timeParam(params map[string]any, name string) (time.Time, bool, error) {
	s, ok := params[name].(string)
	if !ok || s == "" {
		return time.Time{}, false, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
	}
	if err != nil {
		return time.Time{}, false, fmt.Errorf("%s: %w", name, err)
	}
	return t, true, nil
}

func levelParam(params map[string]any, name string) (slog.Level, bool) {
	s, ok := params[name].(string)
	if !ok {
		return 0, false
	}
	switch strings.ToUpper(s) {
	case "DEBUG":
		return slog.LevelDebug, true
	case "INFO":
		return slog.LevelInfo, true
	case "WARN", "WARNING":
		return slog.LevelWarn, true
	case "ERROR":
		return slog.LevelError, true
	}
	return 0, false
}

// levelAtLeast returns true if the entry's `level` field is at or above
// min. Defensive against malformed entries (missing/non-string level).
func levelAtLeast(entry map[string]any, min slog.Level) bool {
	s, ok := entry["level"].(string)
	if !ok {
		return false
	}
	var lv slog.Level
	switch strings.ToUpper(s) {
	case "DEBUG":
		lv = slog.LevelDebug
	case "INFO":
		lv = slog.LevelInfo
	case "WARN":
		lv = slog.LevelWarn
	case "ERROR":
		lv = slog.LevelError
	default:
		return false
	}
	return lv >= min
}
