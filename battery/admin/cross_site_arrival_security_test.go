package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// arrivalEnv is a gated admin behind auth.SessionMiddleware, with one
// signed-in admin's session cookie value.
func arrivalEnv(t *testing.T, cfg Config) (http.Handler, string) {
	t.Helper()
	var mgr *auth.AuthManager
	var cookie string
	cfg.Entities = []string{"posts"}
	x := setup(t, map[string]entity.EntityConfig{"posts": postsConfig()}, cfg, func(x *env, c *Config) {
		ctx := context.Background()
		users := auth.NewEntityUserStore(x.db, "users")
		if err := users.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		u, err := users.CreateUser(ctx, "ada@example.com", "x", []string{"admin"})
		if err != nil {
			t.Fatal(err)
		}
		mgr = auth.New(auth.AuthConfig{JWTSecret: "test-secret", UserStore: users, DevMode: true})
		sess, err := mgr.SessionStore().Create(ctx, u.GetID(), time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		cookie = sess.Token
		c.Auth = mgr
	})
	return auth.SessionMiddleware(mgr, auth.WithSessionLogger(nil))(x.h), cookie
}

func arrive(h http.Handler, path, site, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Sec-Fetch-Site", site)
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Dest", "document")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: cookie})
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// A browser withholds the SameSite=Strict session cookie from a link
// followed on another site, so the gates read a signed-in admin as
// signed out (issue 696). The screen gate (/admin/audit) and the HTTP
// gate (/admin/_count) both answer that arrival with the same-site hop.
func TestCrossSiteArrivalIsNotSentToLogin(t *testing.T) {
	for _, cfg := range []Config{{LoginPath: "/login"}, {}} {
		h, cookie := arrivalEnv(t, cfg)
		for _, path := range []string{"/admin/audit?actor=u1", "/admin/_count/posts?x=1"} {
			rr := arrive(h, path, "cross-site", "")
			if loc := rr.Header().Get("Location"); loc != "" {
				t.Fatalf("LoginPath %q: a cross-site arrival at %s was sent to %q", cfg.LoginPath, path, loc)
			}
			if got := rr.Header().Get("Refresh"); rr.Code != http.StatusOK || got != "0;url="+path {
				t.Fatalf("LoginPath %q: GET %s = %d Refresh %q, want a reload of the same page from this origin", cfg.LoginPath, path, rr.Code, got)
			}
			// The reload starts on this origin and carries the cookie.
			if rr := arrive(h, path, "same-origin", cookie); rr.Code != http.StatusOK || rr.Header().Get("Refresh") != "" {
				t.Fatalf("LoginPath %q: the reload of %s with the session = %d Refresh %q, want the page", cfg.LoginPath, path, rr.Code, rr.Header().Get("Refresh"))
			}
		}
	}
}

// The reload is same-origin, so it never hops again: a visitor who is
// really signed out gets the login redirect with next intact, or the 401.
func TestCrossSiteArrivalReloadSignedOutGetsLogin(t *testing.T) {
	h, _ := arrivalEnv(t, Config{LoginPath: "/login"})
	for path, next := range map[string]string{
		"/admin/audit?actor=u1":   "/login?next=%2Fadmin%2Faudit%3Factor%3Du1",
		"/admin/_count/posts?x=1": "/login?next=%2Fadmin%2F_count%2Fposts%3Fx%3D1",
	} {
		rr := arrive(h, path, "same-origin", "")
		if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != next || rr.Header().Get("Refresh") != "" {
			t.Fatalf("signed-out reload of %s = %d Location %q Refresh %q, want 303 %s", path, rr.Code, rr.Header().Get("Location"), rr.Header().Get("Refresh"), next)
		}
	}
	h, _ = arrivalEnv(t, Config{})
	if rr := arrive(h, "/admin/audit", "same-origin", ""); rr.Code != http.StatusUnauthorized || rr.Header().Get("Refresh") != "" {
		t.Fatalf("signed-out reload with no LoginPath = %d Refresh %q, want 401", rr.Code, rr.Header().Get("Refresh"))
	}
}
