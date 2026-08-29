package framework_test

// Issue #291 slice: characterise the middleware posture of the agent
// surface (/mcp) for bearer-only clients, on both serving roles.
//
// The browser chain is built for cookies: the documented browser posture
// mounts auth.CSRF app-wide, and an agent authenticates with
// `Authorization: Bearer gfsk_…` while carrying NO cookie, NO Origin, NO
// Sec-Fetch-Site, and NO X-CSRF-Token. If any layer in front of /mcp
// demanded one of those, agents break in ways that look like auth bugs.
//
// Every other wiring test in this package (agent_identity_test.go,
// app_role_test.go) builds its app with WithoutDefaultMiddleware, so the
// REAL default chain — recovery, request-id, DB-context, security headers,
// timeout — had never met a bearer /mcp request either. These tests run
// the full default chain plus the documented auth middlewares
// (auth.md: CSRF, then SessionMiddleware outer, TokenMiddleware inner),
// on a live listener for both RoleServe and RoleAgent, and pin:
//
//   - a cookieless bearer POST /mcp survives the whole chain: initialize
//     answers, and an MCPUser-gated tool resolves the bearer principal
//     (which also proves no layer strips or mangles Authorization);
//   - a request with NEITHER credential is refused — by CSRF's
//     missing-cookie branch when the browser posture is mounted, and by
//     the tool gate when it is not (the generated-app posture, which
//     deliberately omits CSRF for non-browser clients).

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework"

	// Registers the "sqlite3" driver this file opens. Explicit rather
	// than relying on a transitive import from a sibling test file.
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// ──────────────────────────────────────────────────────────────────────
// Environment
// ──────────────────────────────────────────────────────────────────────

// bearerPostureEnv is a live listener serving /mcp under the FULL default
// middleware chain plus the documented auth wiring, with one minted gfsk_
// credential for alice.
type bearerPostureEnv struct {
	addr   string
	bearer string
}

const (
	postureInitialize = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{` +
		`"protocolVersion":"2025-06-18","capabilities":{},` +
		`"clientInfo":{"name":"posture-test","version":"0"}}}`
	postureWhoami = `{"jsonrpc":"2.0","id":2,"method":"tools/call",` +
		`"params":{"name":"posture_whoami","arguments":{}}}`
)

// newBearerPostureEnv builds the app the way a real host does: NO
// WithoutDefaultMiddleware, NO WithRouter — NewApp installs its default
// chain, and the auth middlewares join it via Use in the order
// framework/docs/content/auth.md prescribes. mountCSRF selects between the
// two documented postures: the browser posture (auth.CSRF mounted app-wide,
// the worst case for a cookieless client) and the generated-app posture
// (no CSRF, agents and JSON clients in mind — see docs/content/blueprints).
func newBearerPostureEnv(t *testing.T, role framework.Role, mountCSRF bool) *bearerPostureEnv {
	t.Helper()
	t.Setenv("GOFASTR_ROLE", "")

	// Fatal, never Skip: a skipped posture guard is indistinguishable
	// from a passing one in CI.
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite3: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	alice := identityUser{id: "u-alice", email: "alice@example.com"}
	users := &identityUserStore{
		byID:   map[string]identityUser{alice.id: alice},
		byMail: map[string]identityUser{alice.email: alice},
	}
	tokens := newIdentityTokenStore()

	mgr := auth.New(auth.AuthConfig{
		DevMode:       true, // memory session store, non-Secure cookie on plain HTTP
		SessionTTL:    time.Hour,
		SessionCookie: "session_id",
		UserStore:     users,
	})
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("auth init: %v", err)
	}

	app := framework.NewApp(
		framework.WithConfig(framework.AppConfig{Name: "bearer-posture"}),
		framework.WithDB(db),
		framework.WithRole(role),
		framework.WithMCP(),
	)
	if mountCSRF {
		// The documented browser mounting, verbatim: no custom Skip, so
		// the shipped default (SkipBearerAuth + embed exemption) is what
		// gets pinned.
		app.Use(router.Middleware(auth.CSRF(
			auth.WithCSRFSecret([]byte("bearer-posture-test-csrf-key-32b")),
		)))
	}
	app.Use(router.Middleware(auth.SessionMiddleware(mgr)))
	app.Use(router.Middleware(auth.TokenMiddleware(users, nil, tokens)))

	// One MCPUser-gated tool: it only answers when SOME layer resolved a
	// principal, so a 200 with alice's id proves the Authorization header
	// survived every hop of the chain untouched.
	if err := app.MCP.RegisterTool("posture_whoami",
		"returns the authenticated caller's id",
		map[string]any{"type": "object"},
		mcp.Gated(auth.MCPUser(), func(ctx context.Context, _ map[string]any) (any, error) {
			return map[string]any{"caller": auth.GetCurrentUser(ctx).GetID()}, nil
		})); err != nil {
		t.Fatalf("register posture_whoami: %v", err)
	}

	return &bearerPostureEnv{
		addr:   startPostureServer(t, app),
		bearer: mintUserToken(t, tokens, "tok-posture-alice", alice.id),
	}
}

// startPostureServer runs app.Start on 127.0.0.1:0 and returns the bound
// address, shutting the app down at cleanup. Signal handling is disabled so
// concurrent tests don't fight over the process signal mask.
func startPostureServer(t *testing.T, app *framework.App) string {
	t.Helper()
	app.Config.DisableSignalHandling = true

	ready := make(chan string, 1)
	app.OnReady(func(addr string) { ready <- addr })
	done := make(chan error, 1)
	go func() { done <- app.Start("127.0.0.1:0") }()
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("Start returned before ready: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server never became ready")
	}

	t.Cleanup(func() {
		// Drain the shared client's keep-alive pool first, or a queued
		// dial can hold the server's Shutdown for its 5s grace period.
		http.DefaultClient.CloseIdleConnections()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := app.Shutdown(ctx); err != nil {
			t.Fatalf("Shutdown: %v", err)
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Start did not return after Shutdown")
		}
	})
	return addr
}

// postMCP sends one JSON-RPC POST to /mcp with an agent client's CREDENTIAL
// shape: Authorization set only when bearer != "", and NEVER a Cookie,
// Origin, Sec-Fetch-*, or X-CSRF-Token header. It sends no Accept header, so
// the response takes the plain-JSON path rather than the streamable-http SSE
// one - every gate pinned here sits upstream of that negotiation.
func (e *bearerPostureEnv) postMCP(t *testing.T, bearer, payload string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://"+e.addr+"/mcp", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// rpcErrorMessage decodes a JSON-RPC error message, or "" when the response
// carries no error member.
func rpcErrorMessage(t *testing.T, body string) string {
	t.Helper()
	var resp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode JSON-RPC response: %v\n%s", err, body)
	}
	if resp.Error == nil {
		return ""
	}
	return resp.Error.Message
}

// ──────────────────────────────────────────────────────────────────────
// The contract
// ──────────────────────────────────────────────────────────────────────

// On RoleServe with the full browser posture mounted (default chain +
// CSRF + session + token), a cookieless bearer request to /mcp succeeds
// end to end: the handshake answers, and the gated tool resolves the
// bearer principal — proving CSRF skips the request and no layer strips
// Authorization.
func TestBearerPosture_RoleServe_CookielessBearerSucceeds(t *testing.T) {
	e := newBearerPostureEnv(t, framework.RoleServe, true)

	code, body := e.postMCP(t, e.bearer, postureInitialize)
	if code != http.StatusOK {
		t.Fatalf("bearer initialize: got %d body %s", code, body)
	}
	if strings.Contains(body, `"error"`) || !strings.Contains(body, "serverInfo") {
		t.Fatalf("bearer initialize must complete cleanly: %s", body)
	}

	code, body = e.postMCP(t, e.bearer, postureWhoami)
	if code != http.StatusOK {
		t.Fatalf("bearer tools/call: got %d body %s", code, body)
	}
	if msg := rpcErrorMessage(t, body); msg != "" {
		t.Fatalf("bearer tools/call must pass the auth gate, got: %s", msg)
	}
	if !strings.Contains(body, "u-alice") {
		t.Fatalf("gated tool must see the bearer principal, got: %s", body)
	}

	// The whole point of this file is that the REAL default chain is in the
	// path - every earlier wiring test used WithoutDefaultMiddleware(), so a
	// bearer /mcp request had never met it. Everything asserted above comes
	// from the Use()-added auth middlewares, so observe the default chain
	// directly: RequestID is installed by DefaultMiddleware and by nothing
	// else here. Without this, all four guards would still pass if NewApp
	// stopped committing defaults for apps that call Use.
	req, err := http.NewRequest(http.MethodPost, "http://"+e.addr+"/mcp", strings.NewReader(postureInitialize))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp: %v", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if rid := resp.Header.Get("X-Request-Id"); rid == "" {
		t.Error("no X-Request-Id on the response: the default middleware chain is not in the path, so this file is not testing what it claims")
	}
}

// Same pin on RoleAgent: the agent mux forwards /mcp into the same router,
// so the same full chain applies on the dedicated agent listener.
func TestBearerPosture_RoleAgent_CookielessBearerSucceeds(t *testing.T) {
	e := newBearerPostureEnv(t, framework.RoleAgent, true)

	code, body := e.postMCP(t, e.bearer, postureInitialize)
	if code != http.StatusOK {
		t.Fatalf("bearer initialize: got %d body %s", code, body)
	}
	if strings.Contains(body, `"error"`) || !strings.Contains(body, "serverInfo") {
		t.Fatalf("bearer initialize must complete cleanly: %s", body)
	}

	code, body = e.postMCP(t, e.bearer, postureWhoami)
	if code != http.StatusOK {
		t.Fatalf("bearer tools/call: got %d body %s", code, body)
	}
	if msg := rpcErrorMessage(t, body); msg != "" {
		t.Fatalf("bearer tools/call must pass the auth gate, got: %s", msg)
	}
	if !strings.Contains(body, "u-alice") {
		t.Fatalf("gated tool must see the bearer principal, got: %s", body)
	}
}

// A request with NEITHER credential is refused under the browser posture:
// CSRF has nothing to skip on (no Authorization) and no cookie to double-
// submit, so the unsafe POST is refused by the CSRF layer before the MCP
// transport ever sees it.
//
// The assertion deliberately pins the LAYER, not the branch. A credential-free
// POST can be refused by the missing-cookie branch or the token-mismatch one
// and either satisfies the posture contract, so asserting a branch would
// freeze an incidental detail; only removing CSRF enforcement entirely turns
// this red.
func TestBearerPosture_RoleServe_NoCredentialRefusedByCSRF(t *testing.T) {
	e := newBearerPostureEnv(t, framework.RoleServe, true)

	code, body := e.postMCP(t, "", postureInitialize)
	if code != http.StatusForbidden {
		t.Fatalf("credential-free initialize: got %d body %s, want 403 from CSRF", code, body)
	}
	if !strings.Contains(body, "csrf") {
		t.Fatalf("refusal must come from the CSRF layer, got: %s", body)
	}

	code, body = e.postMCP(t, "", postureWhoami)
	if code != http.StatusForbidden {
		t.Fatalf("credential-free tools/call: got %d body %s, want 403 from CSRF", code, body)
	}
}

// The same refusal on the agent role. Without this the credential-free
// path was pinned only on RoleServe, while the sole unauthenticated
// RoleAgent case ran with CSRF unmounted - so if agent routing stopped
// running CSRF but kept token auth, every guard here stayed green and the
// "both roles" claim was unverifiable.
func TestBearerPosture_RoleAgent_NoCredentialRefusedByCSRF(t *testing.T) {
	e := newBearerPostureEnv(t, framework.RoleAgent, true)

	code, body := e.postMCP(t, "", postureInitialize)
	if code != http.StatusForbidden {
		t.Fatalf("credential-free initialize on the agent role: got %d body %s, want 403 from CSRF", code, body)
	}
	if !strings.Contains(body, "csrf") {
		t.Fatalf("refusal on the agent role must come from the CSRF layer, got: %s", body)
	}

	code, body = e.postMCP(t, "", postureWhoami)
	if code != http.StatusForbidden {
		t.Fatalf("credential-free tools/call on the agent role: got %d body %s, want 403 from CSRF", code, body)
	}
}

// Without CSRF (the generated-app posture for JSON/MCP-first apps), a
// credential-free request passes the middleware and is refused by the
// tool gate instead: the handler never runs, the JSON-RPC error names the
// missing credential, and the bearer counterpart still works.
func TestBearerPosture_RoleAgent_NoCredentialRefusedByToolGate(t *testing.T) {
	e := newBearerPostureEnv(t, framework.RoleAgent, false)

	code, body := e.postMCP(t, "", postureWhoami)
	if code >= 500 {
		t.Fatalf("credential-free tools/call must be a clean refusal, got %d body %s", code, body)
	}
	if msg := rpcErrorMessage(t, body); !strings.Contains(msg, "authenticated caller") {
		t.Fatalf("credential-free tools/call must be refused by the gate, got: %s", body)
	}

	code, body = e.postMCP(t, e.bearer, postureWhoami)
	if code != http.StatusOK || !strings.Contains(body, "u-alice") {
		t.Fatalf("bearer tools/call on the CSRF-less posture: got %d body %s", code, body)
	}
}
