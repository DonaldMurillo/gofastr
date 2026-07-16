package auth

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// A jar can hold several cookies with the session name at once: a stale
// cookie from an old deployment at a more specific Path, or another
// localhost port's cookie (localhost cookies ignore ports). Browsers send
// the most path-specific first, and r.Cookie returns only that one — so a
// dead cookie shadows a live session and login silently fails while a valid
// cookie sits one position later. Every session read must try ALL
// candidates. (Found live in a host app; see its fix this ports upstream.)
func TestSessionReadsTryEveryCookieCandidate(t *testing.T) {
	mgr, db := newShadowTestMgr(t)
	_ = db

	user, err := mgr.UserStore().(*EntityUserStore).CreateUser(context.Background(), "shadow@example.com", "x", nil)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sess, err := mgr.SessionStore().Create(context.Background(), user.GetID(), time.Hour)
	if err != nil {
		t.Fatalf("session create: %v", err)
	}
	cookieName := mgr.Config().SessionCookie

	// Dead cookie FIRST (the browser's most-specific-path ordering), live second.
	shadowed := func(r *http.Request) *http.Request {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: "stale-token-from-a-dead-deployment"})
		r.AddCookie(&http.Cookie{Name: cookieName, Value: sess.Token})
		return r
	}

	t.Run("SessionMiddleware", func(t *testing.T) {
		var seen User
		h := SessionMiddleware(mgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = GetCurrentUser(r.Context())
		}))
		h.ServeHTTP(httptest.NewRecorder(), shadowed(httptest.NewRequest(http.MethodGet, "/", nil)))
		if seen == nil || seen.GetID() != user.GetID() {
			t.Fatalf("valid session shadowed by stale duplicate cookie: got %+v", seen)
		}
	})

	t.Run("me handler", func(t *testing.T) {
		core := NewCorePlugin()
		if err := core.Init(mgr); err != nil {
			t.Fatalf("core init: %v", err)
		}
		rr := httptest.NewRecorder()
		core.meHandler()(rr, shadowed(httptest.NewRequest(http.MethodGet, "/auth/me", nil)))
		if rr.Code != http.StatusOK {
			t.Fatalf("me: valid session shadowed by stale duplicate: %d %s", rr.Code, rr.Body.String())
		}
	})

	t.Run("logout revokes every candidate", func(t *testing.T) {
		// A second live session for the same user — logout must revoke both
		// cookies' sessions, not just the first, or a shadowed-but-valid
		// session survives logout.
		sess2, err := mgr.SessionStore().Create(context.Background(), user.GetID(), time.Hour)
		if err != nil {
			t.Fatalf("session create: %v", err)
		}
		core := NewCorePlugin()
		if err := core.Init(mgr); err != nil {
			t.Fatalf("core init: %v", err)
		}
		req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
		req.AddCookie(&http.Cookie{Name: cookieName, Value: sess.Token})
		req.AddCookie(&http.Cookie{Name: cookieName, Value: sess2.Token})
		core.logoutHandler()(httptest.NewRecorder(), req)

		for i, tok := range []string{sess.Token, sess2.Token} {
			if s, err := mgr.SessionStore().Get(context.Background(), tok); err == nil && s != nil {
				t.Errorf("logout left session %d alive — shadowed sessions must be revoked too", i+1)
			}
		}
	})
}

func newShadowTestMgr(t *testing.T) (*AuthManager, *sql.DB) {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "auth-shadow-*.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	db, err := sql.Open("sqlite3", tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mgr := New(AuthConfig{
		JWTSecret:    "shadow-test",
		UserStore:    NewEntityUserStore(db, "users"),
		SessionStore: NewEntitySessionStore(db, "sessions"),
		DevMode:      true,
		SessionTTL:   time.Hour,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("mgr.Init: %v", err)
	}
	return mgr, db
}
