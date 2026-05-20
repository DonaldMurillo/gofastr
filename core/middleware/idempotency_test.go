package middleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIdempotency_BypassesSafeMethods(t *testing.T) {
	var calls int32
	h := Idempotency(IdempotencyConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(IdempotencyKeyHeader, "k")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %d: got %d", i, rr.Code)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Fatalf("expected 3 handler invocations for GET, got %d", got)
	}
}

func TestIdempotency_NoKey_OptionalPasses(t *testing.T) {
	var calls int32
	h := Idempotency(IdempotencyConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d", rr.Code)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("handler should have run")
	}
}

func TestIdempotency_NoKey_RequiredRejects(t *testing.T) {
	h := Idempotency(IdempotencyConfig{Required: true})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not have been invoked")
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestIdempotency_ReplaysCachedResponse(t *testing.T) {
	var calls int32
	h := Idempotency(IdempotencyConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("X-Custom", "first")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":42}`))
	}))

	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"qty":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(IdempotencyKeyHeader, "abc-123")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	first := send()
	if first.Code != http.StatusCreated || first.Body.String() != `{"id":42}` {
		t.Fatalf("first: %d %q", first.Code, first.Body.String())
	}
	if first.Header().Get("Idempotent-Replay") != "" {
		t.Fatalf("first response should not be marked as replay")
	}

	second := send()
	if second.Code != http.StatusCreated || second.Body.String() != `{"id":42}` {
		t.Fatalf("replay: %d %q", second.Code, second.Body.String())
	}
	if second.Header().Get("Idempotent-Replay") != "true" {
		t.Fatalf("replay missing Idempotent-Replay header")
	}
	if second.Header().Get("X-Custom") != "first" {
		t.Fatalf("replay lost custom header")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("handler should have run once, got %d", calls)
	}
}

func TestIdempotency_FingerprintMismatchReturns422(t *testing.T) {
	h := Idempotency(IdempotencyConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	}))

	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(IdempotencyKeyHeader, "same-key")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}

	if rr := post(`{"qty":1}`); rr.Code != http.StatusCreated {
		t.Fatalf("first: %d", rr.Code)
	}
	rr := post(`{"qty":2}`) // different body, same key
	if rr.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 on fingerprint mismatch, got %d", rr.Code)
	}
}

func TestIdempotency_NonSuccessReleasesClaim(t *testing.T) {
	var calls int32
	h := Idempotency(IdempotencyConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
		req.Header.Set(IdempotencyKeyHeader, "release-me")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	if rr := send(); rr.Code != http.StatusInternalServerError {
		t.Fatalf("first: %d", rr.Code)
	}
	if rr := send(); rr.Code != http.StatusCreated {
		t.Fatalf("retry should have run handler again; got %d", rr.Code)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 handler runs, got %d", calls)
	}
}

func TestIdempotency_TooLongKeyRejected(t *testing.T) {
	h := Idempotency(IdempotencyConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	req.Header.Set(IdempotencyKeyHeader, strings.Repeat("a", 256))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestIdempotency_BodyTooLargeBypassesCleanly(t *testing.T) {
	var calls int32
	var captured []byte
	h := Idempotency(IdempotencyConfig{MaxBodyBytes: 16})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		b, _ := io.ReadAll(r.Body)
		captured = b
		w.WriteHeader(http.StatusCreated)
	}))

	payload := strings.Repeat("x", 64) // > MaxBodyBytes
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
		req.Header.Set(IdempotencyKeyHeader, "big")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	rr := send()
	if rr.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", rr.Code)
	}
	if rr.Header().Get("Idempotent-Bypass") != "body-too-large" {
		t.Fatalf("expected bypass header, got %q", rr.Header().Get("Idempotent-Bypass"))
	}
	if !bytes.Equal(captured, []byte(payload)) {
		t.Fatalf("handler did not see full body")
	}

	// Same key second time still runs the handler (no caching took place).
	send()
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("oversized requests should bypass caching; got %d calls", calls)
	}
}

func TestIdempotency_ConcurrentReturnsInFlight(t *testing.T) {
	start := make(chan struct{})
	release := make(chan struct{})
	var concurrent int32
	h := Idempotency(IdempotencyConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&concurrent, 1)
		start <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	}))

	results := make(chan int, 2)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
		req.Header.Set(IdempotencyKeyHeader, "in-flight")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		results <- rr.Code
	}()

	<-start // first handler is now blocked inside the middleware

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	req.Header.Set(IdempotencyKeyHeader, "in-flight")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409 while first request in flight, got %d", rr.Code)
	}
	if rr.Header().Get("Retry-After") == "" {
		t.Fatalf("expected Retry-After header on in-flight conflict")
	}

	close(release)
	if got := <-results; got != http.StatusOK {
		t.Fatalf("first request should have completed 200, got %d", got)
	}
}

func TestIdempotency_StoreFailureFailsClosedByDefault(t *testing.T) {
	bad := failingStore{err: errors.New("backend down")}
	var calls int32
	h := Idempotency(IdempotencyConfig{Store: bad})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
	}))
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	req.Header.Set(IdempotencyKeyHeader, "k")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("default behaviour must fail closed on store error; got %d", rr.Code)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("handler must NOT run on store failure when fail-closed")
	}
}

func TestMemoryStore_TTLExpiry(t *testing.T) {
	s := NewMemoryIdempotencyStore(30 * time.Millisecond)
	resp := &IdempotentResponse{Status: 201, Header: http.Header{}, Body: []byte(`ok`)}
	if _, _, err := s.Begin(nil, "k", "fp"); err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(nil, "k", resp); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Begin(nil, "k", "fp")
	if err != nil || !ok || got == nil {
		t.Fatalf("expected cached hit, got %v %v %v", got, ok, err)
	}
	time.Sleep(50 * time.Millisecond)
	got, ok, err = s.Begin(nil, "k", "fp")
	if err != nil {
		t.Fatalf("expected fresh claim, got err %v", err)
	}
	if ok || got != nil {
		t.Fatalf("expected expired entry to allow fresh claim, got cached")
	}
}

func TestIdempotency_ReplayDoesNotOverwriteUpstreamHeaders(t *testing.T) {
	// Upstream middleware writes a per-request header (mimics RequestID).
	// On replay, that header should reflect the CURRENT request, not the
	// original one — the cache only stores headers the handler set.
	reqID := int32(0)
	upstream := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&reqID, 1)
			w.Header().Set("X-Request-ID", "req-"+strconv.Itoa(int(n)))
			next.ServeHTTP(w, r)
		})
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Handler-Set", "by-handler")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("ok"))
	})
	chain := upstream(Idempotency(IdempotencyConfig{})(handler))

	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
		req.Header.Set(IdempotencyKeyHeader, "k")
		rr := httptest.NewRecorder()
		chain.ServeHTTP(rr, req)
		return rr
	}
	first := send()
	if first.Header().Get("X-Request-ID") != "req-1" {
		t.Fatalf("first X-Request-ID: got %q", first.Header().Get("X-Request-ID"))
	}
	second := send()
	if second.Header().Get("Idempotent-Replay") != "true" {
		t.Fatal("second call should be a replay")
	}
	if second.Header().Get("X-Request-ID") != "req-2" {
		t.Fatalf("replay should carry the CURRENT request's X-Request-ID, got %q",
			second.Header().Get("X-Request-ID"))
	}
	if second.Header().Get("X-Handler-Set") != "by-handler" {
		t.Fatalf("handler-set header missing on replay: %q", second.Header().Get("X-Handler-Set"))
	}
}

func TestIdempotency_OversizedResponseBypassesCache(t *testing.T) {
	const cap = 64
	bigBody := strings.Repeat("x", cap*2) // > cap
	var calls int32
	h := Idempotency(IdempotencyConfig{MaxResponseBytes: cap})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(bigBody))
	}))
	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
		req.Header.Set(IdempotencyKeyHeader, "big-resp")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr
	}
	first := send()
	if first.Body.String() != bigBody {
		t.Fatalf("first response body should pass through unchanged")
	}
	second := send()
	if second.Header().Get("Idempotent-Replay") != "" {
		t.Fatalf("oversized response should not be cached/replayed")
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("handler should run again when previous response was uncacheable; got %d", calls)
	}
}

// failingStore returns err from Begin to exercise the fail-open path.
type failingStore struct{ err error }

func (f failingStore) Begin(_ context.Context, _, _ string) (*IdempotentResponse, bool, error) {
	return nil, false, f.err
}
func (f failingStore) Finish(_ context.Context, _ string, _ *IdempotentResponse) error { return nil }
