package framework

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofastr/gofastr/core/schema"
)

// ============================================================================
// Tier 7 — stdlib baseline comparisons
//
// Hand-rolled net/http + database/sql implementations sitting next to the
// framework-equivalent endpoints. The delta is what GoFastr's
// declare-once-get-many surface actually costs vs writing the same
// endpoint by hand.
//
// Honest: this is the "overhead tax". A useful framework is worth some
// of it; too much makes the abstraction not worth wearing.
// ============================================================================

// ----------------------------------------------------------------------------
// 7.1 — Plaintext
// ----------------------------------------------------------------------------

func BenchmarkT7_Plaintext_NetHTTP(b *testing.B) {
	mux := http.NewServeMux()
	mux.HandleFunc("/plaintext", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("Hello, World!"))
	})
	req := httptest.NewRequest(http.MethodGet, "/plaintext", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
	}
}

func BenchmarkT7_Plaintext_GoFastr(b *testing.B) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Router.GetFunc("/plaintext", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("Hello, World!"))
	})
	req := httptest.NewRequest(http.MethodGet, "/plaintext", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.Router.ServeHTTP(rec, req)
	}
}

// ----------------------------------------------------------------------------
// 7.2 — JSON serialization (small)
// ----------------------------------------------------------------------------

func BenchmarkT7_JSON_NetHTTP(b *testing.B) {
	mux := http.NewServeMux()
	type msg struct {
		Message string `json:"message"`
	}
	mux.HandleFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msg{Message: "Hello, World!"})
	})
	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
	}
}

func BenchmarkT7_JSON_GoFastr(b *testing.B) {
	app := NewApp(WithoutDefaultMiddleware())
	type msg struct {
		Message string `json:"message"`
	}
	app.Router.GetFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(msg{Message: "Hello, World!"})
	})
	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.Router.ServeHTTP(rec, req)
	}
}

// ----------------------------------------------------------------------------
// 7.3 — Single query
// ----------------------------------------------------------------------------

// BenchmarkT7_SingleQuery_NetHTTP is the textbook hand-rolled handler:
// open the request, parse the path, query the row, marshal, write. Zero
// abstraction overhead.
func BenchmarkT7_SingleQuery_NetHTTP(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		const N = 10000
		seedWorldsTable(b, db, N)

		mux := http.NewServeMux()
		mux.HandleFunc("/worlds/", func(w http.ResponseWriter, r *http.Request) {
			id := r.URL.Path[len("/worlds/"):]
			var randomNumber int
			err := db.QueryRowContext(r.Context(),
				"SELECT random_number FROM worlds WHERE id = $1", id).Scan(&randomNumber)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":            id,
				"randomNumber":  randomNumber,
			})
		})

		rng := rand.New(rand.NewSource(42))
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			id := fmt.Sprintf("w%d", rng.Intn(N))
			req := httptest.NewRequest(http.MethodGet, "/worlds/"+id, nil)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				b.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		}
	})
}

// BenchmarkT7_SingleQuery_GoFastr is the equivalent endpoint expressed
// through the framework: entity declaration, CRUD routes, the rest is
// generated. Numbers compare directly with the hand-rolled variant.
func BenchmarkT7_SingleQuery_GoFastr(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		const N = 10000
		seedWorldsTable(b, db, N)

		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		worlds := Define("worlds", EntityConfig{
			Table: "worlds",
			Fields: []schema.Field{
				{Name: "random_number", Type: schema.Int},
			},
		}.WithTimestamps(false))
		app.Registry.Register(worlds)
		RegisterCrudRoutes(app.Router, NewCrudHandler(worlds, db), "/worlds")

		rng := rand.New(rand.NewSource(42))
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			id := fmt.Sprintf("w%d", rng.Intn(N))
			req := httptest.NewRequest(http.MethodGet, "/worlds/"+id, nil)
			rec := httptest.NewRecorder()
			app.Router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				b.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		}
	})
}

// ----------------------------------------------------------------------------
// 7.4 — List with filter (the framework's bread and butter)
// ----------------------------------------------------------------------------

// BenchmarkT7_FilteredList_NetHTTP is a hand-rolled list endpoint that
// understands ?status= and ?limit=, queries the DB, and JSON-encodes.
func BenchmarkT7_FilteredList_NetHTTP(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		app := setupBlogDomain(b, db, 500, 0)
		_ = app // we use the same DB but our own router

		mux := http.NewServeMux()
		mux.HandleFunc("/posts", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			status := q.Get("status")
			limit := q.Get("limit")
			if limit == "" {
				limit = "20"
			}
			var (
				rows *sql.Rows
				err  error
			)
			if status != "" {
				rows, err = db.QueryContext(r.Context(),
					"SELECT id, title, body, status, author_id, views FROM posts WHERE status = $1 LIMIT "+limit,
					status)
			} else {
				rows, err = db.QueryContext(r.Context(),
					"SELECT id, title, body, status, author_id, views FROM posts LIMIT "+limit)
			}
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			defer rows.Close()

			var out []map[string]any
			for rows.Next() {
				var id, title, body, status, authorID string
				var views int
				if err := rows.Scan(&id, &title, &body, &status, &authorID, &views); err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				out = append(out, map[string]any{
					"id":       id,
					"title":    title,
					"body":     body,
					"status":   status,
					"authorId": authorID,
					"views":    views,
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": out})
		})

		req := httptest.NewRequest(http.MethodGet, "/posts?status=published&limit=50", nil)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				b.Fatalf("status %d", rec.Code)
			}
		}
	})
}

// BenchmarkT7_FilteredList_GoFastr is the same endpoint through the
// framework's auto-generated CRUD list.
func BenchmarkT7_FilteredList_GoFastr(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		app := setupBlogDomain(b, db, 500, 0)
		req := httptest.NewRequest(http.MethodGet, "/posts?status=published&limit=50", nil)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			rec := httptest.NewRecorder()
			app.Router.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				b.Fatalf("status %d", rec.Code)
			}
		}
	})
}
