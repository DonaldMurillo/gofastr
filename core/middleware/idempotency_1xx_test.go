package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A handler that sends 103 Early Hints before its 201 still has the 201
// recorded: a retry of the key replays it instead of running the write
// again.
func TestIdempotencyRecordsPastEarlyHints(t *testing.T) {
	var calls atomic.Int32
	h := Idempotency(IdempotencyConfig{Principal: testPrincipal})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Link", "</app.css>; rel=preload; as=style")
		w.WriteHeader(http.StatusEarlyHints)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":7}`))
	}))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	send := func() (int, string, string) {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/orders", strings.NewReader(`{"qty":1}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(IdempotencyKeyHeader, "hints-1")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, string(body), resp.Header.Get("Idempotent-Replay")
	}
	if code, body, _ := send(); code != http.StatusCreated || body != `{"id":7}` {
		t.Fatalf("first: %d %q", code, body)
	}
	code, body, replay := send()
	if code != http.StatusCreated || body != `{"id":7}` || replay != "true" {
		t.Fatalf("retry: %d %q replay=%q, want the recorded 201", code, body, replay)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
}
