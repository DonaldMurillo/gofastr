package middleware

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A Principal that returns "" for a request caches nothing for it. Every
// such caller shared one namespace, so the documented session-lookup
// Principal (which runs before the app's auth and always returned "")
// replayed one user's stored response to another who sent the same key
// and body (2026-10-27 bug bash P38).
func TestEmptyPrincipalSkipsCache(t *testing.T) {
	mw := Idempotency(IdempotencyConfig{
		Principal: func(r *http.Request) string { return r.Header.Get("X-User") },
	})
	var calls int32
	srv := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "call=%d body=%s", n, body)
	}))
	for i := 1; i <= 2; i++ {
		r := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"sku":"a"}`))
		r.Header.Set(IdempotencyKeyHeader, "order-1")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, r)
		if want := fmt.Sprintf(`call=%d body={"sku":"a"}`, i); w.Body.String() != want {
			t.Fatalf("request %d body = %q, want %q (its own handler run)", i, w.Body.String(), want)
		}
		if got := w.Header().Get("Idempotent-Replay"); got != "" {
			t.Fatalf("request %d Idempotent-Replay = %q, want none", i, got)
		}
	}
}
