package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofastr/gofastr/core/router"
)

// fakeUser is the minimal User impl for these tests.
type fakeUser struct {
	id, email string
	roles     []string
}

func (u fakeUser) GetID() string      { return u.id }
func (u fakeUser) GetEmail() string   { return u.email }
func (u fakeUser) GetRoles() []string { return u.roles }

// fakeUserRepo serves one user keyed by email with a known password.
type fakeUserRepo struct {
	users map[string]struct {
		user User
		hash string
	}
}

func (r *fakeUserRepo) FindByEmail(_ context.Context, email string) (User, string, error) {
	entry, ok := r.users[email]
	if !ok {
		return nil, "", ErrInvalidCredentials
	}
	return entry.user, entry.hash, nil
}

func newRepoWithUser(t *testing.T, email, password string) *fakeUserRepo {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return &fakeUserRepo{
		users: map[string]struct {
			user User
			hash string
		}{
			email: {user: fakeUser{id: "u1", email: email, roles: []string{"reader"}}, hash: hash},
		},
	}
}

// ============================================================================
// Login → cookie set; bad credentials → 401
// ============================================================================

func TestSession_Login_SetsCookie(t *testing.T) {
	r := router.New()
	store := NewMemorySessionStore()
	repo := newRepoWithUser(t, "alice@x.com", "hunter2")
	MountAuthRoutes(r, store, repo, SessionConfig{})

	body, _ := json.Marshal(map[string]string{"email": "alice@x.com", "password": "hunter2"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Header().Get("Set-Cookie"), "session_id=") {
		t.Fatalf("expected session cookie, got %q", w.Header().Get("Set-Cookie"))
	}
}

func TestSession_Login_BadPasswordReturns401(t *testing.T) {
	r := router.New()
	store := NewMemorySessionStore()
	repo := newRepoWithUser(t, "alice@x.com", "hunter2")
	MountAuthRoutes(r, store, repo, SessionConfig{})

	body, _ := json.Marshal(map[string]string{"email": "alice@x.com", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "alice") {
		t.Fatal("error body must not leak the email")
	}
}

// ============================================================================
// SessionMiddleware hydrates the session into ctx
// ============================================================================

func TestSession_MiddlewareHydratesCtx(t *testing.T) {
	store := NewMemorySessionStore()
	sess, _ := store.Create(context.Background(), "u1", time.Minute)

	var seenUserID string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s, ok := SessionFromContext(r.Context()); ok {
			seenUserID = s.UserID
		}
		w.WriteHeader(http.StatusOK)
	})
	h := SessionMiddleware(store, "")(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sess.Token})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if seenUserID != "u1" {
		t.Fatalf("expected ctx user u1, got %q", seenUserID)
	}
}

// ============================================================================
// RequireSession blocks unauthenticated requests
// ============================================================================

func TestSession_RequireSession_Blocks(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RequireSession()(inner)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// ============================================================================
// Logout invalidates the session
// ============================================================================

func TestSession_LogoutClearsSession(t *testing.T) {
	r := router.New()
	store := NewMemorySessionStore()
	repo := newRepoWithUser(t, "alice@x.com", "hunter2")
	MountAuthRoutes(r, store, repo, SessionConfig{})

	// Login
	body, _ := json.Marshal(map[string]string{"email": "alice@x.com", "password": "hunter2"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("login: %d", w.Code)
	}
	// Extract the cookie
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "session_id" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	if _, err := store.Get(context.Background(), cookie.Value); err != nil {
		t.Fatalf("session should exist post-login: %v", err)
	}

	// Logout
	logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logoutReq.AddCookie(cookie)
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, logoutReq)
	if w2.Code != http.StatusNoContent {
		t.Fatalf("logout: expected 204, got %d", w2.Code)
	}
	if _, err := store.Get(context.Background(), cookie.Value); err == nil {
		t.Fatal("session should be deleted after logout")
	}
}

// ============================================================================
// Cleanup removes expired sessions
// ============================================================================

func TestSession_Cleanup_RemovesExpired(t *testing.T) {
	store := NewMemorySessionStore()
	// Create one expired, one fresh.
	s1, _ := store.Create(context.Background(), "u1", time.Hour)
	s2, _ := store.Create(context.Background(), "u2", time.Hour)
	// Force-expire s1.
	s1.ExpiresAt = time.Now().Add(-time.Hour)

	n, err := store.Cleanup(context.Background())
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 reaped, got %d", n)
	}
	if _, err := store.Get(context.Background(), s2.Token); err != nil {
		t.Fatalf("fresh session should remain: %v", err)
	}
}
