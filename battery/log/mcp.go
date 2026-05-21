package log

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
		{
			name:        "log_set_level",
			description: "Change the minimum log level emitted by the fan-out handler. Useful for temporarily switching to DEBUG during an investigation, then restoring INFO. Returns the previous level.",
			schema: map[string]any{
				"type":     "object",
				"required": []string{"level"},
				"properties": map[string]any{
					"level": map[string]any{"type": "string", "enum": []string{"DEBUG", "INFO", "WARN", "ERROR"}, "description": "New minimum level."},
				},
			},
			handler: p.toolSetLevel,
		},
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
	limit := intParam(params, "limit", 50)
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
	limit := intParam(params, "limit", 100)
	minLevel, hasLevel := levelParam(params, "level")
	msgFilter := strParam(params, "msg", "")
	pathFilter := strParam(params, "path", "")
	requestID := strParam(params, "request_id", "")
	sinceFilter := strParam(params, "since_ts", "")
	untilFilter := strParam(params, "until_ts", "")
	historical := boolParam(params, "historical", false)

	pool := p.ring.SnapshotDecoded()
	if historical && p.resolvedFilePath != "" {
		// Combine: tail the file for older entries that may have been
		// evicted from the ring. We don't dedup — the agent gets back
		// a chronological stream and can filter further if needed.
		extra, err := tailFile(p.resolvedFilePath, limit*4)
		if err == nil && len(extra) > 0 {
			pool = append(extra, pool...)
		}
	}

	out := make([]map[string]any, 0, limit)
	for _, e := range pool {
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
		if sinceFilter != "" {
			ts, _ := e["time"].(string)
			if ts < sinceFilter { // RFC3339 is lexicographically comparable
				continue
			}
		}
		if untilFilter != "" {
			ts, _ := e["time"].(string)
			if ts > untilFilter {
				continue
			}
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return map[string]any{
		"entries":      out,
		"count":        len(out),
		"ring_size":    p.ring.cap,
		"file_tailed":  historical && p.resolvedFilePath != "",
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
