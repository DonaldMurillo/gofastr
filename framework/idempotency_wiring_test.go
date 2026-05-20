package framework

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// withTestRoute mounts a single POST handler that increments a counter
// and returns the supplied response so a test can probe replay vs.
// re-execution.
func withTestRoute(a *App, counter *int32, status int, body string) {
	a.Router().Post("/r", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(counter, 1)
		// Drain body so the test sees the recorder's behaviour, not a
		// leak through HTTPServer keep-alive.
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

func TestWithIdempotency_OmittedMeansNoExtraMiddleware(t *testing.T) {
	a := NewApp()
	var calls int32
	withTestRoute(a, &calls, http.StatusCreated, "ok")
	// Without idempotency wired, two identical POSTs hit the handler
	// twice — same as before.
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/r", strings.NewReader("{}"))
		req.Header.Set("Idempotency-Key", "x")
		rr := httptest.NewRecorder()
		a.Router().ServeHTTP(rr, req)
	}
	if calls != 2 {
		t.Fatalf("no-idempotency: expected 2 calls, got %d", calls)
	}
}

func TestWithIdempotency_RepliesCachedResponse(t *testing.T) {
	a := NewApp(WithIdempotency(middleware.IdempotencyConfig{}))
	var calls int32
	withTestRoute(a, &calls, http.StatusCreated, `{"id":99}`)

	send := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/r", strings.NewReader(`{"q":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "stable-key")
		rr := httptest.NewRecorder()
		a.Router().ServeHTTP(rr, req)
		return rr
	}

	first := send()
	if first.Code != http.StatusCreated {
		t.Fatalf("first: %d", first.Code)
	}
	second := send()
	if second.Code != http.StatusCreated {
		t.Fatalf("replay: %d", second.Code)
	}
	if second.Header().Get("Idempotent-Replay") != "true" {
		t.Fatalf("expected Idempotent-Replay header on replay")
	}
	if calls != 1 {
		t.Fatalf("handler should run once, ran %d times", calls)
	}
}

func TestWithIdempotency_PanicsWithWithoutDefaultMiddleware(t *testing.T) {
	// WithIdempotency wires the middleware into the default chain;
	// combined with WithoutDefaultMiddleware it would silently no-op.
	// The framework now panics so the misconfiguration is loud.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic when WithIdempotency is paired with WithoutDefaultMiddleware")
		}
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "WithoutDefaultMiddleware") {
			t.Fatalf("panic message should mention the conflict; got %v", r)
		}
	}()
	_ = NewApp(WithoutDefaultMiddleware(), WithIdempotency(middleware.IdempotencyConfig{}))
}

// Compile-time assertion that router.Middleware is the adapter shape
// the wiring uses — guards against accidental refactor.
var _ router.Middleware = router.Middleware(middleware.Idempotency(middleware.IdempotencyConfig{}))
