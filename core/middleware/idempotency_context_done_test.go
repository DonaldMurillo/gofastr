package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// A handler that returns without writing leaves the recorder at net/http's
// implicit 200. That is a response when the request is still live, and no
// response at all when its context has ended (a deadline, a client that
// hung up): the handler gave up. The middleware used to cache the second
// as "200, empty body" and replay it to the retry for the whole TTL.
//
// Pinned here without a clock: nothing is cached for an unwritten response
// once the context is done; an unwritten response on a live request, and a
// 2xx the handler wrote after its context ended, are both cached.
func TestIdempotencyUnwrittenResponseAndEndedContext(t *testing.T) {
	cases := []struct {
		name       string
		cancelled  bool // the first request's context ends before the handler returns
		writes     bool // the handler writes 201 + body
		wantReplay bool // the same key again is answered from the cache
	}{
		{name: "nothing written, context ended: released", cancelled: true, writes: false, wantReplay: false},
		{name: "nothing written, live request: implicit 200 cached", cancelled: false, writes: false, wantReplay: true},
		{name: "201 written after the context ended: cached", cancelled: true, writes: true, wantReplay: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var runs atomic.Int32
			h := Idempotency(IdempotencyConfig{Principal: testPrincipal})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				runs.Add(1)
				if tc.writes {
					w.WriteHeader(http.StatusCreated)
					_, _ = w.Write([]byte("created"))
				}
			}))
			send := func(ctx context.Context) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"a":1}`)).WithContext(ctx)
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set(IdempotencyKeyHeader, "order-1")
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, req)
				return rr
			}

			ctx, cancel := context.WithCancel(context.Background())
			if tc.cancelled {
				cancel()
			}
			send(ctx)
			cancel()

			retry := send(context.Background())
			replayed := retry.Header().Get("Idempotent-Replay") == "true"
			if replayed != tc.wantReplay {
				t.Fatalf("retry replayed = %v (status %d, handler runs %d), want %v",
					replayed, retry.Code, runs.Load(), tc.wantReplay)
			}
			wantRuns := int32(2)
			if tc.wantReplay {
				wantRuns = 1
			}
			if got := runs.Load(); got != wantRuns {
				t.Fatalf("handler ran %d times, want %d", got, wantRuns)
			}
			if tc.wantReplay && tc.writes && (retry.Code != http.StatusCreated || retry.Body.String() != "created") {
				t.Fatalf("replay = %d %q, want the handler's 201 \"created\"", retry.Code, retry.Body.String())
			}
		})
	}
}
