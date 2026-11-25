package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMemorySessionExpiredIsNotFound(t *testing.T) {
	st := NewMemorySessionStore()
	ctx := context.Background()
	sess, err := st.Create(ctx, "u1", time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := st.Get(ctx, sess.Token); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expired session Get = %v, want ErrSessionNotFound", err)
	}
}

func TestRequireSession_RedirectOnlyForHTML(t *testing.T) {
	h := RequireSession(WithRedirectOnFail("/login"))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	html := httptest.NewRequest(http.MethodGet, "/x", nil)
	html.Header.Set("Accept", "text/html;q=0.9, */*")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, html)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Errorf("HTML request = %d %q, want 303 /login", rec.Code, rec.Header().Get("Location"))
	}
	api := httptest.NewRequest(http.MethodGet, "/x", nil)
	api.Header.Set("Accept", "application/json")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, api)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("JSON request = %d, want 401", rec.Code)
	}
}

func TestRequestSessionPrefersRequestedKind(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", DevMode: true})
	st := mgr.SessionStore().(*MemorySessionStore)
	ctx := context.Background()
	full, _ := st.Create(ctx, "u1", time.Hour)
	pend, _ := st.Create(ctx, "u1", time.Hour)
	_ = st.MarkPendingTwoFactor(ctx, pend.Token)
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: full.Token})
	req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: pend.Token})
	if s, err := mgr.requestSession(req, true); err != nil || s.Token != pend.Token {
		t.Errorf("preferPending picked %+v, %v; want the pending session", s, err)
	}
	if s, err := mgr.requestSession(req, false); err != nil || s.Token != full.Token {
		t.Errorf("non-pending preference picked %+v, %v; want the full session", s, err)
	}
}

func TestRequireSessionMixedCaseHTML(t *testing.T) {
	h := RequireSession(WithRedirectOnFail("/login"))(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	for _, tc := range []struct {
		accept string
		want   int
	}{
		{"application/json, Text/HTML; charset=utf-8", http.StatusSeeOther},
		{"Application/XHTML+XML", http.StatusSeeOther},
		{"text/html;q=0", http.StatusSeeOther},
		{`application/json; profile="unterminated, text/html`, http.StatusSeeOther},
		{"", http.StatusUnauthorized},
		{"*/*", http.StatusUnauthorized},
	} {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Accept", tc.accept)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("Accept %q: status = %d, want %d", tc.accept, rec.Code, tc.want)
		}
	}
}
