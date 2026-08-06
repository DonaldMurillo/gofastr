package middleware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

	// Replay — must NOT carry the handler's session cookie or Authorization.
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
	// middleware must NOT replay Alice's response for Bob — that would
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

func TestMemoryStore_MaxEntriesEvictsOldest(t *testing.T) {
	s := NewMemoryIdempotencyStore(time.Hour, WithMemoryStoreMaxEntries(3))
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("k%d", i)
		_, _, _ = s.Begin(ctx, key, "fp")
		// micro-spacing so createdAt differs deterministically
		time.Sleep(1 * time.Millisecond)
	}
	// After 5 inserts with cap=3, only the most recent 3 should be present.
	// The oldest keys (k0, k1) should be evicted — fresh Begin with their
	// keys is treated as a NEW claim, not an in-flight return.
	resp, ok, err := s.Begin(ctx, "k0", "fp")
	if ok || resp != nil {
		t.Fatalf("k0 should have been evicted (replay=%v,err=%v)", ok, err)
	}
}

func TestIdempotency_FinishUsesUncancelledContext(t *testing.T) {
	store := &recordingStore{}
	mw := Idempotency(IdempotencyConfig{Store: store, Principal: testPrincipal})
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Build a request whose context is already cancelled by the time the
	// handler returns — simulates a client that disconnected mid-handler.
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
// caller's late Finish staples its response onto the second caller's row — and
// because Begin never rewrites the fingerprint column, the second caller's
// retry matches the fingerprint and is served the FIRST caller's body. That is a
// cross-user leak whenever Principal returns a user/tenant id.
//
// The fix is a breaking signature change — Finish now takes the fingerprint and
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
	// 3. User B — same key, DIFFERENT body/fingerprint — re-claims the stale row.
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
	// 5. B's retry must NEVER receive A's response body. Assert on the BODY — a
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
// it re-inserts — so BOTH believe they own the key and BOTH run the handler.
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
// legitimately expire mid-race — with a short TTL a slow, loaded runner lets a
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
				createdAt:   time.Now().Add(-2 * time.Second),
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
		var fresh int64
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
					atomic.AddInt64(&fresh, 1)
				case errors.Is(err, ErrInFlight), errors.Is(err, ErrFingerprintMismatch):
					// Expected: lost the race to another claimant, or a
					// sibling won under a different fingerprint.
				default:
					// ok==true (replay) is impossible against a seeded
					// in-flight row; any other error is a store fault.
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
		if f := atomic.LoadInt64(&fresh); f != 1 {
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
