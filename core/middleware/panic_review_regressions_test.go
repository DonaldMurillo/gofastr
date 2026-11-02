package middleware

// Regressions found reviewing the 2026-11-01 core hunt fixes: a Hijack
// that panics under Timeout, a panicked request's metric under Compress,
// Timeout's panic wrapper hiding the handler's error, an invalid status
// code cut instead of answered, and a non-UTF-8 key on Postgres.

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- 1. Timeout: a Hijack whose underlying call panics leaves hijacking
// set forever; expire then waits on a Broadcast that never comes.

type panicHijackWriter struct{ http.ResponseWriter }

func (panicHijackWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// What a third-party wrapper's unchecked http.Hijacker assertion
	// does on a writer that has none.
	panic("interface conversion: writer is not http.Hijacker")
}

func TestTimeoutHijackPanicStillExpires(t *testing.T) {
	h := Timeout(50 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		func() {
			defer func() { _ = recover() }() // e.g. an upgrade library that recovers
			_, _, _ = w.(http.Hijacker).Hijack()
		}()
		time.Sleep(300 * time.Millisecond) // a slow handler: owes the client a 504
	}))
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(panicHijackWriter{rec}, httptest.NewRequest("GET", "/", nil))
		close(done)
	}()
	select {
	case <-done:
		if rec.Code != http.StatusGatewayTimeout {
			t.Fatalf("status = %d, want 504", rec.Code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout's request goroutine never returned: expire is waiting on hijackDone forever")
	}
}

// --- 2. Metrics records the handler's status for a panic Compress kept
// unsent; Recovery sends 500.

func TestMetricsPanickedStatusIsWhatTheClientGot(t *testing.T) {
	m := NewMetrics()
	h := RecoveryFn(nil)(Compress()(MetricsMiddleware(m)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("partial"))
		panic("boom")
	}))))
	req := httptest.NewRequest("POST", "/x", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("client status = %d", rec.Code)
	}
	mr := httptest.NewRecorder()
	MetricsHandler(m).ServeHTTP(mr, httptest.NewRequest("GET", "/metrics", nil))
	out := mr.Body.String()
	if !strings.Contains(out, `status="500"`) {
		t.Fatalf("client got 500, metrics recorded:\n%s", grepLines(out, "http_requests_total"))
	}
}

func grepLines(s, sub string) string {
	var b strings.Builder
	for _, l := range strings.Split(s, "\n") {
		if strings.Contains(l, sub) {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// --- 3. handlerPanic hides the panicked error from errors.Is/As in any
// recover outside Timeout that is not this package's Recovery.

var errSentinel = errors.New("sentinel")

func TestTimeoutPanicUnwrapsToHandlerError(t *testing.T) {
	var got any
	outer := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() { got = recover() }()
			next.ServeHTTP(w, r)
		})
	}
	h := outer(Timeout(time.Second)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(errSentinel)
	})))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	err, ok := got.(error)
	if !ok || !errors.Is(err, errSentinel) {
		t.Fatalf("outer recover got %T %v; errors.Is(sentinel) = false", got, got)
	}
}

// --- 4. An invalid status code marks the response started before
// net/http's own panic, so Recovery cuts instead of sending its 500.

func TestRecoveryInvalidStatusStill500(t *testing.T) {
	srv := httptest.NewServer(RecoveryFn(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(0) // a handler bug: net/http panics before sending anything
	})))
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("connection cut although nothing was sent: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 500 {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

var _ = context.Background

// --- 5. Postgres: a non-UTF-8 Idempotency-Key (net/http admits obs-text
// bytes in header values) is refused by TEXT, and the middleware answers
// "store down" 503 for a client's malformed key.

func TestSQLIdempotencyPostgres_NonUTF8KeyIs400(t *testing.T) {
	db, table := pgIdemTable(t)
	s, err := NewSQLIdempotencyStore(db, WithSQLIdempotencyTable(table))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_, _, berr := s.Begin(context.Background(), idemStoreKey("alice", "", "", "k\xff"), "fp")
	t.Logf("Begin err = %v", berr)
	h := Idempotency(IdempotencyConfig{Store: s, Principal: func(*http.Request) string { return "alice" }})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }))
	req := httptest.NewRequest("POST", "/orders", strings.NewReader("{}"))
	req.Header.Set(IdempotencyKeyHeader, "order-\xff")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a client's non-UTF-8 key answered %d, want 400 (not 503 store-down); Begin err = %v", rec.Code, berr)
	}
	if !errors.Is(berr, errKeyNotUTF8) {
		t.Fatalf("Begin with a non-UTF-8 store key: err %v, want errKeyNotUTF8 before any query", berr)
	}
}
