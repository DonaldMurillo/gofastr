package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
)

// Pins the unbounded default idempotency store behind the recommended
// all-defaults construction, found by the 2026-09-04 red-probe round; fixed
// by bounding the default memory store (100_000 entries, idle-first
// eviction, 1 MiB per-entry byte cap) and surfacing the knobs on
// IdempotencyConfig so hosts can size the bounds themselves. The unit guards
// live in core/middleware (TestMemoryStore_DefaultEntriesBounded and
// friends); this end-to-end test pins that the knobs survive the
// WithIdempotency wiring and actually evict.
// Family: F1 resource exhaustion from request-borne input (attacker-mintable cache keys)
// Property: the store behind the recommended all-defaults construction is bounded, and the config knobs reach it.
// Surfaces: core/middleware/idempotency.go::NewMemoryIdempotencyStore (default caps),
//           core/middleware/idempotency.go::IdempotencyConfig (MaxStoreEntries / MaxStoreEntryBytes),
//           framework/app.go::WithIdempotency (knobs forwarded, Principal defaulted).

// TestIdempotencyDefaultStoreBounded: with MaxStoreEntries set through the
// app wiring, a flood of unique keys evicts the oldest cached response — the
// evicted key re-executes instead of replaying — while a still-resident key
// keeps replaying. Without the bound, every unique attacker-minted key would
// stay resident for the 24h TTL.
func TestIdempotencyDefaultStoreBounded(t *testing.T) {
	a := NewApp(WithIdempotency(middleware.IdempotencyConfig{MaxStoreEntries: 2}))
	var calls int32
	withTestRoute(a, &calls, http.StatusCreated, "ok")

	send := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/r", strings.NewReader(`{"q":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		rr := httptest.NewRecorder()
		a.Router().ServeHTTP(rr, req)
		return rr
	}

	// Four unique keys through a cap-2 store: k1 and k2 are evicted as
	// k3 and k4 claim their slots.
	for _, k := range []string{"k1", "k2", "k3", "k4"} {
		if rr := send(k); rr.Code != http.StatusCreated {
			t.Fatalf("post %s: %d", k, rr.Code)
		}
	}

	// k1 was evicted: replaying it must re-execute the handler, not serve
	// the stale cached body.
	if rr := send("k1"); rr.Header().Get("Idempotent-Replay") == "true" {
		t.Fatal("evicted key k1 replayed from cache: the entry-count bound did not evict it")
	}
	if calls != 5 {
		t.Fatalf("expected 5 handler invocations (4 floods + 1 re-execution of evicted k1), got %d", calls)
	}

	// k4 is still resident: it replays without re-executing, proving the
	// eviction is selective rather than the cache being broken.
	if rr := send("k4"); rr.Header().Get("Idempotent-Replay") != "true" {
		t.Fatal("resident key k4 did not replay: eviction dropped more than the idle entries")
	}
	if calls != 5 {
		t.Fatalf("resident key k4 re-executed (calls=%d): replay must not increment the handler", calls)
	}
}
