package middleware

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// finishErrorStore is a first-writer store whose Finish always fails — the
// DB-blip case: Begin claims the key, the handler runs and answers the
// client, then persisting the cached response strands the claim.
type finishErrorStore struct{}

func (finishErrorStore) Begin(context.Context, string, string) (*IdempotentResponse, bool, error) {
	return nil, true, nil // first writer; middleware must call Finish
}
func (finishErrorStore) Finish(context.Context, string, string, *IdempotentResponse) error {
	return errors.New("finish failed: db down")
}

// TestIdempotency_FinishErrorLogged pins fix #3: a Finish failure must produce
// a log record (the claim is stranded — retries of the same key 409 until the
// entry TTLs — and the failure is otherwise invisible). Critically, the client
// still receives its original response unchanged: this is observability, not a
// control-flow change.
func TestIdempotency_FinishErrorLogged(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))

	h := Idempotency(IdempotencyConfig{
		Store:     finishErrorStore{},
		Principal: testPrincipal,
		Logger:    logger,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))

	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"a":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(IdempotencyKeyHeader, "order-7")
	req.Header.Set("X-Caller", "alice")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Response semantics MUST be unchanged: the client already got its answer
	// before Finish ran.
	if rec.Code != http.StatusCreated {
		t.Fatalf("response status changed by Finish error: got %d, want 201", rec.Code)
	}
	if rec.Body.String() != "created" {
		t.Fatalf("response body changed by Finish error: got %q", rec.Body.String())
	}
	got := buf.String()
	if !strings.Contains(strings.ToLower(got), "finish") {
		t.Fatalf("Finish failure was not logged; got=%q", got)
	}
}
