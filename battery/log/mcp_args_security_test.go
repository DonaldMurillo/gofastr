package log

import (
	"context"
	"testing"
)

// Property family: MCP tool arguments are caller-supplied JSON values,
// so every log_* handler must survive any combination of wrongly-typed
// or out-of-range arguments without panicking, and a numeric overflow
// in `limit` must never allocate past the ring. intParam converts
// float64→int directly: a value beyond the int64 range is
// implementation-specific in Go (arm64 saturates, amd64 yields the
// minimum int), and the minimum int flows through clampLimit into
// make(..., cap) unchanged — on amd64 that is a handler panic, which
// the MCP layer recovers into an internal error for every caller.
func TestMCPArgsAdversarialNoPanic(t *testing.T) {
	_, p := newLogMCPApp(t)
	p.logger.Info("seed.entry", "path", "/x")

	shapes := []map[string]any{
		{"limit": 1e308},  // float64 → int overflow territory
		{"limit": 9.3e18}, // just past MaxInt64
		{"limit": "50"},   // string where integer declared
		{"limit": []any{1}},
		{"limit": -1},
		{"limit": 0.5},
		{"level": 7},
		{"level": []string{"ERROR"}},
		{"msg": 42},
		{"historical": "true"},
		{"request_id": false},
		{"since_ts": 1.7e9},
		{"until_ts": []string{"x"}},
	}
	tools := []struct {
		name string
		fn   func(context.Context, map[string]any) (any, error)
	}{
		{"log_recent", p.toolRecent},
		{"log_filter", p.toolFilter},
		{"log_set_level", p.toolSetLevel},
	}
	for _, tool := range tools {
		for _, params := range shapes {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("SECURITY: [crash] %s panicked on params %v: %v", tool.name, params, r)
					}
				}()
				_, _ = tool.fn(context.Background(), params)
			}()
		}
	}

	// The huge limit must also be bounded by the ring cap, not by the
	// caller's float.
	res, err := p.toolRecent(context.Background(), map[string]any{"limit": 1e308})
	if err == nil {
		entries, _ := res.(map[string]any)["entries"].([]map[string]any)
		if len(entries) > p.ring.Cap() {
			t.Errorf("SECURITY: [resource] limit 1e308 returned %d entries, ring cap is %d", len(entries), p.ring.Cap())
		}
	}
}

// Property: a present-but-malformed time filter errors instead of
// silently filtering nothing. timeParam's own contract says a
// present-but-malformed value must surface as an error "so the caller
// surfaces it back to the agent instead of silently filtering nothing";
// a JSON number in since_ts/until_ts is present and malformed, and
// treating it as absent makes the agent believe it narrowed the window
// while the response quietly contains everything.
func TestFilterTimestampTypeConfusionErr(t *testing.T) {
	_, p := newLogMCPApp(t)
	p.logger.Info("e1")

	wrongType := map[string]any{
		"since_ts": 1767225600,
		"until_ts": 1767225600.5,
	}
	for name, v := range wrongType {
		if _, err := p.toolFilter(context.Background(), map[string]any{name: v}); err == nil {
			t.Errorf("SECURITY: [validation] %s=%v (wrong JSON type) was silently ignored: the requested time filter was not applied", name, v)
		}
	}
	// Present-but-malformed strings already error; the control proves the
	// property is the error, not the type.
	if _, err := p.toolFilter(context.Background(), map[string]any{"since_ts": "yesterday"}); err == nil {
		t.Error("SECURITY: [validation] malformed since_ts string did not error")
	}
}
