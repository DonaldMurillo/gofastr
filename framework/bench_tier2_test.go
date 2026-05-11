package framework

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofastr/gofastr/core/router"
)

// ============================================================================
// Tier 2 — hot-path microbenchmarks (no DB)
// ============================================================================

// BenchmarkMiddleware_DefaultChain measures the per-request overhead of the
// default middleware chain (Recovery → RequestID → Logging →
// SecurityHeaders → Timeout(30s)) against a noop handler.
//
// Compare "with" vs "without" to know how much the safety chain costs.
func BenchmarkMiddleware_DefaultChain(b *testing.B) {
	noop := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	b.Run("with-default-chain", func(b *testing.B) {
		app := NewApp()
		app.Router.GetFunc("/ping", noop)
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rec := httptest.NewRecorder()
			app.Router.ServeHTTP(rec, req)
		}
	})

	b.Run("without-default-chain", func(b *testing.B) {
		app := NewApp(WithoutDefaultMiddleware())
		app.Router.GetFunc("/ping", noop)
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rec := httptest.NewRecorder()
			app.Router.ServeHTTP(rec, req)
		}
	})

	b.Run("raw-router", func(b *testing.B) {
		r := router.New()
		r.GetFunc("/ping", noop)
		req := httptest.NewRequest(http.MethodGet, "/ping", nil)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
		}
	})
}

// BenchmarkJSONCasing measures the cost of camelCase ↔ snake_case conversion
// over a representative entity-shaped map. JSON casing runs once per request
// (twice for write paths) so it's a real per-request cost.
func BenchmarkJSONCasing(b *testing.B) {
	snakeRow := map[string]any{
		"id":          "p1",
		"title":       "Hello",
		"body":        "lorem ipsum",
		"status":      "published",
		"author_id":   "u1",
		"view_count":  42,
		"created_at":  "2026-01-01T00:00:00Z",
		"updated_at":  "2026-01-02T00:00:00Z",
		"is_archived": false,
		"tag_ids":     []string{"a", "b", "c"},
	}
	camelRow := map[string]any{
		"id":         "p1",
		"title":      "Hello",
		"body":       "lorem ipsum",
		"status":     "published",
		"authorId":   "u1",
		"viewCount":  42,
		"createdAt":  "2026-01-01T00:00:00Z",
		"updatedAt":  "2026-01-02T00:00:00Z",
		"isArchived": false,
		"tagIds":     []string{"a", "b", "c"},
	}

	b.Run("snake→camel", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = mapToCamelCase(snakeRow)
		}
	})

	b.Run("camel→snake", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = mapToSnakeCase(camelRow)
		}
	})

	b.Run("toCamelCase-single", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = toCamelCase("created_at")
		}
	})

	b.Run("toSnakeCase-single", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_ = toSnakeCase("createdAt")
		}
	})
}

// BenchmarkDSLParse measures the cost of parsing a representative DSL query
// string (the path an LLM-generated query travels every call).
func BenchmarkDSLParse(b *testing.B) {
	cases := map[string]string{
		"trivial":  `posts.limit(10)`,
		"filter":   `posts.where(status="published", views>=10).limit(20)`,
		"complex":  `posts.where(status="published", views>=10, title contains "go").include(author,comments).order(created_at DESC).limit(50)`,
		"in-list":  `posts.where(status in ["draft","published","archived"]).limit(100)`,
	}
	for name, q := range cases {
		q := q
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := ParseDSL(q); err != nil {
					b.Fatalf("parse: %v", err)
				}
			}
		})
	}
}
