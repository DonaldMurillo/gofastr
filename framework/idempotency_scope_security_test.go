package framework

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

type idemUserKey struct{}

// A session and a narrower API token of the same user send one
// Idempotency-Key, path and body. The token must not get the session's
// stored response: that would hand it what its scopes do not allow. A
// retry under the same scopes still replays, in any scope order.
func TestIdemReplayKeyedOnHeldScopes(t *testing.T) {
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		id, ok := ctx.Value(idemUserKey{}).(string)
		return id, ok && id != ""
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })

	a := NewApp(WithIdempotency(middleware.IdempotencyConfig{}))
	// Stands in for the app's auth: every caller is user u1; an API token
	// narrows the request to the scopes it names.
	a.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := context.WithValue(r.Context(), idemUserKey{}, "u1")
			if s, ok := r.Header["X-Test-Scopes"]; ok {
				var held []string
				if s[0] != "" {
					held = strings.Split(s[0], ",")
				}
				ctx = access.WithHeldScopes(ctx, held)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	})
	var calls int32
	a.Router().Post("/a2a", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		_, _ = io.Copy(io.Discard, r.Body)
		held, scoped := access.HeldScopes(r.Context())
		body := "session-artifacts"
		if scoped {
			body = "scoped-" + strings.Repeat("x", len(held))
		}
		_, _ = w.Write([]byte(body))
	}))
	send := func(scopes *string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/a2a", strings.NewReader(`{"method":"ListTasks"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "list-1")
		if scopes != nil {
			req.Header.Set("X-Test-Scopes", *scopes)
		}
		rr := httptest.NewRecorder()
		a.Router().ServeHTTP(rr, req)
		return rr
	}
	str := func(s string) *string { return &s }
	replayed := func(rr *httptest.ResponseRecorder) bool {
		return rr.Header().Get("Idempotent-Replay") == "true"
	}

	send(nil)
	if rr := send(str("tasks:read")); replayed(rr) || rr.Body.String() != "scoped-x" {
		t.Fatalf("narrow token got %q (replay=%v); its own call must run", rr.Body.String(), replayed(rr))
	}
	if rr := send(str("")); replayed(rr) {
		t.Fatalf("a token holding no scopes replayed %q", rr.Body.String())
	}
	if rr := send(str("b,a")); replayed(rr) {
		t.Fatalf("a two-scope token replayed %q", rr.Body.String())
	}
	if rr := send(str("a,b")); !replayed(rr) || rr.Body.String() != "scoped-xx" {
		t.Fatalf("same scopes, other order: replay=%v body %q", replayed(rr), rr.Body.String())
	}
	if rr := send(nil); !replayed(rr) || rr.Body.String() != "session-artifacts" {
		t.Fatalf("session retry: replay=%v body %q", replayed(rr), rr.Body.String())
	}
	if calls != 4 {
		t.Fatalf("handler ran %d times, want 4", calls)
	}
}
