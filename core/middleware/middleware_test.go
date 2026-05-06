package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Chain ordering
// ---------------------------------------------------------------------------

func TestChainOrder(t *testing.T) {
	var order []string
	record := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, "pre-"+name)
				next.ServeHTTP(w, r)
				order = append(order, "post-"+name)
			})
		}
	}

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	})

	handler := Chain(record("A"), record("B"), record("C"))(final)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	expected := []string{"pre-A", "pre-B", "pre-C", "handler", "post-C", "post-B", "post-A"}
	if len(order) != len(expected) {
		t.Fatalf("expected %d entries, got %d: %v", len(expected), len(order), order)
	}
	for i, e := range expected {
		if order[i] != e {
			t.Errorf("position %d: expected %q, got %q", i, e, order[i])
		}
	}
}

func TestChainEmpty(t *testing.T) {
	called := false
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	handler := Chain()(final)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("handler was not called")
	}
}

// ---------------------------------------------------------------------------
// Pipeline
// ---------------------------------------------------------------------------

func TestPipelineBuild(t *testing.T) {
	var order []string
	record := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		order = append(order, "final")
	})

	p := NewPipeline(record("a"), record("b"))
	p.Use(record("c"))
	handler := p.Build(final)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	expected := []string{"a", "b", "c", "final"}
	if len(order) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, order)
	}
	for i, e := range expected {
		if order[i] != e {
			t.Errorf("position %d: expected %q, got %q", i, e, order[i])
		}
	}
}

// ---------------------------------------------------------------------------
// RequestID
// ---------------------------------------------------------------------------

func TestRequestIDGenerates(t *testing.T) {
	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id == "" {
			t.Error("expected non-empty request ID in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	hdr := rec.Header().Get(HeaderRequestID)
	if hdr == "" {
		t.Error("expected X-Request-ID response header")
	}
	if len(hdr) != 36 { // 8-4-4-4-12
		t.Errorf("expected UUID format, got %q", hdr)
	}
}

func TestRequestIDReusesIncoming(t *testing.T) {
	incoming := "test-id-123"
	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id != incoming {
			t.Errorf("expected %q, got %q", incoming, id)
		}
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderRequestID, incoming)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get(HeaderRequestID); got != incoming {
		t.Errorf("response header: expected %q, got %q", incoming, got)
	}
}

func TestRequestIDUnique(t *testing.T) {
	ids := make(map[string]bool)
	mu := sync.Mutex{}
	handler := RequestID()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		mu.Lock()
		ids[id] = true
		mu.Unlock()
	}))

	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	if len(ids) != 100 {
		t.Errorf("expected 100 unique IDs, got %d", len(ids))
	}
}

// ---------------------------------------------------------------------------
// Logging
// ---------------------------------------------------------------------------

func TestLoggingOutput(t *testing.T) {
	var buf bytes.Buffer
	handler := LoggingWithWriter(&buf)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "/test/path", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	output := buf.String()
	if output == "" {
		t.Fatal("expected log output")
	}

	// Should be valid JSON
	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("output is not valid JSON: %s\nraw: %q", err, output)
	}

	if entry["method"] != "POST" {
		t.Errorf("expected method POST, got %v", entry["method"])
	}
	if entry["path"] != "/test/path" {
		t.Errorf("expected path /test/path, got %v", entry["path"])
	}
	// status is logged as a number via slog
	if entry["status"] != float64(http.StatusCreated) {
		t.Errorf("expected status 201, got %v", entry["status"])
	}
	if entry["duration"] == nil {
		t.Error("expected duration field")
	}
}

// ---------------------------------------------------------------------------
// Recovery
// ---------------------------------------------------------------------------

func TestRecoveryCatchesPanic(t *testing.T) {
	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	handler := Recovery()(panicking)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

func TestRecoveryPassesThrough(t *testing.T) {
	called := false
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := Recovery()(ok)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler was not called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// CORS
// ---------------------------------------------------------------------------

func TestCORSSetsHeaders(t *testing.T) {
	handler := CORS(CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Content-Type"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://example.com" {
		t.Errorf("origin: got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got != "GET, POST" {
		t.Errorf("methods: got %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type" {
		t.Errorf("headers: got %q", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	handler := CORS(CORSConfig{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called for preflight")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/api", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("origin: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Timeout
// ---------------------------------------------------------------------------

func TestTimeoutFastHandler(t *testing.T) {
	handler := Timeout(5 * time.Second)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestTimeoutSlowHandler(t *testing.T) {
	handler := Timeout(50 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected 504, got %d", rec.Code)
	}
}

func TestTimeoutContextCancelled(t *testing.T) {
	ctxErrCh := make(chan error, 1)
	handler := Timeout(50 * time.Millisecond)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		ctxErrCh <- r.Context().Err()
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	select {
	case ctxErr := <-ctxErrCh:
		if ctxErr == nil {
			t.Error("expected context to be cancelled")
		}
		if ctxErr != context.DeadlineExceeded {
			t.Errorf("expected DeadlineExceeded, got %v", ctxErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for context cancellation")
	}
}

// ---------------------------------------------------------------------------
// Integration: full pipeline
// ---------------------------------------------------------------------------

func TestFullPipeline(t *testing.T) {
	var buf bytes.Buffer

	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := GetRequestID(r.Context())
		if id == "" {
			t.Error("expected request ID in context")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	p := NewPipeline(
		Recovery(),
		RequestID(),
		LoggingWithWriter(&buf),
	)
	handler := p.Build(final)

	req := httptest.NewRequest(http.MethodGet, "/hello", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if got := rec.Header().Get(HeaderRequestID); got == "" {
		t.Error("expected X-Request-ID header")
	}
	if !strings.Contains(buf.String(), "GET") {
		t.Error("expected log output to contain GET")
	}
}
