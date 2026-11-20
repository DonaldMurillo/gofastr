package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type failingCreateStore struct{ *memoryUserStore }

func (s failingCreateStore) CreateUser(context.Context, string, string, []string) (User, error) {
	return nil, errors.New("db down")
}

type panickingSender struct{ done chan struct{} }

func (s *panickingSender) Send(context.Context, string, string) error {
	defer close(s.done)
	panic("sender exploded")
}

type failingCreateSessions struct{ *MemorySessionStore }

func (failingCreateSessions) Create(context.Context, string, time.Duration) (*Session, error) {
	return nil, errors.New("session store down")
}

type swappedOwnerStore struct {
	*memoryUserStore
	calls int
}

func (s *swappedOwnerStore) FindByEmail(ctx context.Context, email string) (User, string, error) {
	u, h, err := s.memoryUserStore.FindByEmail(ctx, email)
	s.calls++
	if s.calls > 1 && err == nil {
		return &BasicUser{ID: "someone-else", Email: email}, h, nil
	}
	return u, h, err
}

func loginJSON(t *testing.T, mgr *AuthManager, email, pw string) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"email": email, "password": pw})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mountRoutes(mgr).ServeHTTP(rec, req)
	return rec
}

func TestLoginCrossSitePostsDontBurnLoginBudget(t *testing.T) {
	store := newMemoryUserStore()
	seedUser(t, store, "victim@example.com", "supersecret1")
	mgr := New(AuthConfig{JWTSecret: "k", SessionTTL: time.Hour, UserStore: store, AllowInMemoryStores: true,
		LoginRateLimit: &RateLimiterConfig{MaxAttempts: 2, Window: time.Minute, BlockDuration: time.Minute}})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	r := mountRoutes(mgr)
	for range 5 {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader("email=victim%40example.com&password=x"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "https://evil.example")
		req.Host = "app.example"
		req.RemoteAddr = "203.0.113.9:4444"
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	b, _ := json.Marshal(map[string]string{"email": "victim@example.com", "password": "supersecret1"})
	req := httptest.NewRequest(http.MethodPost, "/auth/login", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.9:4444"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("own login after refused cross-site posts = %d, want 200", rec.Code)
	}
}

func TestSessionCookieAttributes(t *testing.T) {
	cfg := AuthConfig{SessionCookie: "__Host-session", SessionSecure: true}
	rec := httptest.NewRecorder()
	exp := time.Now().Add(time.Hour)
	mintSessionCookie(rec, cfg, "tok", exp)
	c := rec.Result().Cookies()[0]
	if !c.HttpOnly || !c.Secure || c.Path != "/" || c.SameSite != http.SameSiteStrictMode || c.Domain != "" || c.Value != "tok" {
		t.Errorf("session cookie = %+v, want HttpOnly Secure Path=/ SameSite=Strict no Domain", c)
	}
	rec = httptest.NewRecorder()
	mintSessionCookie(rec, cfg, "", time.Time{})
	c = rec.Result().Cookies()[0]
	if c.MaxAge >= 0 || !c.Expires.Before(time.Unix(1, 0)) || !c.HttpOnly || !c.Secure {
		t.Errorf("logout cookie = %+v, want MaxAge<0, epoch Expires and the same flags", c)
	}
}

func TestLoginSessionCreateFailureIsNotATwoFactorFailure(t *testing.T) {
	store := newMemoryUserStore()
	seedUser(t, store, "a@example.com", "supersecret1")
	sink := &capturingSink{}
	mgr := New(AuthConfig{JWTSecret: "s", DevMode: true, UserStore: store, AuditSink: sink,
		SessionStore: failingCreateSessions{NewMemorySessionStore()}})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	if rec := loginJSON(t, mgr, "a@example.com", "supersecret1"); rec.Code != http.StatusInternalServerError {
		t.Errorf("login on a failing session store = %d, want 500", rec.Code)
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, ev := range sink.events {
		if ev.Meta["reason"] == "twofa_failclosed" {
			t.Error("a session-store failure was audited as a two-factor failure")
		}
	}
}

func TestLoginRefusedWhenEmailNowNamesAnotherUser(t *testing.T) {
	inner := newMemoryUserStore()
	seedUser(t, inner, "a@example.com", "supersecret1")
	mgr := New(AuthConfig{JWTSecret: "s", DevMode: true, UserStore: &swappedOwnerStore{memoryUserStore: inner}})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	if rec := loginJSON(t, mgr, "a@example.com", "supersecret1"); rec.Code != http.StatusUnauthorized {
		t.Errorf("login whose address moved to another user mid-request = %d, want 401", rec.Code)
	}
}

func TestLinkBaseURLNeedsAHost(t *testing.T) {
	for _, base := range []string{"https://", "http:///path"} {
		if invalidLinkBaseURL(base) == "" {
			t.Errorf("BaseURL %q with no host was accepted", base)
		}
	}
	if why := invalidLinkBaseURL("https://app.example"); why != "" {
		t.Errorf("a plain origin was refused: %s", why)
	}
}

func TestRegisterStoreFailureIs500(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", DevMode: true, UserStore: failingCreateStore{newMemoryUserStore()}})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	rec := (&cookieJar{}).do(mountRoutes(mgr), http.MethodPost, "/auth/register",
		map[string]string{"email": "a@example.com", "password": "supersecret1"}, "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("register on a failing store = %d, want 500", rec.Code)
	}
}

func TestDuplicateNoticeSenderPanicRecovered(t *testing.T) {
	sender := &panickingSender{done: make(chan struct{})}
	store := newMemoryUserStore()
	mgr := New(AuthConfig{JWTSecret: "s", DevMode: true, UserStore: store, RegisterEmailSender: sender})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.OnStop(context.Background()) })
	seedUser(t, store, "holder@example.com", "supersecret1")
	(&cookieJar{}).do(mountRoutes(mgr), http.MethodPost, "/auth/register",
		map[string]string{"email": "holder@example.com", "password": "supersecret1"}, "")
	select {
	case <-sender.done:
	case <-time.After(2 * time.Second):
		t.Fatal("notice never sent")
	}
	time.Sleep(50 * time.Millisecond)
}

func TestCanonicalizerErrorTextNotEchoed(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", DevMode: true, UserStore: newMemoryUserStore(),
		CanonicalizeEmail: func(string) (string, error) { return "", errors.New("internal-detail-xyz") }})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	rec := (&cookieJar{}).do(mountRoutes(mgr), http.MethodPost, "/auth/register",
		map[string]string{"email": "a@example.com", "password": "supersecret1"}, "")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("canonicalizer error = %d, want 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "internal-detail-xyz") {
		t.Errorf("an unphrased error's text reached the caller: %s", rec.Body.String())
	}
}
