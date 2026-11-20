package middleware

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSweepCSRFCookieIsScriptReadableAndLax(t *testing.T) {
	w := httptest.NewRecorder()
	CSRF(CSRFConfig{})(csrfHandler()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	if cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("csrf cookie HttpOnly=%v SameSite=%v, want false and Lax", cookies[0].HttpOnly, cookies[0].SameSite)
	}
}

func TestSweepSecurityHeadersDisableXSSAuditor(t *testing.T) {
	w := httptest.NewRecorder()
	SecurityHeaders(SecurityHeadersConfig{})(csrfHandler()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if got := w.Header().Get("X-XSS-Protection"); got != "0" {
		t.Fatalf("X-XSS-Protection = %q, want 0", got)
	}
}

func TestSweepDevCSRFKeyDirIsPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested")
	if _, err := DevCSRFKeyFromFile(filepath.Join(dir, "csrf.key")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("key dir mode = %o, want 700", perm)
	}
}

func TestSweepMemoryFinishSkipsAnotherOwnersClaim(t *testing.T) {
	s := NewMemoryIdempotencyStore(time.Hour, WithMemoryIdempotencyInFlightTTL(20*time.Millisecond))
	ctx := context.Background()
	_, _, _ = s.Begin(ctx, "k", "fp-a")
	time.Sleep(40 * time.Millisecond)
	_, _, _ = s.Begin(ctx, "k", "fp-b")
	_ = s.Finish(ctx, "k", "fp-a", &IdempotentResponse{Status: http.StatusOK, Body: []byte("a-secret")})
	if replay, ok, _ := s.Begin(ctx, "k", "fp-b"); ok && replay != nil {
		t.Fatalf("b was served a's response %q", replay.Body)
	}
}

func TestSweepMemoryEvictsCompletedBeforeInFlight(t *testing.T) {
	s := NewMemoryIdempotencyStore(time.Hour, WithMemoryStoreMaxEntries(2))
	ctx := context.Background()
	_, _, _ = s.Begin(ctx, "in-flight", "fp")
	time.Sleep(time.Millisecond)
	_, _, _ = s.Begin(ctx, "done", "fp")
	_ = s.Finish(ctx, "done", "fp", &IdempotentResponse{Status: http.StatusOK})
	time.Sleep(time.Millisecond)
	_, _, _ = s.Begin(ctx, "new", "fp")
	if _, _, err := s.Begin(ctx, "in-flight", "fp"); !errors.Is(err, ErrInFlight) {
		t.Fatalf("in-flight claim was evicted before a completed entry: err=%v", err)
	}
}

func TestSweepIdemStoreKeyInjectiveAcrossParts(t *testing.T) {
	if idemStoreKey("a\x00b", "c", "", "k") == idemStoreKey("a", "b\x00c", "", "k") {
		t.Fatal("principal and tenant collide")
	}
	if idemStoreKey("p", "a\x00b", "c", "k") == idemStoreKey("p", "a", "b\x00c", "k") {
		t.Fatal("tenant and scope collide")
	}
}

func TestSweepIdempotencyDoesNotCacheClientErrors(t *testing.T) {
	calls := 0
	h := Idempotency(IdempotencyConfig{Principal: func(*http.Request) string { return "u" }})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set(IdempotencyKeyHeader, "k")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if calls != 2 {
		t.Fatalf("a 4xx answer was replayed: handler ran %d times", calls)
	}
}

func TestSweepIdempotencyPassesEntryByteCapToDefaultStore(t *testing.T) {
	calls := 0
	h := Idempotency(IdempotencyConfig{Principal: func(*http.Request) string { return "u" }, MaxStoreEntryBytes: 4})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte("more than four bytes"))
	}))
	for range 2 {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set(IdempotencyKeyHeader, "k")
		h.ServeHTTP(httptest.NewRecorder(), req)
	}
	if calls != 2 {
		t.Fatalf("a response over MaxStoreEntryBytes was replayed: handler ran %d times", calls)
	}
}

func TestSweepMetricsCollectorRunsOutsideTheLock(t *testing.T) {
	m := NewMetrics()
	m.RegisterCollector("reentrant", func(io.Writer) { m.RegisterCollector("other", nil) })
	done := make(chan struct{})
	go func() {
		MetricsHandler(m).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics", nil))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a collector that touches the registry deadlocked the scrape")
	}
}

func TestSweepSampledLoggingAlwaysLogsErrors(t *testing.T) {
	var out bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&out, nil))
	status := http.StatusOK
	h := SampledLoggingFn(1000, time.Hour, func() *slog.Logger { return logger })(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
	}))
	for _, s := range []int{http.StatusOK, http.StatusOK, http.StatusInternalServerError} {
		status = s
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	}
	if n := strings.Count(out.String(), "\n"); n != 2 {
		t.Fatalf("logged %d lines, want the first sampled one and the 500:\n%s", n, out.String())
	}
}

func TestSweepTimeoutContextReportsNoDeadline(t *testing.T) {
	var has bool
	Timeout(time.Second)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, has = r.Context().Deadline()
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if has {
		t.Fatal("the handler's context advertises a deadline a streaming response may never reach")
	}
}

func TestSweepTimeoutKeepsFirstStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		w.WriteHeader(http.StatusInternalServerError)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want the first one, 201", rec.Code)
	}
}

func TestSweepCeilSecondsRoundsUp(t *testing.T) {
	if got := ceilSeconds(1500 * time.Millisecond); got != 2 {
		t.Fatalf("ceilSeconds(1.5s) = %d, want 2", got)
	}
}

func TestSweepRateLimitForwardedOnlyFromTrustedPeer(t *testing.T) {
	cases := []struct{ name, peer, hopFormat string }{
		{"untrusted peer", "203.0.113.9:1", "198.51.100.%d"},
		{"private hop", "192.0.2.1:1", "10.0.0.%d"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			allowed := 0
			h := RateLimit(RateLimitConfig{Capacity: 2, RefillEvery: time.Hour, TrustProxyHeaders: true, TrustedProxies: []string{"192.0.2.1"}})(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { allowed++ }))
			for i := range 5 {
				req := httptest.NewRequest(http.MethodGet, "/", nil)
				req.RemoteAddr = c.peer
				req.Header.Set("X-Forwarded-For", fmt.Sprintf(c.hopFormat, i+1))
				h.ServeHTTP(httptest.NewRecorder(), req)
			}
			if allowed != 2 {
				t.Fatalf("rotating X-Forwarded-For got %d requests through, want 2", allowed)
			}
		})
	}
}

func TestSweepSQLReleaseKeepsAnotherOwnersClaim(t *testing.T) {
	_, s := openSQLIdemStoreWithOpts(t, WithSQLIdempotencyInFlightTTL(20*time.Millisecond))
	ctx := context.Background()
	_, _, _ = s.Begin(ctx, "k", "fp-a")
	time.Sleep(1100 * time.Millisecond)
	s.inFlightTTL = time.Hour
	if _, _, err := s.Begin(ctx, "k", "fp-b"); err != nil {
		t.Fatal(err)
	}
	_ = s.Finish(ctx, "k", "fp-a", nil)
	if _, _, err := s.Begin(ctx, "k", "fp-b"); !errors.Is(err, ErrInFlight) {
		t.Fatalf("a's release removed b's claim: err=%v", err)
	}
}

func TestSweepIdempotencyPassesEntryCapToDefaultStore(t *testing.T) {
	calls := 0
	h := Idempotency(IdempotencyConfig{Principal: func(*http.Request) string { return "u" }, MaxStoreEntries: 2})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
	}))
	for _, key := range []string{"k1", "k2", "k3", "k1"} {
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set(IdempotencyKeyHeader, key)
		h.ServeHTTP(httptest.NewRecorder(), req)
		time.Sleep(time.Millisecond)
	}
	if calls != 4 {
		t.Fatalf("k1 was still cached past MaxStoreEntries 2: handler ran %d times", calls)
	}
}
