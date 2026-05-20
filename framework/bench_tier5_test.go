package framework

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// ============================================================================
// Tier 5 — TechEmpower-style endpoints
//
// The six canonical comparable workloads everyone publishes numbers for.
// Numbers here can be cross-referenced with the TechEmpower Framework
// Benchmarks (techempower.com/benchmarks) to know where GoFastr sits next
// to Gin/Echo/Fiber/Actix/etc.
//
// Reported as a single ns/op; for throughput compare 1 / (ns/op * 1e-9) =
// requests/sec. Allocations are reported to expose hot-path GC pressure.
// ============================================================================

// ----------------------------------------------------------------------------
// 5.1 — Plaintext
// "Return Hello, World!" — measures raw routing + handler dispatch cost.
// ----------------------------------------------------------------------------

// BenchmarkT5_Plaintext is the floor: no DB, no JSON, no entity machinery.
// Throughput here is the framework's per-request ceiling.
func BenchmarkT5_Plaintext(b *testing.B) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Router().GetFunc("/plaintext", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("Hello, World!"))
	})

	req := httptest.NewRequest(http.MethodGet, "/plaintext", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
	}
}

// BenchmarkT5_Plaintext_WithDefaults runs the same workload through the
// default middleware chain. The delta between this and the bare variant is
// the per-request cost users actually pay in production.
func BenchmarkT5_Plaintext_WithDefaults(b *testing.B) {
	app := NewApp()
	app.Router().GetFunc("/plaintext", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("Hello, World!"))
	})
	req := httptest.NewRequest(http.MethodGet, "/plaintext", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
	}
}

// ----------------------------------------------------------------------------
// 5.2 — JSON serialization
// "Return {"message":"Hello, World!"}" — measures encode + write overhead.
// ----------------------------------------------------------------------------

// BenchmarkT5_JSON encodes a small message object and writes it.
func BenchmarkT5_JSON(b *testing.B) {
	app := NewApp(WithoutDefaultMiddleware())
	type message struct {
		Message string `json:"message"`
	}
	app.Router().GetFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(message{Message: "Hello, World!"})
	})

	req := httptest.NewRequest(http.MethodGet, "/json", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
	}
}

// ----------------------------------------------------------------------------
// 5.3 — Single query
// "SELECT one random row by primary key" — measures DB round-trip + decode.
// ----------------------------------------------------------------------------

// BenchmarkT5_SingleQuery hits GET /worlds/{id} for a random id. Mirrors
// TechEmpower's "Test 2: Single Query".
func BenchmarkT5_SingleQuery(b *testing.B) {
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
		RegisterCrudRoutes(app.Router(), NewCrudHandler(worlds, db), "/worlds")

		rng := rand.New(rand.NewSource(42))
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			id := fmt.Sprintf("w%d", rng.Intn(N))
			req := httptest.NewRequest(http.MethodGet, "/worlds/"+id, nil)
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				b.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		}
	})
}

// ----------------------------------------------------------------------------
// 5.4 — Multiple queries
// "Fetch N random rows" — measures N round-trips vs amortised overhead.
// ----------------------------------------------------------------------------

// BenchmarkT5_MultiQuery fetches N random worlds per request. TechEmpower
// runs this at N=1, 5, 10, 15, 20 to expose the slope.
func BenchmarkT5_MultiQuery(b *testing.B) {
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
		RegisterCrudRoutes(app.Router(), NewCrudHandler(worlds, db), "/worlds")

		for _, queries := range []int{1, 5, 10, 20} {
			queries := queries
			b.Run(fmt.Sprintf("queries=%d", queries), func(b *testing.B) {
				rng := rand.New(rand.NewSource(42))
				b.ResetTimer()
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					for q := 0; q < queries; q++ {
						id := fmt.Sprintf("w%d", rng.Intn(N))
						req := httptest.NewRequest(http.MethodGet, "/worlds/"+id, nil)
						rec := httptest.NewRecorder()
						app.Router().ServeHTTP(rec, req)
						if rec.Code != http.StatusOK {
							b.Fatalf("status %d", rec.Code)
						}
					}
				}
			})
		}
	})
}

// ----------------------------------------------------------------------------
// 5.5 — Fortunes equivalent
// TechEmpower's Fortunes test: fetch all rows from a small table, add a
// row in-memory, sort, render HTML. We don't have a templating layer in
// the API surface, so this measures the equivalent JSON-list workload
// against a small table — comparable to "Test 4: Fortunes" minus the HTML.
// ----------------------------------------------------------------------------

// BenchmarkT5_FortunesLike fetches a small full table as JSON. Closest
// API-side analogue to TechEmpower's Fortunes that doesn't require a
// templating layer.
func BenchmarkT5_FortunesLike(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		const N = 12 // Fortunes ships with 12 rows
		seedFortunesTable(b, db, N)

		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		fortunes := Define("fortunes", EntityConfig{
			Table: "fortunes",
			Fields: []schema.Field{
				{Name: "message", Type: schema.Text, Required: true},
			},
		}.WithTimestamps(false))
		app.Registry.Register(fortunes)
		RegisterCrudRoutes(app.Router(), NewCrudHandler(fortunes, db), "/fortunes")

		req := httptest.NewRequest(http.MethodGet, "/fortunes?limit=100", nil)
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				b.Fatalf("status %d", rec.Code)
			}
		}
	})
}

// ----------------------------------------------------------------------------
// 5.6 — Data updates
// "Fetch N rows, increment random_number, write back" — measures the
// mixed read/write path under realistic ratios.
// ----------------------------------------------------------------------------

// BenchmarkT5_Updates emulates TechEmpower's "Test 5: Data Updates". For
// each iteration: GET N random rows, mutate random_number, PUT each back.
func BenchmarkT5_Updates(b *testing.B) {
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
		RegisterCrudRoutes(app.Router(), NewCrudHandler(worlds, db), "/worlds")

		for _, updates := range []int{1, 5, 10, 20} {
			updates := updates
			b.Run(fmt.Sprintf("updates=%d", updates), func(b *testing.B) {
				rng := rand.New(rand.NewSource(42))
				b.ResetTimer()
				b.ReportAllocs()
				for i := 0; i < b.N; i++ {
					for u := 0; u < updates; u++ {
						id := fmt.Sprintf("w%d", rng.Intn(N))
						// GET
						req := httptest.NewRequest(http.MethodGet, "/worlds/"+id, nil)
						rec := httptest.NewRecorder()
						app.Router().ServeHTTP(rec, req)
						if rec.Code != http.StatusOK {
							b.Fatalf("get status %d", rec.Code)
						}
						// PUT with the new value (we don't need to read the response
						// for TechEmpower's check, just to model the network shape).
						body, _ := json.Marshal(map[string]any{"random_number": rng.Intn(10000)})
						req = httptest.NewRequest(http.MethodPut, "/worlds/"+id, bytesReader(body))
						req.Header.Set("Content-Type", "application/json")
						rec = httptest.NewRecorder()
						app.Router().ServeHTTP(rec, req)
						if rec.Code >= 400 {
							b.Fatalf("put status %d: %s", rec.Code, rec.Body.String())
						}
					}
				}
			})
		}
	})
}

// ----------------------------------------------------------------------------
// Seeders
// ----------------------------------------------------------------------------

func seedWorldsTable(b *testing.B, db *sql.DB, n int) {
	b.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS worlds (
		id TEXT PRIMARY KEY,
		random_number INTEGER NOT NULL
	)`); err != nil {
		b.Fatalf("ddl worlds: %v", err)
	}
	tx, err := db.Begin()
	if err != nil {
		b.Fatalf("begin: %v", err)
	}
	for i := 0; i < n; i++ {
		if _, err := tx.Exec("INSERT INTO worlds (id, random_number) VALUES ($1, $2)",
			fmt.Sprintf("w%d", i), i*7%10000); err != nil {
			_ = tx.Rollback()
			b.Fatalf("seed world %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		b.Fatalf("commit: %v", err)
	}
}

func seedFortunesTable(b *testing.B, db *sql.DB, n int) {
	b.Helper()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS fortunes (
		id      TEXT PRIMARY KEY,
		message TEXT NOT NULL
	)`); err != nil {
		b.Fatalf("ddl fortunes: %v", err)
	}
	msgs := []string{
		"fortune: no such file or directory",
		"A computer scientist is someone who fixes things that aren't broken.",
		"After enough decimal places, nobody gives a damn.",
		"A bad random number generator: 1, 1, 1, 1, 1, 4.33e+67, 1, 1, 1",
		"A computer program does what you tell it to do, not what you want it to do.",
		"Emacs is a nice operating system, but I prefer UNIX. — Tom Christaensen",
		"Any program that runs right is obsolete.",
		"A list is only as strong as its weakest link. — Donald Knuth",
		"Feature: A bug with seniority.",
		"Computers make very fast, very accurate mistakes.",
		"<script>alert('XSS')</script>",
		"フレームワークのベンチマーク",
	}
	for i := 0; i < n; i++ {
		if _, err := db.Exec("INSERT INTO fortunes (id, message) VALUES ($1, $2)",
			fmt.Sprintf("f%d", i), msgs[i%len(msgs)]); err != nil {
			b.Fatalf("seed fortune %d: %v", i, err)
		}
	}
}
