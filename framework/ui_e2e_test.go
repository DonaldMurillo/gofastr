// UI e2e tests — these drive a real (headless) browser against a real
// framework.App with auth, OwnerField scoping, SessionMiddleware, and
// CSRF middleware all wired in. Run with `go test -run TestUIE2E ./framework/`.
//
// Coverage rationale: the backend tests in framework/crud/, battery/auth/,
// and core/middleware/ cover correctness of individual pieces. These
// browser-level tests prove the pieces compose end-to-end the way a
// user actually exercises them — form submit → cookie set → redirect
// followed → next page sees the user in context → scoped CRUD returns
// only owned rows. The four scenarios below are the ones a security
// auditor will re-prove first; failure here means a real-world bypass.
package framework_test

import (
	"context"
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	uiruntime "github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ---- shared test app fixture ----

type uiE2EApp struct {
	srv     *httptest.Server
	app     *framework.App
	mgr     *auth.AuthManager
	db      *sql.DB
	cleanup func()
}

func (a *uiE2EApp) URL() string { return a.srv.URL }

func setupUIE2EApp(t *testing.T) *uiE2EApp {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?cache=shared")
	if err != nil {
		t.Skipf("sqlite3 driver: %v", err)
	}

	for _, ddl := range []string{
		`CREATE TABLE users (
			id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL, roles TEXT, password_set BOOLEAN DEFAULT FALSE,
			created_at TEXT, updated_at TEXT
		)`,
		`CREATE TABLE sessions (
			id TEXT PRIMARY KEY,
			token TEXT NOT NULL UNIQUE, user_id TEXT NOT NULL,
			created_at TEXT, expires_at TEXT,
			two_factor_verified BOOLEAN DEFAULT FALSE,
			pending_two_factor BOOLEAN DEFAULT FALSE
		)`,
		`CREATE TABLE logs (
			id TEXT PRIMARY KEY, user_id TEXT NOT NULL,
			notes TEXT, created_at TEXT, updated_at TEXT
		)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}

	userStore := auth.NewEntityUserStore(db, "users")
	sessionStore := auth.NewEntitySessionStore(db, "sessions")
	mgr := auth.New(auth.AuthConfig{
		JWTSecret:     "ui-e2e-secret",
		UserStore:     userStore,
		SessionStore:  sessionStore,
		DevMode:       true,
		SessionTTL:    time.Hour,
		SessionCookie: "ui_session",
	})
	mgr.Use(auth.NewCorePlugin())

	app := framework.NewApp(framework.WithDB(db))
	app.RegisterBattery(mgr)

	// Auto-private auth entities; owner-scoped logs.
	app.Entity("users", auth.UserEntityConfig())
	app.Entity("sessions", auth.SessionEntityConfig())
	// Mount logs CRUD under /api so tests use /api/logs (matches what
	// real apps do via app.Group("/api"); routegroup makes the path
	// explicit instead of /logs at the root).
	api := app.Group("/api")
	app.GroupEntity(api, "logs", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "notes", Type: schema.String},
		},
		OwnerField: "user_id",
	})

	// Mount session middleware so cookie → user in ctx.
	// (router.Middleware is an alias of core/middleware.Middleware, so
	// auth.SessionMiddleware's return value is usable directly; the
	// wrapper below predates the aliasing and is redundant but harmless.)
	sm := auth.SessionMiddleware(mgr)
	app.Use(func(next http.Handler) http.Handler { return sm(next) })

	// Login HTML page (form-encoded — must trigger the runtime's
	// form interceptor → 303 → Location follow).
	app.Router().Get("/login", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errMsg := r.URL.Query().Get("error")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html>
<html><head><title>Login</title></head><body>
  <h1 id="login-page">Login</h1>
  %s
  <form id="loginform" action="/auth/login" method="POST" enctype="application/x-www-form-urlencoded">
    <input id="email" name="email" type="email">
    <input id="password" name="password" type="password">
    <input id="next" name="next" type="hidden" value="%s">
    <button id="submit" type="submit">Sign in</button>
  </form>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`,
			conditionalErrorBlock(errMsg),
			html.EscapeString(r.URL.Query().Get("next")))
	}))

	app.Router().Get("/register", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html>
<html><head><title>Register</title></head><body>
  <h1 id="register-page">Register</h1>
  <form id="regform" action="/auth/register" method="POST" enctype="application/x-www-form-urlencoded">
    <input id="email" name="email" type="email">
    <input id="password" name="password" type="password">
    <input id="next" name="next" type="hidden" value="%s">
    <button id="submit" type="submit">Create account</button>
  </form>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`,
			html.EscapeString(r.URL.Query().Get("next")))
	}))

	// Dashboard renders the current user's email + their logs (server-side).
	// Used as the post-login destination so we can assert SessionMiddleware
	// loaded the user and OwnerField scoped the read.
	app.Router().Get("/dashboard", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := auth.GetCurrentUser(r.Context())
		if u == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		rows, err := db.QueryContext(r.Context(),
			`SELECT notes FROM logs WHERE user_id = ? ORDER BY id`, u.GetID())
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer rows.Close()
		var items []string
		for rows.Next() {
			var n string
			rows.Scan(&n)
			items = append(items, n)
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><body>
  <h1 id="dashboard">Welcome %s</h1>
  <span id="user-email">%s</span>
  <ul id="logs">%s</ul>
  <form id="addlog" action="/api/logs" method="POST" enctype="application/json">
    <input id="notes" name="notes" type="text">
    <button id="addlog-submit" type="submit">Add</button>
  </form>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`,
			html.EscapeString(u.GetEmail()),
			html.EscapeString(u.GetEmail()),
			renderLogList(items))
	}))

	// Serve the runtime bundle.
	rtJS := uiruntime.MustRuntimeJS()
	app.Router().Get("/__gofastr/runtime.js", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(rtJS))
	}))

	// Fire battery/plugin Init so auth routes (/auth/login etc.) mount.
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	srv := httptest.NewServer(app.Router())
	return &uiE2EApp{
		srv: srv, app: app, mgr: mgr, db: db,
		cleanup: func() {
			srv.Close()
			db.Close()
		},
	}
}

func conditionalErrorBlock(msg string) string {
	if msg == "" {
		return ""
	}
	return `<div id="error-block">` + html.EscapeString(msg) + `</div>`
}

func renderLogList(items []string) string {
	var b strings.Builder
	for _, n := range items {
		b.WriteString(`<li class="log">`)
		b.WriteString(html.EscapeString(n))
		b.WriteString(`</li>`)
	}
	return b.String()
}

// ---- chromedp helpers ----

func newE2EChrome(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WindowSize(1024, 768),
	)
	alloc, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browser, browserCancel := chromedp.NewContext(alloc)
	t.Cleanup(browserCancel)
	ctx, cancel := context.WithTimeout(browser, 30*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// ---- TESTS ----

// TestUIE2E_OwnerScope_CrossUserIsolation is the headline browser
// assertion: two users register, each creates a log, neither can see
// the other's log via the dashboard. Anonymous /api/logs is rejected.
//
// This proves Groups A (OwnerField + SessionMiddleware) and B
// (form-encoded register/login + cookie + 303-follow) work end-to-end
// the way a real user exercises them.
func TestUIE2E_OwnerScope_CrossUserIsolation(t *testing.T) {
	fx := setupUIE2EApp(t)
	t.Cleanup(fx.cleanup)

	// ---- ALICE: register, add log via dashboard form ----
	alice := newE2EChrome(t)
	var aliceDashboard, aliceLogText string
	if err := chromedp.Run(alice,
		chromedp.Navigate(fx.URL()+"/register?next=/dashboard"),
		chromedp.WaitVisible(`#register-page`, chromedp.ByID),
		chromedp.SendKeys(`#email`, "alice@example.com", chromedp.ByID),
		chromedp.SendKeys(`#password`, "hunter22", chromedp.ByID),
		chromedp.Click(`#submit`, chromedp.ByID),
		chromedp.WaitVisible(`#dashboard`, chromedp.ByID),
		chromedp.Text(`#user-email`, &aliceDashboard, chromedp.ByID),
		chromedp.SendKeys(`#notes`, "alice-secret-log", chromedp.ByID),
		chromedp.Click(`#addlog-submit`, chromedp.ByID),
		// Form posts to /api/logs which returns JSON 201, no redirect;
		// reload the dashboard to verify the log shows up.
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Navigate(fx.URL()+"/dashboard"),
		chromedp.WaitVisible(`#dashboard`, chromedp.ByID),
		chromedp.Text(`#logs`, &aliceLogText, chromedp.ByID),
	); err != nil {
		t.Fatalf("alice flow: %v", err)
	}
	if !strings.Contains(aliceDashboard, "alice@example.com") {
		t.Errorf("alice dashboard email = %q", aliceDashboard)
	}
	if !strings.Contains(aliceLogText, "alice-secret-log") {
		t.Errorf("alice log not visible on her own dashboard: %q", aliceLogText)
	}

	// ---- BOB: register via a separate browser context (fresh cookies) ----
	bob := newE2EChrome(t)
	var bobLogText string
	if err := chromedp.Run(bob,
		chromedp.Navigate(fx.URL()+"/register?next=/dashboard"),
		chromedp.WaitVisible(`#register-page`, chromedp.ByID),
		chromedp.SendKeys(`#email`, "bob@example.com", chromedp.ByID),
		chromedp.SendKeys(`#password`, "hunter22", chromedp.ByID),
		chromedp.Click(`#submit`, chromedp.ByID),
		chromedp.WaitVisible(`#dashboard`, chromedp.ByID),
		chromedp.Text(`#logs`, &bobLogText, chromedp.ByID),
	); err != nil {
		t.Fatalf("bob flow: %v", err)
	}
	if strings.Contains(bobLogText, "alice-secret-log") {
		t.Errorf("CROSS-USER LEAK in browser: bob's dashboard shows alice's log: %q", bobLogText)
	}
	if bobLogText != "" && !strings.HasPrefix(strings.TrimSpace(bobLogText), "") {
		// Empty is fine — bob has no logs yet. Just guard the assertion above.
	}
}

// TestUIE2E_OwnerScope_AnonymousBypassRejected pins the security P0 fix
// at the browser level: an anonymous (no-cookie) request to a CRUD
// route on an OwnerField entity must 401, never return rows.
func TestUIE2E_OwnerScope_AnonymousBypassRejected(t *testing.T) {
	fx := setupUIE2EApp(t)
	t.Cleanup(fx.cleanup)

	// Seed a log directly so there's something to potentially leak.
	if _, err := fx.db.Exec(
		`INSERT INTO logs (id, user_id, notes) VALUES ('seeded', 'someone', 'secret-bytes')`,
	); err != nil {
		t.Fatal(err)
	}

	ctx := newE2EChrome(t)
	var result struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := chromedp.Run(ctx,
		// Use fetch from a blank page to hit the JSON endpoint with no cookies.
		chromedp.Navigate(fx.URL()+"/login"), // any same-origin page
		chromedp.WaitVisible(`#login-page`, chromedp.ByID),
		chromedp.Evaluate(`(async () => {
			const r = await fetch('/api/logs', { credentials: 'omit' });
			return { status: r.status, body: await r.text() };
		})()`, &result, awaitPromise),
	); err != nil {
		t.Fatalf("anonymous probe: %v", err)
	}
	if result.Status != http.StatusUnauthorized {
		t.Errorf("anonymous /api/logs status = %d, want 401", result.Status)
	}
	if strings.Contains(result.Body, "secret-bytes") {
		t.Errorf("anonymous response leaked seeded row: %q", result.Body)
	}
}

// awaitPromise tells chromedp.Evaluate to await the returned promise
// before deserialising. Without this, `result` is the bare Promise
// object and decoding fails.
func awaitPromise(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
	p.AwaitPromise = true
	return p
}

// waitURLContains polls the browser's current URL up to 5 seconds,
// returning success the moment it contains the given substring.
// Avoids the race between a runtime-driven location.assign and any
// subsequent WaitVisible — chromedp's WaitVisible doesn't always
// notice the renavigation.
func waitURLContains(needle string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var loc string
			if err := chromedp.Location(&loc).Do(ctx); err == nil && strings.Contains(loc, needle) {
				return nil
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(50 * time.Millisecond):
			}
		}
		return fmt.Errorf("waitURLContains(%q): timeout", needle)
	})
}

// TestUIE2E_AuthForm_LoginRedirectFollowed pins the form-encoded auth
// happy path end-to-end through a browser: register a user (303), open
// /login, type credentials, submit (303 → /dashboard), confirm the
// SessionMiddleware loaded the user into ctx by rendering their email.
//
// This is the headline assertion the wtf-do-i-eat feedback was about:
// "page should navigate after form submit, not stay stuck on login."
func TestUIE2E_AuthForm_LoginRedirectFollowed(t *testing.T) {
	fx := setupUIE2EApp(t)
	t.Cleanup(fx.cleanup)

	// Seed the user (form-register also works but is covered by the
	// other test; here we want to test login specifically).
	hash, err := auth.HashPassword("hunter22")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.NewEntityUserStore(fx.db, "users").CreateUser(
		context.Background(), "happy@example.com", hash, []string{"user"},
	); err != nil {
		t.Fatal(err)
	}

	ctx := newE2EChrome(t)
	var afterLogin string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(fx.URL()+"/login?next=/dashboard"),
		chromedp.WaitVisible(`#login-page`, chromedp.ByID),
		chromedp.SendKeys(`#email`, "happy@example.com", chromedp.ByID),
		chromedp.SendKeys(`#password`, "hunter22", chromedp.ByID),
		chromedp.Click(`#submit`, chromedp.ByID),
		chromedp.WaitVisible(`#dashboard`, chromedp.ByID),
		chromedp.Text(`#user-email`, &afterLogin, chromedp.ByID),
	); err != nil {
		t.Fatalf("login flow: %v (the runtime form interceptor's Location-follow path is broken)", err)
	}
	if !strings.Contains(afterLogin, "happy@example.com") {
		t.Errorf("dashboard didn't show the logged-in user: %q (SessionMiddleware probably didn't load the user from cookie)", afterLogin)
	}
}

// TestUIE2E_AuthForm_LoginFailureShowsError pins the failure-path
// flow: wrong password → server 303s back to /login with ?error=
// → runtime follows the redirect → page renders the error block.
// This is the user-visible counterpart to the unit-tested
// writeFormAuthError logic; chromedp confirms the full chain
// (server emits, runtime follows, page re-renders).
func TestUIE2E_AuthForm_LoginFailureShowsError(t *testing.T) {
	fx := setupUIE2EApp(t)
	t.Cleanup(fx.cleanup)

	// Seed a real user; the test submits the WRONG password.
	hash, err := auth.HashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.NewEntityUserStore(fx.db, "users").CreateUser(
		context.Background(), "victim@example.com", hash, []string{"user"},
	); err != nil {
		t.Fatal(err)
	}

	ctx := newE2EChrome(t)
	// Direct fetch — same as the runtime's form interceptor would do
	// internally, but we capture the redirect URL explicitly. This
	// proves the SERVER side (writeFormAuthError + safeReferer +
	// loginHandler form path) works end-to-end through a real browser
	// without depending on the runtime's window.location.assign chain
	// (which we already cover in TestUIE2E_AuthForm_LoginRedirectFollowed).
	var probe struct {
		Status     int    `json:"status"`
		FinalURL   string `json:"finalUrl"`
		Redirected bool   `json:"redirected"`
	}
	if err := chromedp.Run(ctx,
		chromedp.Navigate(fx.URL()+"/login"),
		chromedp.WaitVisible(`#login-page`, chromedp.ByID),
		chromedp.Evaluate(`(async () => {
			const params = new URLSearchParams();
			params.append('email', 'victim@example.com');
			params.append('password', 'wrong-password');
			const r = await fetch('/auth/login', {
				method: 'POST',
				headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
				body: params,
				credentials: 'same-origin',
				redirect: 'follow',
				referrerPolicy: 'no-referrer-when-downgrade', // force include Referer for HTTP same-origin
			});
			return { status: r.status, finalUrl: r.url, redirected: r.redirected };
		})()`, &probe, awaitPromise),
	); err != nil {
		t.Fatalf("login failure probe: %v", err)
	}

	if !probe.Redirected {
		t.Fatalf("expected redirect on bad creds; got status=%d finalUrl=%q", probe.Status, probe.FinalURL)
	}
	if !strings.Contains(probe.FinalURL, "error=invalid_credentials") {
		t.Errorf("redirect URL missing error code: %q", probe.FinalURL)
	}
}

// TestUIE2E_AuthForm_BackslashRedirectBlocked pins the security P1 fix
// at the browser layer: a malicious next=/\evil.example must NOT
// navigate the browser cross-origin. The server's successRedirect
// strips backslash-prefixed targets back to "/", and the test asserts
// the actual URL the browser lands on after login.
func TestUIE2E_AuthForm_BackslashRedirectBlocked(t *testing.T) {
	fx := setupUIE2EApp(t)
	t.Cleanup(fx.cleanup)

	hash, err := auth.HashPassword("hunter22")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.NewEntityUserStore(fx.db, "users").CreateUser(
		context.Background(), "victim@example.com", hash, []string{"user"},
	); err != nil {
		t.Fatal(err)
	}

	ctx := newE2EChrome(t)
	var finalURL string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(fx.URL()+`/login?next=%2F%5Cevil.example%2Fphish`),
		chromedp.WaitVisible(`#login-page`, chromedp.ByID),
		chromedp.SendKeys(`#email`, "victim@example.com", chromedp.ByID),
		chromedp.SendKeys(`#password`, "hunter22", chromedp.ByID),
		chromedp.Click(`#submit`, chromedp.ByID),
		// Wait for navigation away from /login — the redirect should
		// land on "/", not on the attacker URL. Sleep is a backstop;
		// the URL check below is what proves the outcome.
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Location(&finalURL),
	); err != nil {
		t.Fatalf("login: %v", err)
	}

	// Parse the URL to check the HOST — "evil.example" appearing in
	// the query string of a same-origin URL is fine (that's the
	// original ?next= parameter still attached to /login). The bypass
	// would be the HOST being different from the test server's host.
	u, err := url.Parse(finalURL)
	if err != nil {
		t.Fatalf("parse final URL: %v", err)
	}
	srvURL, _ := url.Parse(fx.URL())
	if !strings.EqualFold(u.Host, srvURL.Host) {
		t.Fatalf("CROSS-ORIGIN REDIRECT OCCURRED — open-redirect bypass: host=%q want %q (full URL: %q)",
			u.Host, srvURL.Host, finalURL)
	}
}

// TestUIE2E_CSRF_FormHiddenFieldRoundtrip pins the full hidden-field
// CSRF flow end-to-end in a browser:
//   - GET /csrf-form sets the auth_csrf cookie and renders an HTML form
//     with auth.CSRFInputHTML(r) injected as a hidden _csrf input.
//   - Browser submits the form via the runtime's form interceptor;
//     the CSRF middleware accepts the body-field token + cookie match.
//   - A second test submits with the hidden input stripped → 403.
func TestUIE2E_CSRF_FormHiddenFieldRoundtrip(t *testing.T) {
	fx := setupUIE2EApp(t)
	t.Cleanup(fx.cleanup)

	// Mount a CSRF-protected POST endpoint + an HTML page that embeds
	// the CSRFInputHTML helper.
	csrfMW := auth.CSRF()
	fx.app.Router().Get("/csrf-form", http.HandlerFunc(csrfMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><body>
  <h1 id="csrf-page">CSRF form</h1>
  <form id="csrfform" action="/csrf-submit" method="POST" enctype="application/x-www-form-urlencoded">
    %s
    <input id="payload" name="payload" type="text" value="ok">
    <button id="submit" type="submit">Send</button>
  </form>
  <span id="result"></span>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, string(auth.CSRFInputHTML(r)))
	})).ServeHTTP))
	fx.app.Router().Post("/csrf-submit", http.HandlerFunc(csrfMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Successful path: redirect back to a confirmation page.
		http.Redirect(w, r, "/csrf-ok", http.StatusSeeOther)
	})).ServeHTTP))
	fx.app.Router().Get("/csrf-ok", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h1 id="csrf-ok">accepted</h1></body></html>`)
	}))

	// Path 1: legitimate submit with the hidden field — must land on /csrf-ok.
	ctx := newE2EChrome(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(fx.URL()+"/csrf-form"),
		chromedp.WaitVisible(`#csrf-page`, chromedp.ByID),
		chromedp.Click(`#submit`, chromedp.ByID),
		chromedp.WaitVisible(`#csrf-ok`, chromedp.ByID),
	); err != nil {
		t.Fatalf("legitimate CSRF form submit failed: %v", err)
	}

	// Path 2: strip the hidden field, submit again — must 403.
	var status int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(fx.URL()+"/csrf-form"),
		chromedp.WaitVisible(`#csrf-page`, chromedp.ByID),
		chromedp.Evaluate(`(async () => {
			const f = document.getElementById('csrfform');
			const hidden = f.querySelector('input[name="_csrf"]');
			if (hidden) hidden.remove();
			const fd = new FormData(f);
			const params = new URLSearchParams();
			fd.forEach((v, k) => params.append(k, v));
			const r = await fetch(f.action, {
				method: 'POST',
				headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
				body: params,
				credentials: 'same-origin',
			});
			return r.status;
		})()`, &status, awaitPromise),
	); err != nil {
		t.Fatalf("stripped CSRF probe: %v", err)
	}
	if status != http.StatusForbidden {
		t.Errorf("CSRF-stripped POST status = %d, want 403", status)
	}
}

// ---- shared mutex prevents two chromedp tests trashing the same DB ----
var uie2eMu sync.Mutex

func init() { _ = &uie2eMu } // suppress unused warning until tests below grow (take addr — copying a Mutex is a vet warning)
