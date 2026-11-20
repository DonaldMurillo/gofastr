package framework

// The GoFastr workload builders shared by benchmark tiers 5 and 7. Tier 5
// measures these workloads for TechEmpower comparison; tier 7 re-runs the
// same workloads beside a hand-rolled net/http baseline so the delta between
// the two is what the framework's declare-once surface costs. They live in
// one place so the tiers cannot drift apart.

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

// benchGoFastrPlaintext serves "Hello, World!" from a bare app: no DB, no
// JSON, no entity machinery. Throughput here is the framework's per-request
// ceiling.
func benchGoFastrPlaintext(b *testing.B) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Router().GetFunc("/plaintext", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("Hello, World!"))
	})

	req := benchRequest(b, http.MethodGet, "/plaintext", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
	}
}

// benchGoFastrJSON encodes a small message object and writes it.
func benchGoFastrJSON(b *testing.B) {
	app := NewApp(WithoutDefaultMiddleware())
	type message struct {
		Message string `json:"message"`
	}
	app.Router().GetFunc("/json", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(message{Message: "Hello, World!"})
	})

	req := benchRequest(b, http.MethodGet, "/json", nil)
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, req)
	}
}

// benchGoFastrSingleQuery hits GET /worlds/{id} for a random id, mirroring
// TechEmpower's "Test 2: Single Query" through the framework's generated
// CRUD routes.
func benchGoFastrSingleQuery(b *testing.B) {
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
		benchMountCrud(b, app, worlds, "/worlds")

		rng := rand.New(rand.NewSource(42))
		b.ResetTimer()
		b.ReportAllocs()
		for b.Loop() {
			id := fmt.Sprintf("w%d", rng.Intn(N))
			req := benchAuthed(b, http.MethodGet, "/worlds/"+id, nil)
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				b.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
		}
	})
}
