package auth_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/mattn/go-sqlite3"
)

// e2eApp wraps a running GoFastr app for E2E testing.
type e2eApp struct {
	app     *framework.App
	db      *sql.DB
	baseURL string
	client  *http.Client
	cleanup func()
}

func newE2EApp(t *testing.T) *e2eApp {
	t.Helper()

	tmpFile, err := os.CreateTemp("", "auth-e2e-*.db")
	if err != nil {
		t.Fatalf("temp file: %v", err)
	}
	tmpFile.Close()

	db, err := sql.Open("sqlite3", tmpFile.Name())
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	// Create auth tables
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		roles TEXT DEFAULT '["user"]',
		password_set BOOLEAN NOT NULL DEFAULT FALSE,
		created_at DATETIME,
		updated_at DATETIME
	)`)
	if err != nil {
		t.Fatalf("create users table: %v", err)
	}

	app := framework.NewApp(
		framework.WithDB(db),
		framework.WithConfig(framework.AppConfig{Name: "auth-e2e"}),
	)

	// Register user entity so AutoMigrate creates the table
	app.Entity("users", framework.EntityConfig{
		Fields: auth.UserEntityFields(),
	})

	// Create auth manager with entity-backed store
	mgr := auth.New(auth.AuthConfig{
		BasePath:      "/auth",
		SessionCookie: "session_id",
		SessionTTL:    24 * time.Hour,
		SessionSecure: false,
		UserStore:     auth.NewEntityUserStore(db, "users"),
	})
	mgr.Use(auth.NewCorePlugin())

	// Register as battery
	app.RegisterBattery(mgr)

	// Start on port 18082
	addr := "127.0.0.1:18082"
	baseURL := "http://" + addr

	go func() {
		if err := app.Start(addr); err != nil {
			t.Logf("server stopped: %v", err)
		}
	}()

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 5 * time.Second,
	}

	// Wait for server ready
	ready := false
	for i := 0; i < 50; i++ {
		resp, err := client.Get(baseURL + "/openapi.json")
		if err == nil {
			resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("server did not start within 2.5s")
	}

	return &e2eApp{
		app:     app,
		db:      db,
		baseURL: baseURL,
		client:  client,
		cleanup: func() {
			app.Stop(context.Background())
			db.Close()
			os.Remove(tmpFile.Name())
		},
	}
}

func (e *e2eApp) close() {
	e.cleanup()
}

func (e *e2eApp) doRequest(method, path string, body any, cookies []*http.Cookie) (*http.Response, map[string]any, []*http.Cookie) {
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, e.baseURL+path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, nil, nil
	}
	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	resp.Body.Close()
	return resp, result, resp.Cookies()
}

// ─── E2E Tests ─────────────────────────────────────────────────────────

func TestE2E_RegisterLoginLogout(t *testing.T) {
	app := newE2EApp(t)
	defer app.close()

	// 1. Register
	resp, body, _ := app.doRequest("POST", "/auth/register", map[string]string{
		"email":    "alice@test.com",
		"password": "securepass123",
	}, nil)
	if resp.StatusCode != http.StatusCreated {
		b, _ := json.Marshal(body)
		t.Fatalf("register: expected 201, got %d: %s", resp.StatusCode, b)
	}
	userID, _ := body["user"].(map[string]any)["id"].(string)
	if userID == "" {
		t.Fatal("expected userId in register response")
	}
	t.Logf("✓ Register: user %s created", userID)

	// 2. Login
	resp, body, cookies := app.doRequest("POST", "/auth/login", map[string]string{
		"email":    "alice@test.com",
		"password": "securepass123",
	}, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: expected 200, got %d", resp.StatusCode)
	}
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session_id" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		respDump, _ := httputil.DumpResponse(resp, true)
		t.Fatalf("login: no session_id cookie.\n%s", respDump)
	}
	t.Logf("✓ Login: session cookie received (value=%s...)", sessionCookie.Value[:8])

	// 3. Get /me
	resp, body, _ = app.doRequest("GET", "/auth/me", nil, []*http.Cookie{sessionCookie})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me: expected 200, got %d: %v", resp.StatusCode, body)
	}
	userObj, _ := body["user"].(map[string]any)
	email, _ := userObj["email"].(string)
	if email != "alice@test.com" {
		t.Fatalf("me: expected alice@test.com, got %q (body=%v)", email, body)
	}
	t.Logf("✓ Me: got user %q", email)

	// 4. Logout
	resp, _, _ = app.doRequest("POST", "/auth/logout", nil, []*http.Cookie{sessionCookie})
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: expected 204 or 200, got %d", resp.StatusCode)
	}
	t.Log("✓ Logout: success")

	// 5. Bad login
	resp, _, _ = app.doRequest("POST", "/auth/login", map[string]string{
		"email":    "alice@test.com",
		"password": "wrongpassword",
	}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login: expected 401, got %d", resp.StatusCode)
	}
	t.Log("✓ Bad password rejected with 401")

	// 6. Duplicate registration (should still fail)
	resp, _, _ = app.doRequest("POST", "/auth/register", map[string]string{
		"email":    "alice@test.com",
		"password": "anotherpass",
	}, nil)
	if resp.StatusCode == http.StatusCreated {
		t.Fatal("duplicate registration should fail")
	}
	t.Log("✓ Duplicate email rejected")
}

func TestE2E_SessionPersistence(t *testing.T) {
	app := newE2EApp(t)
	defer app.close()

	// Register + login
	app.doRequest("POST", "/auth/register", map[string]string{
		"email":    "bob@test.com",
		"password": "bobpass",
	}, nil)
	_, _, cookies := app.doRequest("POST", "/auth/login", map[string]string{
		"email":    "bob@test.com",
		"password": "bobpass",
	}, nil)
	var cookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session_id" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}

	// Same cookie works for multiple requests
	for i := 0; i < 3; i++ {
		resp, body, _ := app.doRequest("GET", "/auth/me", nil, []*http.Cookie{cookie})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("me request %d: expected 200, got %d: %v", i+1, resp.StatusCode, body)
		}
		email, _ := body["user"].(map[string]any)["email"].(string)
		if email != "bob@test.com" {
			t.Fatalf("me request %d: expected bob@test.com, got %q", i+1, email)
		}
	}
	t.Log("✓ Session persists across 3 requests")

	// Verify user in DB
	var count int
	err := app.db.QueryRow("SELECT COUNT(*) FROM users WHERE email = ?", "bob@test.com").Scan(&count)
	if err != nil {
		t.Fatalf("db query: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 user in DB, got %d", count)
	}
	t.Log("✓ User persisted in database")
}

func TestE2E_PasswordHashing(t *testing.T) {
	app := newE2EApp(t)
	defer app.close()

	app.doRequest("POST", "/auth/register", map[string]string{
		"email":    "charlie@test.com",
		"password": "mypassword",
	}, nil)

	var hash string
	err := app.db.QueryRow("SELECT password_hash FROM users WHERE email = ?", "charlie@test.com").Scan(&hash)
	if err != nil {
		t.Fatalf("db query: %v", err)
	}
	if hash == "mypassword" || hash == "" {
		t.Fatalf("password should be hashed, got: %q", hash)
	}
	if len(hash) < 20 {
		t.Fatalf("hash seems too short: %q", hash)
	}
	t.Logf("✓ Password is hashed in DB (length=%d)", len(hash))
}
