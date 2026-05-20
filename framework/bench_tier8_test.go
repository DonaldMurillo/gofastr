package framework

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	_ "github.com/mattn/go-sqlite3"
)

// ============================================================================
// Tier 8 — operational (cold start, sustained memory)
//
// These benchmarks answer "how much does the framework cost just to exist"
// (cold start) and "how much memory does it hold under sustained load"
// (steady-state heap).
//
// Both are 'property' style — reported via custom metrics rather than the
// usual ns/op + B/op channels. They run with -benchtime=1x so the figures
// reported are per-iteration values, not means.
// ============================================================================

// ----------------------------------------------------------------------------
// 8.1 — Cold start: NewApp to first request served
// ----------------------------------------------------------------------------

// BenchmarkT8_ColdStart_Minimal times the smallest meaningful path: open
// SQLite, NewApp, register one entity, serve one request. Reported as
// ns/op (effectively "time per cold start").
func BenchmarkT8_ColdStart_Minimal(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		db.SetMaxOpenConns(1)
		if _, err := db.Exec(`CREATE TABLE worlds (id TEXT PRIMARY KEY, random_number INTEGER)`); err != nil {
			b.Fatalf("ddl: %v", err)
		}
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		worlds := Define("worlds", EntityConfig{
			Table:  "worlds",
			Fields: []schema.Field{{Name: "random_number", Type: schema.Int}},
		}.WithTimestamps(false))
		app.Registry.Register(worlds)
		RegisterCrudRoutes(app.Router(), NewCrudHandler(worlds, db), "/worlds")

		req := httptest.NewRequest(http.MethodGet, "/worlds", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("first request status %d", rec.Code)
		}
		_ = db.Close()
	}
}

// BenchmarkT8_ColdStart_TenEntities is the same path but with 10 entities
// registered. The slope between this and Minimal tells you how registration
// time scales with entity count.
func BenchmarkT8_ColdStart_TenEntities(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			b.Fatalf("open: %v", err)
		}
		db.SetMaxOpenConns(1)
		for j := 0; j < 10; j++ {
			if _, err := db.Exec(fmt.Sprintf(
				`CREATE TABLE ent_%d (id TEXT PRIMARY KEY, name TEXT, val INTEGER)`, j)); err != nil {
				b.Fatalf("ddl %d: %v", j, err)
			}
		}
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		for j := 0; j < 10; j++ {
			ent := Define(fmt.Sprintf("ent_%d", j), EntityConfig{
				Table: fmt.Sprintf("ent_%d", j),
				Fields: []schema.Field{
					{Name: "name", Type: schema.String},
					{Name: "val", Type: schema.Int},
				},
			}.WithTimestamps(false))
			app.Registry.Register(ent)
			RegisterCrudRoutes(app.Router(), NewCrudHandler(ent, db), fmt.Sprintf("/ent_%d", j))
		}

		req := httptest.NewRequest(http.MethodGet, "/ent_0", nil)
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			b.Fatalf("first request status %d", rec.Code)
		}
		_ = db.Close()
	}
}

// ----------------------------------------------------------------------------
// 8.2 — Sustained memory: heap after N requests
// ----------------------------------------------------------------------------

// BenchmarkT8_HeapAfterLoad reports the steady-state heap after a fixed
// workload. Run with -benchtime=1x — each iteration runs the full load
// loop and then reads runtime.MemStats. Reported via b.ReportMetric:
//
//   - heap_alloc_bytes   live heap after the load
//   - heap_inuse_bytes   bytes in in-use spans
//   - heap_objects       live object count
//   - mallocs            cumulative allocs over the load
//   - frees              cumulative frees over the load
//
// A regression here means we're leaking objects across requests or the
// per-request churn grew enough to retain more live heap.
func BenchmarkT8_HeapAfterLoad(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		// Setup once; benchmark loop just measures memory after the workload.
		app := setupBlogDomain(b, db, 200, 0)
		req := httptest.NewRequest(http.MethodGet, "/posts?limit=50", nil)
		// Drive the load.
		const reqs = 5000

		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for j := 0; j < reqs; j++ {
				rec := httptest.NewRecorder()
				app.Router().ServeHTTP(rec, req)
			}
		}
		b.StopTimer()

		runtime.GC()
		runtime.ReadMemStats(&after)

		b.ReportMetric(float64(after.HeapAlloc), "heap_alloc_bytes")
		b.ReportMetric(float64(after.HeapInuse), "heap_inuse_bytes")
		b.ReportMetric(float64(after.HeapObjects), "heap_objects")
		b.ReportMetric(float64(after.Mallocs-before.Mallocs), "mallocs")
		b.ReportMetric(float64(after.Frees-before.Frees), "frees")
		b.ReportMetric(float64(after.PauseTotalNs-before.PauseTotalNs)/1e6, "gc_pause_total_ms")
		b.ReportMetric(float64(after.NumGC-before.NumGC), "gc_cycles")
	})
}

// ----------------------------------------------------------------------------
// 8.3 — Goroutine count under sustained load
// ----------------------------------------------------------------------------

// BenchmarkT8_GoroutinesAfterLoad ensures a fixed workload doesn't leak
// goroutines. A regression where the count climbs across iterations is a
// resource leak.
func BenchmarkT8_GoroutinesAfterLoad(b *testing.B) {
	forEachBenchDialect(b, func(b *testing.B, db *sql.DB, _ Dialect) {
		app := setupBlogDomain(b, db, 200, 0)
		req := httptest.NewRequest(http.MethodGet, "/posts?limit=50", nil)
		const reqs = 2000

		before := runtime.NumGoroutine()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for j := 0; j < reqs; j++ {
				rec := httptest.NewRecorder()
				app.Router().ServeHTTP(rec, req)
			}
		}
		b.StopTimer()

		after := runtime.NumGoroutine()
		b.ReportMetric(float64(before), "gor_before")
		b.ReportMetric(float64(after), "gor_after")
		b.ReportMetric(float64(after-before), "gor_delta")
	})
}
