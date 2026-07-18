package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestEntityStoresPureSQLiteLoginLogoutLifecycle(t *testing.T) {
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatalf("open pure sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	users := NewEntityUserStore(db, "users")
	sessions := NewEntitySessionStore(db, "sessions")
	mgr := New(AuthConfig{
		JWTSecret:     "pure-sqlite-test-secret",
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     users,
		SessionStore:  sessions,
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("init auth: %v", err)
	}

	hash, err := HashPassword("securepass123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if _, err := users.CreateUser(context.Background(), "alice@example.com", hash, []string{"user"}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	r := router.New()
	mgr.RegisterRoutes(r)
	login := authJSONRequest(t, r, http.MethodPost, "/auth/login", map[string]string{
		"email":    "alice@example.com",
		"password": "securepass123",
	}, nil)
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", login.Code, login.Body.String())
	}
	sessionCookie := cookieNamed(login.Result().Cookies(), "session_id")
	if sessionCookie == nil {
		t.Fatal("login did not set durable session cookie")
	}

	var sessionCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions after login: %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("sessions after login = %d, want 1", sessionCount)
	}

	me := authJSONRequest(t, r, http.MethodGet, "/auth/me", nil, sessionCookie)
	if me.Code != http.StatusOK {
		t.Fatalf("me before logout status = %d, body=%s", me.Code, me.Body.String())
	}

	logout := authJSONRequest(t, r, http.MethodPost, "/auth/logout", nil, sessionCookie)
	if logout.Code != http.StatusNoContent && logout.Code != http.StatusOK {
		t.Fatalf("logout status = %d, body=%s", logout.Code, logout.Body.String())
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions after logout: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("sessions after logout = %d, want 0", sessionCount)
	}

	me = authJSONRequest(t, r, http.MethodGet, "/auth/me", nil, sessionCookie)
	if me.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout status = %d, want 401; body=%s", me.Code, me.Body.String())
	}
}

func authJSONRequest(t *testing.T, r http.Handler, method, path string, body any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func cookieNamed(cookies []*http.Cookie, name string) *http.Cookie {
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}
