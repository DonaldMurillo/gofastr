package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Found by the 2026-11-01 battery/auth bug bash: every same-name session
// cookie on a request cost a session-store query, and net/http parses up
// to 3000 cookies, so one anonymous request with a 42 KB Cookie header
// drove 2999 queries through the app-wide SessionMiddleware (and two per
// cookie on logout).

type countingGetStore struct {
	SessionStore
	gets atomic.Int64
}

func (c *countingGetStore) Get(ctx context.Context, tok string) (*Session, error) {
	c.gets.Add(1)
	return c.SessionStore.Get(ctx, tok)
}

func manyCookies(n int, distinct bool) string {
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteString("; ")
		}
		if distinct {
			fmt.Fprintf(&b, "session_id=x%d", i)
		} else {
			b.WriteString("session_id=x")
		}
	}
	return b.String()
}

func TestSessionCookieFanoutBounded(t *testing.T) {
	for _, distinct := range []bool{false, true} {
		store := &countingGetStore{SessionStore: NewMemorySessionStore()}
		mgr := New(AuthConfig{DevMode: true, UserStore: newMemoryUserStore(), SessionStore: store})
		h := SessionMiddleware(mgr, WithSessionLogger(nil))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Cookie", manyCookies(2999, distinct))
		h.ServeHTTP(httptest.NewRecorder(), req)
		want := int64(maxSessionCookieCandidates)
		if !distinct {
			want = 1
		}
		if got := store.gets.Load(); got > want {
			t.Errorf("distinct=%v: one anonymous request drove %d session-store lookups, want at most %d", distinct, got, want)
		}
	}
}

// A real session behind a stale same-name cookie still resolves.
func TestSessionCookieCandidatesKeepOrder(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Cookie", "other=1; session_id=a; session_id=a; session_id=b; session_id=")
	got := sessionCookieCandidates(req, "session_id")
	if strings.Join(got, ",") != "a,b" {
		t.Errorf("candidates = %v, want [a b]", got)
	}
}
