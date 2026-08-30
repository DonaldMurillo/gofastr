package framework

import (
	"context"
	"encoding/json"
	"testing"
)

// maxDocsSearchResponseHits is the reply ceiling the framework_docs_search
// tool schema itself promises: "Max hits to return (default 50, hard cap to
// protect narrow-context clients)" (mcp_introspection.go, tool schema).
// Any server-side clamp at or below this value satisfies the pin.
const maxDocsSearchResponseHits = 200

// maxDocsSearchResponseBytes bounds the marshaled tool reply. maxDocsSearch
// ResponseHits hits with 240-char excerpts plus per-hit metadata stays
// comfortably under 256 KiB; an uncapped scan of the ~1.5 MB embedded corpus
// for a stopword term marshals to multiple megabytes.
const maxDocsSearchResponseBytes = 256 << 10

// TestDocsSearchToolLimitIsCapped pins the oversized-response protection of
// the framework_docs_search MCP tool: a caller-supplied numeric `limit` can
// never push the reply past the documented hard cap.
//
// The tool schema advertises a "hard cap to protect narrow-context clients"
// and docs.defaultSearchHitCap exists (per its doc comment) to keep "MCP /
// narrow-context clients safe from oversized responses". Yet toolDocsSearch
// (mcp_introspection.go:282-293) forwards the raw parameter into
// docs.SearchWithLimit, which honors any positive value (only limit <= 0
// falls back to the default). A ~40-byte request {"term":"the",
// "limit":100000000} therefore returns every matching line of the embedded
// corpus — 10k+ lines for a stopword — as one multi-megabyte JSON reply,
// repeatable per call. This pin extends the docs-search surface the existing
// coverage test (TestCovToolDocsSearchLimitTypes) exercises for type
// coercion but never for clamping.
func TestDocsSearchToolLimitIsCapped(t *testing.T) {
	app := NewApp(WithMCPIntrospection())
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	call := func(t *testing.T, params map[string]any) map[string]any {
		t.Helper()
		res, err := app.toolDocsSearch(context.Background(), params)
		if err != nil {
			t.Fatalf("toolDocsSearch(%v): %v", params, err)
		}
		m, ok := res.(map[string]any)
		if !ok {
			t.Fatalf("toolDocsSearch returned %T, want map[string]any", res)
		}
		return m
	}

	// The three branches of the handler's limit type switch must all clamp.
	// float64 is the realistic MCP wire form: JSON params decode to float64.
	for _, tc := range []struct {
		name  string
		limit any
	}{
		{"float64 limit 1e12", 1e12},
		{"int limit 1<<30", 1 << 30},
		{"int64 limit 1<<40", int64(1) << 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := call(t, map[string]any{"term": "the", "limit": tc.limit})
			count, ok := res["count"].(int)
			if !ok {
				t.Fatalf("count is %T, want int", res["count"])
			}
			if count > maxDocsSearchResponseHits {
				t.Fatalf("framework_docs_search returned %d hits for limit=%v; the tool schema promises a hard cap of at most %d hits to protect narrow-context clients (oversized-response protection bypass)",
					count, tc.limit, maxDocsSearchResponseHits)
			}
			b, err := json.Marshal(res)
			if err != nil {
				t.Fatalf("marshal reply: %v", err)
			}
			if len(b) > maxDocsSearchResponseBytes {
				t.Fatalf("framework_docs_search reply marshals to %d bytes for limit=%v; over the %d-byte reply budget",
					len(b), tc.limit, maxDocsSearchResponseBytes)
			}
		})
	}

	// The clamp must not swallow legitimate small limits.
	t.Run("small limit still honored", func(t *testing.T) {
		res := call(t, map[string]any{"term": "the", "limit": 5})
		if got := res["count"].(int); got != 5 {
			t.Fatalf("limit=5 returned %d hits, want exactly 5", got)
		}
	})

	// Omitted limit keeps the documented default-cap behavior (control).
	t.Run("omitted limit uses default cap", func(t *testing.T) {
		res := call(t, map[string]any{"term": "the"})
		if got := res["count"].(int); got > 50 {
			t.Fatalf("omitted limit returned %d hits, want <= 50 (defaultSearchHitCap)", got)
		}
	})
}
