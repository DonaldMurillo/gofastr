package auth

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeSessionStore lets us drive distinct failure modes per test.
type fakeSessionStore struct {
	get func(ctx context.Context, token string) (*Session, error)
}

func (f *fakeSessionStore) Create(ctx context.Context, userID string, ttl time.Duration) (*Session, error) {
	return nil, errors.New("not used")
}
func (f *fakeSessionStore) Get(ctx context.Context, token string) (*Session, error) {
	return f.get(ctx, token)
}
func (f *fakeSessionStore) Delete(ctx context.Context, token string) error { return nil }
func (f *fakeSessionStore) Cleanup(ctx context.Context) (int, error)       { return 0, nil }

// fakeUserStore is wired to fail FindByID so we can observe the "store
// outage indistinguishable from logged-out" warning.
type fakeUserStore struct {
	findByID func(ctx context.Context, id string) (User, error)
}

func (f *fakeUserStore) FindByEmail(ctx context.Context, email string) (User, string, error) {
	return nil, "", ErrUserNotFound
}
func (f *fakeUserStore) FindByID(ctx context.Context, id string) (User, error) {
	return f.findByID(ctx, id)
}
func (f *fakeUserStore) CreateUser(ctx context.Context, email, hash string, roles []string) (User, error) {
	return nil, errors.New("not used")
}

// TestSessionMiddleware_LogsStoreError pins the visibility fix: when the
// session store returns a non-not-found error (DB down, connection
// dropped, context cancelled), the middleware logs a WARN line so the
// operator can distinguish "users are logged out" from "auth DB is
// dead." The middleware still falls through to anonymous (don't take
// the whole app offline) — but the line in the log is the difference
// between a 5-minute and a 50-minute incident response.
func TestSessionMiddleware_LogsStoreError(t *testing.T) {
	var sink bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&sink, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mgr := newTestManagerWithStores(t,
		&fakeSessionStore{get: func(_ context.Context, _ string) (*Session, error) {
			return nil, errors.New("connection refused")
		}},
		// Use a stub UserStore so the middleware constructor accepts
		// the mgr; the test never reaches the user-lookup path because
		// the SessionStore errors first.
		&fakeUserStore{findByID: func(_ context.Context, _ string) (User, error) { return nil, ErrUserNotFound }},
	)

	mw := SessionMiddleware(mgr, WithSessionLogger(logger))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: "anything"})
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	out := sink.String()
	if !strings.Contains(strings.ToLower(out), "session") {
		t.Errorf("expected session-related log line, got: %q", out)
	}
	if !strings.Contains(strings.ToLower(out), "warn") {
		t.Errorf("expected WARN level for store error, got: %q", out)
	}
	if !strings.Contains(out, "connection refused") {
		t.Errorf("expected underlying error in log, got: %q", out)
	}
}

// TestSessionMiddleware_LogsUserStoreError verifies a non-not-found
// error from FindByID is also visible.
func TestSessionMiddleware_LogsUserStoreError(t *testing.T) {
	var sink bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&sink, &slog.HandlerOptions{Level: slog.LevelDebug}))

	sess := &Session{Token: "tok", UserID: "u1", ExpiresAt: time.Now().Add(time.Hour)}
	mgr := newTestManagerWithStores(t,
		&fakeSessionStore{get: func(_ context.Context, _ string) (*Session, error) { return sess, nil }},
		&fakeUserStore{findByID: func(_ context.Context, _ string) (User, error) {
			return nil, errors.New("db pool drained")
		}},
	)

	mw := SessionMiddleware(mgr, WithSessionLogger(logger))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: "tok"})
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	if !strings.Contains(sink.String(), "db pool drained") {
		t.Errorf("user-store error not logged: %q", sink.String())
	}
}

// TestSessionMiddleware_NotFoundIsNotLouder confirms the legit "session
// expired / user deleted" path stays at DEBUG, not WARN. We don't want
// to spam the logs on every visitor without a cookie.
func TestSessionMiddleware_NotFoundIsNotLouder(t *testing.T) {
	var sink bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&sink, &slog.HandlerOptions{Level: slog.LevelWarn}))

	mgr := newTestManagerWithStores(t,
		&fakeSessionStore{get: func(_ context.Context, _ string) (*Session, error) {
			return nil, ErrSessionNotFound
		}},
		&fakeUserStore{findByID: func(_ context.Context, _ string) (User, error) { return nil, ErrUserNotFound }},
	)

	mw := SessionMiddleware(mgr, WithSessionLogger(logger))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: mgr.Config().SessionCookie, Value: "expired"})
	mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})).ServeHTTP(rec, req)

	if sink.Len() > 0 {
		t.Errorf("expired session emitted WARN-or-above noise: %q", sink.String())
	}
}

// TestSessionMiddleware_PanicsOnNilUserStore pins the construction-time
// guard. A SessionMiddleware against a misconfigured AuthManager (no
// UserStore) is a no-op-with-logging — operators should hit the panic
// at startup, not discover the misconfig in production via missing
// user sessions.
func TestSessionMiddleware_PanicsOnNilUserStore(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when SessionMiddleware constructed against nil-UserStore mgr")
		}
	}()
	// Build a manager with NO UserStore configured.
	mgr := New(AuthConfig{
		JWTSecret:    "x",
		SessionStore: &fakeSessionStore{get: func(ctx context.Context, t string) (*Session, error) { return nil, ErrSessionNotFound }},
	})
	_ = SessionMiddleware(mgr) // must panic
}

func TestSessionMiddleware_PanicsOnNilManager(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when SessionMiddleware constructed with nil mgr")
		}
	}()
	_ = SessionMiddleware(nil)
}

func newTestManagerWithStores(t *testing.T, sess SessionStore, user UserStore) *AuthManager {
	t.Helper()
	mgr := New(AuthConfig{
		JWTSecret:     "test-secret",
		UserStore:     user,
		SessionStore:  sess,
		DevMode:       true,
		SessionTTL:    time.Hour,
		SessionCookie: "test_session",
	})
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("mgr.Init: %v", err)
	}
	return mgr
}
