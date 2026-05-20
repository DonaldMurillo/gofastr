package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ----- Set-Cookie / Authorization stripped from cache -----------------------

func TestIdempotency_StripsHandlerSetCookieFromReplay(t *testing.T) {
	mw := Idempotency(IdempotencyConfig{})
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
func (brokenStore) Finish(context.Context, string, *IdempotentResponse) error {
	return nil
}

func TestIdempotency_FailsClosedOnStoreError(t *testing.T) {
	var calls int32
	mw := Idempotency(IdempotencyConfig{Store: brokenStore{}})
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
	mw := Idempotency(IdempotencyConfig{Store: brokenStore{}, FailOpen: true})
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

func (s *recordingStore) Finish(ctx context.Context, key string, resp *IdempotentResponse) error {
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
	mw := Idempotency(IdempotencyConfig{Store: store})
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
