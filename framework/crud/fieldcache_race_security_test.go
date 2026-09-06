package crud

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// F28 time-of-use of configuration — pinned 2026-09-05 round 4.
// Family: F28 time-of-use of configuration
// Property: mutating an entity's field declarations while requests are in flight must be
// safe for concurrent readers — the handler's derived field caches are shared request-path
// state, so a reload must not race live requests.
// Surfaces: the derived field caches (visibleFields, jsonKeysFor, convertKey,
//   wireKeyColumn, unconvertMapKeys, the filter/sort field feeds, the scanners'
//   bool-column walk).
// Fix (this suite's green state): copy-on-write snapshots (fieldcache.go). The request
//   path loads an immutable snapshot built at construction; rebuilds happen off the
//   request path (VisibleFields re-checks the signature single-threaded and
//   republishes). No in-place map writes exist on the request path, so nothing can
//   crash the process or race under -race.
//
// One mutator goroutine, not four: four unsynchronized writers flipping the same
// Hidden byte race EACH OTHER (a harness bug in the original red probe, flagged by
// -race as test-vs-test before any handler frame appeared). The pinned property is
// mutation-during-serve; a single mutator exercises it without the self-race.

// TestFieldMutationDuringServeIsRaceFree serves list traffic while the
// entity's field declarations mutate (Hidden flag on an unrelated field).
// Run with -race.
func TestFieldMutationDuringServeIsRaceFree(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("race_notes", "race_notes", "user_id", []schema.Field{
		{Name: "user_id", Type: schema.String, Required: true},
		{Name: "title", Type: schema.String},
		{Name: "body", Type: schema.String},
	}), `CREATE TABLE race_notes (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, title TEXT, body TEXT)`)

	var stop atomic.Bool
	var wg sync.WaitGroup

	// Readers: concurrent list requests. ?fields=title stays a valid projection
	// for the whole run (the mutation below touches a different field), so any
	// non-200 is a request that observed a torn cache refresh.
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for !stop.Load() {
				req := makeRequest(t, RequestOpts{
					Method: http.MethodGet,
					Path:   "/race_notes?fields=title",
					UserID: "alice",
				})
				rr := httptest.NewRecorder()
				ch.List()(rr, req)
				if rr.Code != http.StatusOK {
					t.Errorf("SECURITY: [field-cache]: list returned %d (%s) while an unrelated field's declaration was being mutated — the request observed a torn cache refresh", rr.Code, rr.Body.String())
					return
				}
			}
		}()
	}

	// Mutator: the host reloads a field declaration (public Entity.Config
	// surface). Flipping "body" never makes ?fields=title invalid.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for !stop.Load() {
			ch.Entity.Config.Fields[3].Hidden = !ch.Entity.Config.Fields[3].Hidden
		}
	}()

	time.Sleep(1200 * time.Millisecond)
	stop.Store(true)
	wg.Wait()
}
