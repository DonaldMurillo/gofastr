package auth

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func setupAuthTestServer(t *testing.T) (*httptest.Server, *AuthManager) {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "auth-form-*.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })
	db, err := sql.Open("sqlite3", tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE,
		password_hash TEXT,
		roles TEXT,
		password_set BOOLEAN DEFAULT FALSE,
		created_at DATETIME,
		updated_at DATETIME
	)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE sessions (
		token TEXT PRIMARY KEY,
		user_id TEXT,
		created_at DATETIME,
		expires_at DATETIME,
		two_factor_verified BOOLEAN DEFAULT FALSE,
		pending_two_factor BOOLEAN DEFAULT FALSE
	)`); err != nil {
		t.Fatal(err)
	}

	userStore := NewEntityUserStore(db, "users")
	sessionStore := NewEntitySessionStore(db, "sessions")
	mgr := New(AuthConfig{
		JWTSecret:     "test-secret",
		UserStore:     userStore,
		SessionStore:  sessionStore,
		DevMode:       true,
		SessionTTL:    time.Hour,
		SessionCookie: "test_session",
	})
	mgr.Use(NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("mgr.Init: %v", err)
	}

	r := router.New()
	mgr.RegisterRoutes(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, mgr
}

func TestRegisterAcceptsFormEncoded(t *testing.T) {
	srv, _ := setupAuthTestServer(t)

	form := url.Values{}
	form.Set("email", "form-user@example.com")
	form.Set("password", "hunter22")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequestWithContext(context.Background(),
		http.MethodPost, srv.URL+"/auth/register", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("form register status = %d, want 303. body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}
	// Session cookie should be set.
	gotCookie := false
	for _, c := range resp.Cookies() {
		if c.Name == "test_session" && c.Value != "" {
			gotCookie = true
		}
	}
	if !gotCookie {
		t.Errorf("expected test_session cookie on form-register response")
	}
}

func TestLoginAcceptsFormEncoded(t *testing.T) {
	srv, mgr := setupAuthTestServer(t)

	// Pre-create a user via JSON path so we can exercise the form login.
	regForm := strings.NewReader(`{"email":"alice@example.com","password":"hunter22"}`)
	jsonReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/auth/register", regForm)
	jsonReq.Header.Set("Content-Type", "application/json")
	if _, err := http.DefaultClient.Do(jsonReq); err != nil {
		t.Fatal(err)
	}

	form := url.Values{}
	form.Set("email", "alice@example.com")
	form.Set("password", "hunter22")
	form.Set("next", "/dashboard")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("form login status = %d (want 303). body=%s", resp.StatusCode, mustReadBody(t, resp))
	}
	if loc := resp.Header.Get("Location"); loc != "/dashboard" {
		t.Errorf("Location = %q, want /dashboard (?next override)", loc)
	}

	// Confirm session cookie is real (recoverable to a User).
	for _, c := range resp.Cookies() {
		if c.Name == "test_session" && c.Value != "" {
			if _, err := mgr.SessionStore().Get(context.Background(), c.Value); err != nil {
				t.Errorf("session not recoverable: %v", err)
			}
		}
	}
}

func TestLoginFormRejectsExternalRedirect(t *testing.T) {
	srv, _ := setupAuthTestServer(t)

	// Create user.
	reg := strings.NewReader(`{"email":"u@example.com","password":"hunter22"}`)
	r1, _ := http.NewRequest(http.MethodPost, srv.URL+"/auth/register", reg)
	r1.Header.Set("Content-Type", "application/json")
	http.DefaultClient.Do(r1)

	form := url.Values{}
	form.Set("email", "u@example.com")
	form.Set("password", "hunter22")
	form.Set("next", "//evil.example/")

	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if loc := resp.Header.Get("Location"); loc != "/" {
		t.Errorf("external next allowed: Location=%q (want /)", loc)
	}
}

func TestLogoutFormRedirects(t *testing.T) {
	srv, _ := setupAuthTestServer(t)
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/auth/logout", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Errorf("form logout status = %d, want 303", resp.StatusCode)
	}
}

func TestLoginJSONStillReturnsJSON(t *testing.T) {
	srv, _ := setupAuthTestServer(t)
	reg := strings.NewReader(`{"email":"j@example.com","password":"hunter22"}`)
	r1, _ := http.NewRequest(http.MethodPost, srv.URL+"/auth/register", reg)
	r1.Header.Set("Content-Type", "application/json")
	http.DefaultClient.Do(r1)

	loginBody := strings.NewReader(`{"email":"j@example.com","password":"hunter22"}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/auth/login", loginBody)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("JSON login status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("JSON login Content-Type = %q", ct)
	}
}

func mustReadBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	return string(buf[:n])
}
