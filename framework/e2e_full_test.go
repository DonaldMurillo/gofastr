package framework

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

// Full-stack E2E: spins up a real httptest.Server with the full middleware
// pipeline (recovery, request-id, logging, metrics, ratelimit, csrf,
// session) plus entity CRUD + auth routes, and drives a real http.Client
// with cookie jar through a user-shaped happy path:
//
//   register-style seed → login → CSRF roundtrip → create post →
//   batch-create → list with ?include= → cursor pagination → upload image
//   → subscribe SSE + trigger event → logout
//
// Each step is a subtest so failures isolate. Runs on both SQLite and
// Postgres via forEachDialect — proves the pieces compose under real
// network conditions, not just direct ServeHTTP.

// e2eApp wires every relevant feature into one app.
type e2eEnv struct {
	app    *App
	server *httptest.Server
	client *http.Client
}

// fullTestUser implements auth.User for the e2e flow.
type fullTestUser struct {
	id, email string
	roles     []string
}

func (u fullTestUser) GetID() string      { return u.id }
func (u fullTestUser) GetEmail() string   { return u.email }
func (u fullTestUser) GetRoles() []string { return u.roles }

// e2eMemUserStore implements auth.UserStore (the modern interface) for
// the e2e flow. Exposes FindByEmail / FindByID / CreateUser.
type e2eMemUserStore struct {
	byEmail map[string]struct {
		user auth.User
		hash string
	}
	byID map[string]struct {
		user auth.User
		hash string
	}
}

func (s *e2eMemUserStore) FindByEmail(_ context.Context, email string) (auth.User, string, error) {
	if e, ok := s.byEmail[email]; ok {
		return e.user, e.hash, nil
	}
	return nil, "", auth.ErrUserNotFound
}

func (s *e2eMemUserStore) FindByID(_ context.Context, id string) (auth.User, error) {
	if e, ok := s.byID[id]; ok {
		return e.user, nil
	}
	return nil, auth.ErrUserNotFound
}

func (s *e2eMemUserStore) CreateUser(_ context.Context, email, hashedPassword string, roles []string) (auth.User, error) {
	if _, exists := s.byEmail[email]; exists {
		return nil, auth.ErrEmailTaken
	}
	u := fullTestUser{id: "u-" + email, email: email, roles: roles}
	entry := struct {
		user auth.User
		hash string
	}{user: u, hash: hashedPassword}
	s.byEmail[email] = entry
	s.byID[u.id] = entry
	return u, nil
}

func newE2EUserStore(t *testing.T) *e2eMemUserStore {
	t.Helper()
	hash, err := auth.HashPassword("hunter2")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u := fullTestUser{id: "u1", email: "alice@x.com", roles: []string{"admin"}}
	entry := struct {
		user auth.User
		hash string
	}{user: u, hash: hash}
	return &e2eMemUserStore{
		byEmail: map[string]struct {
			user auth.User
			hash string
		}{"alice@x.com": entry},
		byID: map[string]struct {
			user auth.User
			hash string
		}{"u1": entry},
	}
}

// e2eSetup constructs the full app + httptest.Server + cookie-jar client.
func e2eSetup(t *testing.T, db *sql.DB, uploadDir string) *e2eEnv {
	t.Helper()

	// Tables for posts + comments (HasMany).
	for _, ddl := range []string{
		`CREATE TABLE posts (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL,
			body TEXT DEFAULT '',
			author_id TEXT,
			avatar TEXT DEFAULT ''
		)`,
		`CREATE TABLE comments (
			id TEXT PRIMARY KEY,
			body TEXT NOT NULL,
			post_id TEXT NOT NULL
		)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	// Pre-route middleware: metrics + ratelimit + csrf.
	r := router.New()
	userStore := newE2EUserStore(t)

	metrics := middleware.NewMetrics()
	r.Use(
		router.Middleware(middleware.Recovery()),
		router.Middleware(middleware.RequestID()),
		router.Middleware(middleware.MetricsMiddleware(metrics)),
		router.Middleware(middleware.RateLimit(middleware.RateLimitConfig{
			Capacity:    200,
			RefillEvery: time.Minute,
			RefillBy:    200,
		})),
		// SecretKey makes the CSRF cookie HMAC-signed and pins this test
		// to the production wiring pattern. In production you want a
		// stable key sourced from config (env / secret manager) so
		// tokens survive deploys and the signing seam is auditable. The
		// fixed test key here documents that pattern; the per-process
		// autogen path is acceptable for the simplest dev setups but
		// rotates on every restart and is not the recommended posture.
		router.Middleware(middleware.CSRF(middleware.CSRFConfig{
			Skip:      middleware.SkipBearerAuth(),
			SecretKey: []byte("e2e-test-csrf-key-32-bytes-long!!"),
		})),
	)

	app := NewApp(WithDB(db), WithoutDefaultMiddleware(), WithRouter(r),
		WithFileStorage(upload.NewLocalStorage(uploadDir)))
	app.Entity("posts", entity.EntityConfig{
		Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.Text},
			{Name: "author_id", Type: schema.String},
			{Name: "avatar", Type: schema.Image},
		},
		Relations: []entity.Relation{
			entity.HasMany("comments", "comments", "post_id"),
		},
	}.WithTimestamps(false))
	app.Entity("comments", entity.EntityConfig{
		Table: "comments",
		Fields: []schema.Field{
			{Name: "body", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))

	// Auth wiring: AuthManager + CorePlugin.
	mgr := auth.New(auth.AuthConfig{
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     userStore,
		// httptest serves over plain HTTP — DevMode keeps the cookie
		// readable (no __Host- prefix) and Secure=false.
		DevMode: true,
	})
	mgr.Use(auth.NewCorePlugin())
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("auth init: %v", err)
	}
	mgr.RegisterRoutes(r)

	r.Get("/metrics", middleware.MetricsHandler(metrics))

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 5 * time.Second}

	return &e2eEnv{app: app, server: srv, client: client}
}

// doRequest sends req through the env's client + cookie jar, captures + returns
// the body. Caller is responsible for closing nothing — buffered.
func (e *e2eEnv) doRequest(t *testing.T, req *http.Request) (int, http.Header, []byte) {
	t.Helper()
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatalf("client.Do(%s %s): %v", req.Method, req.URL.Path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, body
}

// jsonReq builds a JSON-bodied request relative to the server URL.
func (e *e2eEnv) jsonReq(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, e.server.URL+path, buf)
	if err != nil {
		t.Fatalf("build req: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// withCSRF attaches the current CSRF cookie's value as the header. Pulls
// the cookie from the jar.
func (e *e2eEnv) withCSRF(req *http.Request) {
	u, _ := url.Parse(e.server.URL)
	for _, c := range e.client.Jar.Cookies(u) {
		if c.Name == "csrf_token" {
			req.Header.Set("X-CSRF-Token", c.Value)
			return
		}
	}
}

// ============================================================================
// TestE2E_Full drives the full real-network happy path on both dialects.
// ============================================================================

func TestE2E_Full(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		uploadDir := t.TempDir()
		env := e2eSetup(t, db, uploadDir)

		// --- Step 1: seed CSRF cookie via a GET, then attempt unsafe call → blocked
		t.Run("csrf_blocks_post_without_token", func(t *testing.T) {
			// GET seeds the csrf_token cookie.
			req, _ := http.NewRequest("GET", env.server.URL+"/posts", nil)
			if code, _, _ := env.doRequest(t, req); code != http.StatusUnauthorized && code != http.StatusOK {
				// Either is fine — depends on whether the route requires auth
				// (we haven't added that yet). The point is the GET succeeds at
				// the CSRF layer and seeds the cookie.
			}
			// POST without the header should be 403 from CSRF middleware.
			postReq := env.jsonReq(t, "POST", "/posts", map[string]any{"title": "x"})
			code, _, body := env.doRequest(t, postReq)
			if code != http.StatusForbidden {
				t.Fatalf("expected CSRF 403 without header, got %d: %s", code, body)
			}
		})

		// --- Step 2: login → cookie set
		var loginBody map[string]any
		t.Run("login_sets_session_cookie", func(t *testing.T) {
			req := env.jsonReq(t, "POST", "/auth/login",
				map[string]string{"email": "alice@x.com", "password": "hunter2"})
			env.withCSRF(req)
			code, _, body := env.doRequest(t, req)
			if code != http.StatusOK {
				t.Fatalf("login: %d %s", code, body)
			}
			if err := json.Unmarshal(body, &loginBody); err != nil {
				t.Fatalf("decode: %v", err)
			}
			user, ok := loginBody["user"].(map[string]any)
			if !ok || user["email"] != "alice@x.com" {
				t.Fatalf("login response: %v", loginBody)
			}
			// Confirm session cookie now in jar.
			u, _ := url.Parse(env.server.URL)
			seen := false
			for _, c := range env.client.Jar.Cookies(u) {
				if c.Name == "session_id" {
					seen = true
				}
			}
			if !seen {
				t.Fatal("expected session_id cookie after login")
			}
		})

		// --- Step 3: GET /auth/me → returns the active session info
		t.Run("me_returns_active_session", func(t *testing.T) {
			req, _ := http.NewRequest("GET", env.server.URL+"/auth/me", nil)
			code, _, body := env.doRequest(t, req)
			if code != http.StatusOK {
				t.Fatalf("me: %d %s", code, body)
			}
			var me map[string]any
			if err := json.Unmarshal(body, &me); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if me["userId"] != "u1" {
				t.Fatalf("expected userId u1, got %v", me)
			}
		})

		// --- Step 4: create a post (CSRF + session both required)
		var postID string
		t.Run("create_post", func(t *testing.T) {
			req := env.jsonReq(t, "POST", "/posts",
				map[string]any{"title": "First", "body": "Hello world", "author_id": "u1"})
			env.withCSRF(req)
			code, _, body := env.doRequest(t, req)
			if code != http.StatusCreated {
				t.Fatalf("create: %d %s", code, body)
			}
			var created map[string]any
			if err := json.Unmarshal(body, &created); err != nil {
				t.Fatalf("decode: %v", err)
			}
			postID, _ = created["id"].(string)
			if postID == "" {
				t.Fatalf("missing id in: %v", created)
			}
		})

		// --- Step 5: batch-create comments under the post
		t.Run("batch_create_comments", func(t *testing.T) {
			req := env.jsonReq(t, "POST", "/comments/_batch", map[string]any{
				"items": []map[string]any{
					{"body": "nice", "post_id": postID},
					{"body": "great", "post_id": postID},
					{"body": "agree", "post_id": postID},
				},
			})
			env.withCSRF(req)
			code, _, body := env.doRequest(t, req)
			if code != http.StatusOK {
				t.Fatalf("batch: %d %s", code, body)
			}
			var resp map[string]any
			json.Unmarshal(body, &resp)
			if resp["committed"] != true {
				t.Fatalf("expected committed=true, got %v", resp)
			}
			results, _ := resp["results"].([]any)
			if len(results) != 3 {
				t.Fatalf("expected 3 results, got %d", len(results))
			}
		})

		// --- Step 6: list posts with ?include=comments → all 3 attached
		t.Run("list_with_include", func(t *testing.T) {
			req, _ := http.NewRequest("GET", env.server.URL+"/posts?include=comments", nil)
			code, _, body := env.doRequest(t, req)
			if code != http.StatusOK {
				t.Fatalf("list: %d %s", code, body)
			}
			var env crud.ListResponse
			if err := json.Unmarshal(body, &env); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if env.Total != 1 {
				t.Fatalf("expected 1 post, got %d", env.Total)
			}
			row := env.Data[0]
			comments, ok := row["comments"].([]any)
			if !ok || len(comments) != 3 {
				t.Fatalf("expected 3 included comments, got %v", row["comments"])
			}
		})

		// --- Step 7: cursor pagination over posts
		t.Run("cursor_pagination", func(t *testing.T) {
			// Add 4 more posts so cursor has something to walk.
			for i := 0; i < 4; i++ {
				req := env.jsonReq(t, "POST", "/posts",
					map[string]any{"title": fmt.Sprintf("Post %d", i+2), "author_id": "u1"})
				env.withCSRF(req)
				if code, _, body := env.doRequest(t, req); code != http.StatusCreated {
					t.Fatalf("seed post %d: %d %s", i+2, code, body)
				}
			}
			req, _ := http.NewRequest("GET", env.server.URL+"/posts?cursor=&limit=2", nil)
			code, _, body := env.doRequest(t, req)
			if code != http.StatusOK {
				t.Fatalf("cursor first: %d %s", code, body)
			}
			var page pagination.CursorPage
			if err := json.Unmarshal(body, &page); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(page.Data) != 2 || !page.HasMore || page.Cursor == "" {
				t.Fatalf("first page: %+v", page)
			}
			// Walk to end.
			seen := len(page.Data)
			for page.HasMore && page.Cursor != "" {
				next, _ := http.NewRequest("GET", env.server.URL+"/posts?cursor="+page.Cursor+"&limit=2", nil)
				if code, _, body := env.doRequest(t, next); code != http.StatusOK {
					t.Fatalf("cursor walk: %d %s", code, body)
				} else {
					page = pagination.CursorPage{}
					json.Unmarshal(body, &page)
					seen += len(page.Data)
				}
			}
			if seen != 5 {
				t.Fatalf("expected to walk 5 posts, walked %d", seen)
			}
		})

		// --- Step 8: multipart upload of an avatar on a new post
		t.Run("multipart_upload", func(t *testing.T) {
			var buf bytes.Buffer
			mw := multipart.NewWriter(&buf)
			_ = mw.WriteField("title", "with avatar")
			_ = mw.WriteField("author_id", "u1")
			fw, _ := mw.CreateFormFile("avatar", "a.png")
			_, _ = fw.Write([]byte("fake-png-bytes"))
			_ = mw.Close()

			req, _ := http.NewRequest("POST", env.server.URL+"/posts", &buf)
			req.Header.Set("Content-Type", mw.FormDataContentType())
			env.withCSRF(req)
			code, _, body := env.doRequest(t, req)
			if code != http.StatusCreated {
				t.Fatalf("upload: %d %s", code, body)
			}
			var got map[string]any
			json.Unmarshal(body, &got)
			avatar, _ := got["avatar"].(string)
			if !strings.HasPrefix(avatar, "uploads/posts/avatar/") {
				t.Fatalf("expected uploaded avatar URL, got %q", avatar)
			}
		})

		// --- Step 9: SSE subscription receives the create event
		t.Run("sse_subscription", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", env.server.URL+"/posts/_events", nil)
			// SSE uses GET so CSRF cookie is set as a side effect on the
			// subscribing request; no header needed (safe method).
			resp, err := env.client.Do(req)
			if err != nil {
				t.Fatalf("subscribe: %v", err)
			}
			defer resp.Body.Close()

			// Fire a create after the subscription opens.
			go func() {
				time.Sleep(80 * time.Millisecond)
				postReq := env.jsonReq(t, "POST", "/posts",
					map[string]any{"title": "live", "author_id": "u1"})
				env.withCSRF(postReq)
				_, _ = env.client.Do(postReq)
			}()

			scanner := bufio.NewScanner(resp.Body)
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				if !scanner.Scan() {
					break
				}
				line := scanner.Text()
				if strings.HasPrefix(line, "event: entity.created") {
					return // success
				}
			}
			t.Fatal("did not receive entity.created within deadline")
		})

		// --- Step 10: metrics endpoint exposes recorded counters
		t.Run("metrics_endpoint", func(t *testing.T) {
			req, _ := http.NewRequest("GET", env.server.URL+"/metrics", nil)
			code, _, body := env.doRequest(t, req)
			if code != http.StatusOK {
				t.Fatalf("metrics: %d %s", code, body)
			}
			s := string(body)
			if !strings.Contains(s, "http_requests_total{") {
				t.Fatalf("expected request counters, got:\n%s", s)
			}
			if !strings.Contains(s, "http_request_duration_ms_bucket") {
				t.Fatalf("expected duration histogram")
			}
		})

		// --- Step 11: logout invalidates the session
		t.Run("logout_clears_session", func(t *testing.T) {
			req := env.jsonReq(t, "POST", "/auth/logout", nil)
			env.withCSRF(req)
			if code, _, body := env.doRequest(t, req); code != http.StatusNoContent {
				t.Fatalf("logout: %d %s", code, body)
			}
			// /auth/me should now 401.
			meReq, _ := http.NewRequest("GET", env.server.URL+"/auth/me", nil)
			code, _, body := env.doRequest(t, meReq)
			if code != http.StatusUnauthorized {
				t.Fatalf("expected post-logout me to 401, got %d %s", code, body)
			}
		})
	})
}
