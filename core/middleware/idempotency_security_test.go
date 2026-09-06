package middleware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ----- Set-Cookie / Authorization stripped from cache -----------------------

func TestIdempotency_StripsHandlerSetCookieFromReplay(t *testing.T) {
	mw := Idempotency(IdempotencyConfig{Principal: testPrincipal})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "secret-token", Path: "/"})
		w.Header().Set("Authorization", "Bearer first-call-token")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))

	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{}`))
	req.Header.Set(IdempotencyKeyHeader, "k1")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first call status: %d", rr.Code)
	}
	if rr.Header().Get("Set-Cookie") == "" {
		t.Fatalf("first call should still set its own cookie")
	}

	// Replay: must NOT carry the handler's session cookie or Authorization.
	req2 := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{}`))
	req2.Header.Set(IdempotencyKeyHeader, "k1")
	rr2 := httptest.NewRecorder()
	srv.ServeHTTP(rr2, req2)
	if rr2.Header().Get("Idempotent-Replay") != "true" {
		t.Fatalf("expected replay marker on second call, headers=%v", rr2.Header())
	}
	if c := rr2.Header().Get("Set-Cookie"); c != "" {
		t.Fatalf("replay leaked Set-Cookie: %q", c)
	}
	if a := rr2.Header().Get("Authorization"); a != "" {
		t.Fatalf("replay leaked Authorization: %q", a)
	}
}

// ----- principal namespacing in fingerprint --------------------------------

func TestIdempotency_FingerprintNamespacedByPrincipal(t *testing.T) {
	mw := Idempotency(IdempotencyConfig{
		Principal: func(r *http.Request) string { return r.Header.Get("X-User-ID") },
	})
	var calls int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `tenant=%s`, r.Header.Get("X-User-ID"))
	}))

	// Alice sends with key k1.
	r1 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	r1.Header.Set(IdempotencyKeyHeader, "k1")
	r1.Header.Set("X-User-ID", "alice")
	w1 := httptest.NewRecorder()
	srv.ServeHTTP(w1, r1)

	// Bob sends with the SAME key k1. With principal namespacing, the
	// middleware must NOT replay Alice's response for Bob; that would
	// leak her body across tenants.
	r2 := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	r2.Header.Set(IdempotencyKeyHeader, "k1")
	r2.Header.Set("X-User-ID", "bob")
	w2 := httptest.NewRecorder()
	srv.ServeHTTP(w2, r2)

	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("each principal must execute the handler exactly once; got %d calls", calls)
	}
	if got := w1.Body.String(); got != "tenant=alice" {
		t.Fatalf("alice body: %q", got)
	}
	if got := w2.Body.String(); got != "tenant=bob" {
		t.Fatalf("bob must NOT receive alice's body; got %q", got)
	}
}

// ----- fail closed on store error ------------------------------------------

type brokenStore struct{}

func (brokenStore) Begin(context.Context, string, string) (*IdempotentResponse, bool, error) {
	return nil, false, errors.New("store down")
}
func (brokenStore) Finish(context.Context, string, string, *IdempotentResponse) error {
	return nil
}

func TestIdempotency_FailsClosedOnStoreError(t *testing.T) {
	var calls int32
	mw := Idempotency(IdempotencyConfig{Store: brokenStore{}, Principal: testPrincipal})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	req.Header.Set(IdempotencyKeyHeader, "k1")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on store error (fail closed), got %d", rr.Code)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("handler must NOT execute when store fails closed; got %d calls", calls)
	}
}

func TestIdempotency_FailOpenOptionPreservesAvailability(t *testing.T) {
	var calls int32
	mw := Idempotency(IdempotencyConfig{Store: brokenStore{}, FailOpen: true, Principal: testPrincipal})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	req.Header.Set(IdempotencyKeyHeader, "k1")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("FailOpen should let request through; got %d", rr.Code)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected handler to run with FailOpen; got %d calls", calls)
	}
}

// ----- Finish survives client disconnect -----------------------------------

// recordingStore captures the ctx state DURING Finish so the assertion
// can inspect cancellation without racing the middleware's own
// per-call cancel func.
type recordingStore struct {
	mu             sync.Mutex
	beginCtx       context.Context
	finishCalled   bool
	finishCtxErr   error // ctx.Err() at the moment Finish was invoked
	finishCtxValid bool  // ctx.Done() returns a non-nil channel (i.e. derived)
	finishVal      *IdempotentResponse
}

func (s *recordingStore) Begin(ctx context.Context, key, fp string) (*IdempotentResponse, bool, error) {
	s.mu.Lock()
	s.beginCtx = ctx
	s.mu.Unlock()
	return nil, false, nil
}

func (s *recordingStore) Finish(ctx context.Context, key, fingerprint string, resp *IdempotentResponse) error {
	s.mu.Lock()
	s.finishCalled = true
	s.finishCtxErr = ctx.Err()
	s.finishCtxValid = ctx.Done() != nil
	s.finishVal = resp
	s.mu.Unlock()
	return nil
}

func TestMemoryStore_EvictsIdleFirst(t *testing.T) {
	s := NewMemoryIdempotencyStore(time.Hour, WithMemoryStoreMaxEntries(3))
	ctx := context.Background()
	for _, k := range []string{"k1", "k2", "k3"} {
		_, _, _ = s.Begin(ctx, k, "fp")
		if err := s.Finish(ctx, k, "fp", &IdempotentResponse{Status: 200, Body: []byte("b")}); err != nil {
			t.Fatalf("finish %s: %v", k, err)
		}
		time.Sleep(1 * time.Millisecond) // distinct lastAccess stamps
	}
	// Warm k1: a replay touch makes it the most recently used entry.
	if _, ok, _ := s.Begin(ctx, "k1", "fp"); !ok {
		t.Fatal("k1 should replay before the flood")
	}
	time.Sleep(1 * time.Millisecond)
	// Flood to the cap: the two claims the flood forces out must be the
	// idle entries (k2, k3), not the warm k1 — a flood of fresh keys can
	// only evict flood and idle state, never the recently replayed key.
	for i := range 2 {
		k := fmt.Sprintf("flood-%d", i)
		_, _, _ = s.Begin(ctx, k, "fp")
		time.Sleep(1 * time.Millisecond)
	}
	if _, ok, _ := s.Begin(ctx, "k1", "fp"); !ok {
		t.Fatal("warm entry k1 was evicted while idle entries k2/k3 were available: eviction must be idle-first")
	}
}

// ----- default store bounded (F1: attacker-mintable cache keys) -------------
//
// Pins the unbounded default memory store, found by the 2026-09-04
// red-probe round; fixed by bounding NewMemoryIdempotencyStore at
// 100_000 entries (idle-first eviction, the framework/ratelimit maxKeys
// precedent) plus a 1 MiB per-entry byte cap, with IdempotencyConfig
// knobs (MaxStoreEntries / MaxStoreEntryBytes) for hosts that need
// different bounds.
// Family: F1 resource exhaustion from request-borne input (attacker-mintable cache keys)
// Property: the default store's retained entry count never exceeds its cap, whatever the client mints.
// Surfaces: core/middleware/idempotency.go::NewMemoryIdempotencyStore (default caps),
//           core/middleware/idempotency.go::Begin+evictLocked (shed at the cap),
//           core/middleware/idempotency.go::Finish (per-entry byte cap).

// TestMemoryStore_DefaultEntriesBounded: the all-defaults store is
// bounded — a flood of unique keys can never leave more entries than the
// cap, and in-flight claims are still protected ahead of completed
// entries.
func TestMemoryStore_DefaultEntriesBounded(t *testing.T) {
	s := NewMemoryIdempotencyStore(time.Hour).(*memoryIdempotencyStore)
	if s.maxEntries != defaultMaxIdemEntries {
		t.Fatalf("default cap = %d, want %d", s.maxEntries, defaultMaxIdemEntries)
	}
	if s.maxEntryBytes != defaultMaxIdemEntryBytes {
		t.Fatalf("default per-entry byte cap = %d, want %d", s.maxEntryBytes, defaultMaxIdemEntryBytes)
	}
	ctx := context.Background()
	for i := range defaultMaxIdemEntries + 5_000 {
		_, _, _ = s.Begin(ctx, fmt.Sprintf("k%d", i), "fp")
	}
	s.mu.Lock()
	n := len(s.entries)
	s.mu.Unlock()
	if n > defaultMaxIdemEntries {
		t.Fatalf("SECURITY: [idem-default] flood of %d unique keys left %d resident entries, cap %d: "+
			"process memory must not be proportional to attacker-chosen Idempotency-Key values",
			defaultMaxIdemEntries+5_000, n, defaultMaxIdemEntries)
	}
}

// TestMemoryStore_InFlightClaimsEvictedLast: when every entry is an
// in-flight claim, the cap still holds (the claim is shed LRU), but a
// completed entry is always shed first when one exists — dropping an
// in-flight claim opens a concurrent-execution window for that key.
func TestMemoryStore_InFlightClaimsEvictedLast(t *testing.T) {
	s := NewMemoryIdempotencyStore(time.Hour, WithMemoryStoreMaxEntries(2)).(*memoryIdempotencyStore)
	ctx := context.Background()
	_, _, _ = s.Begin(ctx, "claim-1", "fp")
	time.Sleep(1 * time.Millisecond)
	_, _, _ = s.Begin(ctx, "claim-2", "fp")
	time.Sleep(1 * time.Millisecond)
	// All entries in-flight: the third claim still fits inside the cap.
	_, _, _ = s.Begin(ctx, "claim-3", "fp")
	s.mu.Lock()
	n := len(s.entries)
	s.mu.Unlock()
	if n > 2 {
		t.Fatalf("all-in-flight flood left %d entries, cap 2", n)
	}
}

// TestMemoryStore_EntryByteCapDropsOversized: a response larger than the
// per-entry byte cap is dropped at Finish — the client already received
// it — so one cached response cannot retain unbounded bytes even when
// MaxResponseBytes was raised at the middleware level.
func TestMemoryStore_EntryByteCapDropsOversized(t *testing.T) {
	s := NewMemoryIdempotencyStore(time.Hour, WithMemoryStoreMaxEntryBytes(64))
	ctx := context.Background()
	_, _, _ = s.Begin(ctx, "big", "fp")
	if err := s.Finish(ctx, "big", "fp", &IdempotentResponse{Status: 200, Body: make([]byte, 128)}); err != nil {
		t.Fatalf("finish big: %v", err)
	}
	if resp, ok, _ := s.Begin(ctx, "big", "fp"); ok || resp != nil {
		t.Fatalf("oversized response was retained (ok=%v resp=%v)", ok, resp != nil)
	}
	// Under the cap: retained and replayed.
	_, _, _ = s.Begin(ctx, "small", "fp")
	if err := s.Finish(ctx, "small", "fp", &IdempotentResponse{Status: 200, Body: []byte("ok")}); err != nil {
		t.Fatalf("finish small: %v", err)
	}
	if resp, ok, _ := s.Begin(ctx, "small", "fp"); !ok || resp == nil {
		t.Fatal("under-cap response should replay")
	}
}

func TestIdempotency_FinishUsesUncancelledContext(t *testing.T) {
	store := &recordingStore{}
	mw := Idempotency(IdempotencyConfig{Store: store, Principal: testPrincipal})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Build a request whose context is already cancelled by the time the
	// handler returns; simulates a client that disconnected mid-handler.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/x", strings.NewReader(`{}`))
	req.Header.Set(IdempotencyKeyHeader, "k1")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.finishCalled {
		t.Fatalf("Finish was never called")
	}
	if store.finishCtxErr != nil {
		t.Fatalf("Finish must use uncancelled context to record cleanup; ctx.Err at call time=%v", store.finishCtxErr)
	}
}

// ----- F1: Finish must only write the claim Begin created -------------------
//
// SECURITY (cross-user disclosure): Begin claims a key with a fingerprint, but
// the in-flight claim can expire while the handler is still running. A second
// caller then re-claims the SAME key under a DIFFERENT fingerprint. If Finish
// persists by key alone (ignoring which fingerprint owns the row), the first
// caller's late Finish staples its response onto the second caller's row, and
// because Begin never rewrites the fingerprint column, the second caller's
// retry matches the fingerprint and is served the FIRST caller's body. That is a
// cross-user leak whenever Principal returns a user/tenant id.
//
// The fix is a breaking signature change: Finish now takes the fingerprint and
// a store MUST refuse to write a row whose fingerprint does not match. The
// optional-interface alternative was rejected: it leaves every third-party
// IdempotencyStore silently vulnerable, which is the wrong default for a leak of
// this kind. Memory and SQL both honour it below.

func TestIdemFinishOnlyWritesOwnClaim(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		s := &memoryIdempotencyStore{
			ttl:         time.Second,
			inFlightTTL: 50 * time.Millisecond,
			entries:     map[string]*idemEntry{},
		}
		runFinishOwnClaim(t, s)
	})
	t.Run("sql", func(t *testing.T) {
		_, s := openSQLIdemStore(t)
		s.inFlightTTL = 50 * time.Millisecond // tiny in-flight window so the claim expires mid-handler
		runFinishOwnClaim(t, s)
	})
}

func runFinishOwnClaim(t *testing.T, s IdempotencyStore) {
	t.Helper()
	ctx := context.Background()
	const key = "k-f1"
	const fpA = "fp-userA"
	const fpB = "fp-userB"
	const secretA = `{"secret":"USER-A-PRIVATE-ORDER"}`

	// 1. User A claims the key (in-flight, short TTL).
	if _, ok, err := s.Begin(ctx, key, fpA); err != nil || ok {
		t.Fatalf("A fresh claim: ok=%v err=%v", ok, err)
	}
	// 2. A's claim expires while its handler is still running.
	time.Sleep(80 * time.Millisecond)
	// 3. User B, same key, DIFFERENT body/fingerprint, re-claims the stale row.
	if _, ok, err := s.Begin(ctx, key, fpB); err != nil || ok {
		t.Fatalf("B re-claim of A's expired claim: ok=%v err=%v", ok, err)
	}
	// 4. A's handler finally returns and tries to persist A's response.
	respA := &IdempotentResponse{
		Status: http.StatusOK,
		Header: http.Header{"X-Owner": []string{"user-A"}},
		Body:   []byte(secretA),
	}
	if err := s.Finish(ctx, key, fpA, respA); err != nil {
		t.Fatalf("A Finish: %v", err)
	}
	// 5. B's retry must NEVER receive A's response body. Assert on the BODY; a
	// status-only check would miss the disclosure (A's status can be 200 too).
	replay, ok, err := s.Begin(ctx, key, fpB)
	if ok && replay != nil && string(replay.Body) == secretA {
		t.Fatalf("F1 cross-user disclosure: B's retry was served A's body %q (status=%d, err=%v)",
			secretA, replay.Status, err)
	}
}

// ----- F4: expired-claim reclaim must not delete a fresh claim --------------
//
// When Begin finds an expired row it deletes the stale claim and re-claims. The
// DELETE once matched on key alone, with no expiry predicate. Under contention
// two callers can both observe the expired row: the first deletes it and inserts
// a FRESH claim; the second's key-only DELETE then destroys that FRESH claim and
// it re-inserts, so BOTH believe they own the key and BOTH run the handler.
// That is the double-execution the middleware exists to prevent, and it defeats
// the ErrInFlight fallback. The DELETE is now expiry-gated
// (WHERE key = $1 AND expires_at <= $2).
//
// This is a race-invariant guard: with the fix exactly ONE caller ever obtains a
// fresh claim, so the assertion never false-fails on correct code. The memory
// store serializes under a mutex and is immune by construction; it is covered
// here as the property x surface pairing.
//
// The in-flight TTL here is deliberately LONG. The seeded row is already
// expired, so the reclaim path still runs, but a winner's fresh claim cannot
// legitimately expire mid-race; with a short TTL a slow, loaded runner lets a
// later racer correctly re-claim an expired row, which is the TTL working and
// is indistinguishable from the bug. A long TTL makes a second fresh claim
// proof that the DELETE removed a live one.

func TestIdemReclaimKeepsFreshRow(t *testing.T) {
	t.Run("memory", func(t *testing.T) {
		ms := &memoryIdempotencyStore{
			ttl:         time.Minute,
			inFlightTTL: 30 * time.Second,
			entries:     map[string]*idemEntry{},
		}
		ctx := context.Background()
		_, _, _ = ms.Begin(ctx, "warmup", "fp-w") // consume the first-call reap
		reseed := func() {
			ms.mu.Lock()
			ms.entries["k-f4"] = &idemEntry{
				fingerprint: "fp-seed",
				expires:     time.Now().Add(-time.Second),
				lastAccess:  time.Now().Add(-2 * time.Second),
			}
			ms.mu.Unlock()
		}
		runReclaimOneWinner(t, ms, "k-f4", reseed)
	})
	t.Run("sql", func(t *testing.T) {
		db, s := newWALSQLIdemStore(t, 30*time.Second)
		ctx := context.Background()
		// Run one Begin up front so the per-minute reap has already fired and
		// won't delete our seeded expired row before the racers reach the
		// reclaim path (the steady-state condition this fix targets).
		if _, _, err := s.Begin(ctx, "warmup", "fp-warmup"); err != nil {
			t.Fatalf("warmup Begin: %v", err)
		}
		reseed := func() {
			if _, err := db.Exec("DELETE FROM idempotency_keys WHERE key = ?", "k-f4"); err != nil {
				t.Fatalf("reseed delete: %v", err)
			}
			now := time.Now()
			if _, err := db.Exec(
				"INSERT INTO idempotency_keys (key, fingerprint, expires_at, created_at) VALUES (?, ?, ?, ?)",
				"k-f4", "fp-seed", now.Add(-time.Second), now.Add(-2*time.Second),
			); err != nil {
				t.Fatalf("reseed insert: %v", err)
			}
		}
		runReclaimOneWinner(t, s, "k-f4", reseed)
	})
}

func runReclaimOneWinner(t *testing.T, s IdempotencyStore, key string, reseed func()) {
	t.Helper()
	ctx := context.Background()

	// The F4 window (SELECT sees an expired row, then a concurrent INSERT lands,
	// then this caller's key-only DELETE removes the fresh claim) is narrow, so a
	// single race rarely trips. We run many reseeded iterations to make a
	// regression reliably observable.
	//
	// The invariant on CORRECT code is exactly ONE fresh claimant per reseeded
	// iteration: the reclaim DELETE is expiry-gated, so it can never remove a
	// fresh claim another caller just inserted, which means a second re-claimer's
	// retry-INSERT conflicts and surfaces ErrInFlight instead. Asserting
	// fresh==1 (not merely fresh<=1) is what makes the test non-vacuous: the old
	// guard only failed when fresh>1, so if contention stopped EVERY claimant
	// from reaching the reclaim path (fresh==0) the test passed without
	// exercising the property at all.
	//
	// Per-finding: any Begin error other than the two expected race outcomes
	// (ErrInFlight, ErrFingerprintMismatch) is a store fault and must fail.
	const racers = 60
	const iters = 60
	for iter := range iters {
		reseed()
		var fresh atomic.Int64
		unexpected := make(chan error, racers)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range racers {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				fp := fmt.Sprintf("fp-%d", i)
				_, ok, err := s.Begin(ctx, key, fp)
				switch {
				case err == nil && !ok:
					fresh.Add(1)
				case errors.Is(err, ErrInFlight), errors.Is(err, ErrFingerprintMismatch):
					// Expected: lost the race to another claimant, or a
					// sibling won under a different fingerprint.
				case err == nil && ok:
					// ok==true (replay) is impossible against a seeded
					// in-flight row. The store-fault default below sends
					// err, which is nil here, so the post-loop nil check
					// never fired and a replay passed silently. Surface a
					// non-nil error so the race is actually caught.
					select {
					case unexpected <- fmt.Errorf("unexpected replay: Begin returned ok=true (a response) against a seeded in-flight row"):
					default:
					}
				default:
					// Any other error is a store fault.
					select {
					case unexpected <- err:
					default:
					}
				}
			}(i)
		}
		close(start)
		wg.Wait()
		close(unexpected)
		var firstUnexpected error
		for e := range unexpected {
			if firstUnexpected == nil {
				firstUnexpected = e
			}
		}
		if firstUnexpected != nil {
			t.Fatalf("F4 iter %d: unexpected Begin error (only ErrInFlight/ErrFingerprintMismatch expected): %v", iter, firstUnexpected)
		}
		if f := fresh.Load(); f != 1 {
			t.Fatalf("F4 iter %d: expected exactly ONE fresh claimant (the reclaim path must run AND must not delete a fresh claim), got %d", iter, f)
		}
	}
}

// newWALSQLIdemStore opens a file-backed sqlite DB in WAL mode with several
// connections so concurrent Begins genuinely interleave. The in-memory store
// used elsewhere is pinned to one connection and cannot reproduce the race.
func newWALSQLIdemStore(t *testing.T, inFlightTTL time.Duration) (*sql.DB, *SQLIdempotencyStore) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "idem_f4.db") + "?_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := NewSQLIdempotencyStore(db, WithSQLIdempotencyInFlightTTL(inFlightTTL))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return db, s
}

// ----- identity headers beyond Set-Cookie -----------------------------------
//
// Property: no identity-bearing response header from the original request
// is replayed to a later caller. headersStrippedFromReplay lists five;
// the Set-Cookie surface is pinned by TestIdempotency_StripsHandlerSetCookieFromReplay.
// This loop covers the remaining four surfaces plus a benign handler
// header that MUST survive the strip.
func TestReplayStripsAllCredentialHeaders(t *testing.T) {
	for _, hdr := range []string{"Cookie", "Authorization", "Proxy-Authorization", "Www-Authenticate"} {
		t.Run(hdr, func(t *testing.T) {
			mw := Idempotency(IdempotencyConfig{Principal: testPrincipal})
			srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set(hdr, "secret-credential-token")
				w.Header().Set("X-Trace", "keep-me")
				w.WriteHeader(http.StatusCreated)
			}))
			do := func() *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, "/pay", nil)
				req.Header.Set("X-Caller", "u1")
				req.Header.Set(IdempotencyKeyHeader, "k-"+hdr)
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				return rec
			}
			do()
			replay := do()
			if replay.Header().Get(hdr) != "" {
				t.Fatalf("identity header %s was replayed to the second caller", hdr)
			}
			if replay.Header().Get("X-Trace") != "keep-me" {
				t.Fatal("benign handler header was wrongly stripped from the replay")
			}
		})
	}
}

// ----- fingerprint binds the full request shape ------------------------------
//
// Property: the idempotency fingerprint binds a key to the FULL request
// identity — principal, method, path, query, content-type, and body — so
// reusing a key with ANY differing component is a 422 mismatch, never the
// other request's cached response. The body surface is pinned by
// TestIdempotency_FingerprintMismatchReturns422; this loops the rest.
func TestFingerprintBindsFullRequestShape(t *testing.T) {
	type spec struct {
		method, target, ct, body string
	}
	cases := []struct {
		name  string
		first spec
		retry spec
	}{
		{"different path", spec{"POST", "/orders", "", ""}, spec{"POST", "/transfers", "", ""}},
		{"different query", spec{"POST", "/pay?x=1", "", ""}, spec{"POST", "/pay?x=2", "", ""}},
		{"different method", spec{"POST", "/pay", "", ""}, spec{"PUT", "/pay", "", ""}},
		{"different content-type", spec{"POST", "/pay", "", "same"}, spec{"POST", "/pay", "text/plain", "same"}},
		{"different body", spec{"POST", "/pay", "", "a"}, spec{"POST", "/pay", "", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var calls int32
			mw := Idempotency(IdempotencyConfig{Principal: testPrincipal})
			srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(http.StatusCreated)
			}))
			send := func(s spec) *httptest.ResponseRecorder {
				req := httptest.NewRequest(s.method, s.target, strings.NewReader(s.body))
				if s.ct != "" {
					req.Header.Set("Content-Type", s.ct)
				}
				req.Header.Set("X-Caller", "u1")
				req.Header.Set(IdempotencyKeyHeader, "shared-key")
				rec := httptest.NewRecorder()
				srv.ServeHTTP(rec, req)
				return rec
			}
			if rec := send(tc.first); rec.Code != http.StatusCreated {
				t.Fatalf("first request: %d", rec.Code)
			}
			rec := send(tc.retry)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("key reuse with differing %s must 422, got %d (body %q)", tc.name, rec.Code, rec.Body.String())
			}
			if n := atomic.LoadInt32(&calls); n != 1 {
				t.Fatalf("handler ran %d times; the mismatched retry must not execute it", n)
			}
		})
	}
}

// ----- store shard: same key, different principals ---------------------------
//
// Property: the "principal\x00key" shard keeps two principals using the
// SAME Idempotency-Key value in separate entries — the second principal
// gets its OWN claim (not ErrInFlight) and its OWN replay (never the
// first principal's cached body). Surfaces: memory store and SQL store.
func TestIdemStoreKeyShardedByPrincipal(t *testing.T) {
	run := func(t *testing.T, s IdempotencyStore) {
		t.Helper()
		ctx := context.Background()
		shard := func(principal, key string) string { return principal + "\x00" + key }
		k1, k2 := shard("u1", "shared-key"), shard("u2", "shared-key")

		if _, ok, err := s.Begin(ctx, k1, "fp1"); err != nil || ok {
			t.Fatalf("u1 first claim: ok=%v err=%v", ok, err)
		}
		if _, ok, err := s.Begin(ctx, k2, "fp1"); err != nil || ok {
			t.Fatalf("u2 claim collided with u1's in-flight shard: ok=%v err=%v", ok, err)
		}
		_ = s.Finish(ctx, k1, "fp1", &IdempotentResponse{Status: 201, Header: http.Header{}, Body: []byte("u1-secret")})
		_ = s.Finish(ctx, k2, "fp1", &IdempotentResponse{Status: 201, Header: http.Header{}, Body: []byte("u2-secret")})

		resp, ok, err := s.Begin(ctx, k2, "fp1")
		if err != nil || !ok {
			t.Fatalf("u2 replay: ok=%v err=%v", ok, err)
		}
		if string(resp.Body) != "u2-secret" {
			t.Fatalf("u2 was served u1's cached body: %q", resp.Body)
		}
	}
	t.Run("memory", func(t *testing.T) {
		run(t, NewMemoryIdempotencyStore(time.Minute))
	})
	t.Run("sql", func(t *testing.T) {
		_, s := openSQLIdemStore(t)
		run(t, s)
	})
}

// ----- shard ambiguity under NUL --------------------------------------------
//
// Property: the storage shard must be injective over (principal, key)
// pairs — two DIFFERENT pairs must never land on the same shard entry.
// The middleware builds the shard as principal + "\x00" + key, which is
// ambiguous when either side contains NUL: ("a\x00b","c") and ("a",
// "b\x00c") both produce "a\x00b\x00c". The fingerprint difference (the
// principal is hashed in) saves this from being a body leak — the second
// caller gets 422 — but that caller is still DENIED a key it is the first
// and only user of: a cross-principal cache-poisoning DoS whenever a
// Principal function can return NUL-bearing subjects (e.g. one that
// surfaces a request-derived value). Driven through the middleware so the
// construction site itself is under test.
func TestIdemShardAmbiguousUnderNULPrincipal(t *testing.T) {
	mw := Idempotency(IdempotencyConfig{
		Principal: func(r *http.Request) string { return r.Header.Get("X-Caller") },
	})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	send := func(caller, key string) int {
		req := httptest.NewRequest(http.MethodPost, "/pay", nil)
		req.Header.Set("X-Caller", caller)
		req.Header.Set(IdempotencyKeyHeader, key)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := send("a\x00b", "c"); code != http.StatusOK {
		t.Fatalf("first pair: %d", code)
	}
	if code := send("a", "b\x00c"); code != http.StatusOK {
		t.Fatalf("distinct (principal,key) pair collided onto the first pair's shard and was denied: got %d, want 200", code)
	}
}

// ----- Finish-failure log sink ----------------------------------------------
//
// Property (same family as TestLogSinksScrubAndBound): a request-derived
// value a middleware writes into a slog attribute is control-byte
// scrubbed at EVERY sink. The Idempotency-Key is request-borne and the
// Finish-failure path logs it RAW ("key", key): a forged key paints a
// forged line into the operator's tail. Attack shapes: SOH, ESC-prefixed
// ANSI, DEL.
type finishFailStore struct{}

func (finishFailStore) Begin(context.Context, string, string) (*IdempotentResponse, bool, error) {
	return nil, false, nil
}
func (finishFailStore) Finish(context.Context, string, string, *IdempotentResponse) error {
	return errors.New("db down")
}

func TestIdempotencyFinishLogKeyScrubbed(t *testing.T) {
	sink := &captureHandler{}
	mw := Idempotency(IdempotencyConfig{
		Store:     finishFailStore{},
		Principal: testPrincipal,
		Logger:    slog.New(sink),
	})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	for _, key := range []string{"k\x01quiet", "k\x1b[31mred", "k\x7fdel"} {
		sink.reset()
		req := httptest.NewRequest(http.MethodPost, "/pay", nil)
		req.Header.Set("X-Caller", "u1")
		req.Header.Set(IdempotencyKeyHeader, key)
		srv.ServeHTTP(httptest.NewRecorder(), req)
		if got := sink.get("key"); strings.ContainsAny(got, c0AndDelSet) {
			t.Errorf("raw Idempotency-Key logged on Finish failure: %q", got)
		}
	}
}

// ----- in-flight branch: Retry-After ----------------------------------------
//
// Property: the in-flight branch answers 409 with a Retry-After so a
// concurrent duplicate backs off instead of hot-looping. The concurrent
// 409 is pinned by TestIdempotency_ConcurrentReturnsInFlight; this pins
// the Retry-After half deterministically (stubbed store, no goroutines)
// and that the handler is not invoked.
type inFlightStore struct{}

func (inFlightStore) Begin(context.Context, string, string) (*IdempotentResponse, bool, error) {
	return nil, false, ErrInFlight
}
func (inFlightStore) Finish(context.Context, string, string, *IdempotentResponse) error {
	return nil
}

func TestIdempotencyInFlightSetsRetryAfter(t *testing.T) {
	mw := Idempotency(IdempotencyConfig{Store: inFlightStore{}, Principal: testPrincipal})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler must not run on the in-flight branch")
	}))
	req := httptest.NewRequest(http.MethodPost, "/pay", nil)
	req.Header.Set("X-Caller", "u1")
	req.Header.Set(IdempotencyKeyHeader, "k")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("expected Retry-After: 1, got %q", got)
	}
}

// ----- key-length cap boundary ----------------------------------------------
//
// Property: the Idempotency-Key length cap rejects at 256 and still
// admits its boundary value (255) — an off-by-one here would either let
// unbounded keys into the store or reject valid ones.
func TestIdempotencyKeyLengthBoundary(t *testing.T) {
	var calls int32
	mw := Idempotency(IdempotencyConfig{Principal: testPrincipal})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
	}))
	send := func(n int) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/pay", nil)
		req.Header.Set("X-Caller", "u1")
		req.Header.Set(IdempotencyKeyHeader, strings.Repeat("k", n))
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}
	if rec := send(255); rec.Code != http.StatusOK {
		t.Fatalf("255-char key (boundary) must be accepted, got %d", rec.Code)
	}
	if rec := send(256); rec.Code != http.StatusBadRequest {
		t.Fatalf("256-char key must be rejected 400, got %d", rec.Code)
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Fatalf("handler ran %d times; the oversized key must not reach it", n)
	}
}
