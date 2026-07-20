package uihost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

func TestPartialTitleHeaderIsEncoded(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Gofastr-Navigate", "1") // partial (cross-page nav) response
	ds.ServeHTTP(rec, req)

	h := rec.Header().Get("X-Gofastr-Title")
	if h == "" {
		t.Fatal("partial response should carry X-Gofastr-Title")
	}
	// Must be header-safe ASCII — a raw UTF-8 title (em-dash separator)
	// would arrive mojibaked when a reader decodes the header as Latin-1.
	for i := 0; i < len(h); i++ {
		if h[i] > 127 {
			t.Fatalf("X-Gofastr-Title must be ASCII/percent-encoded, got raw non-ASCII: %q", h)
		}
	}
	dec, err := url.PathUnescape(h)
	if err != nil {
		t.Fatalf("X-Gofastr-Title is not valid percent-encoding: %v", err)
	}
	if !strings.Contains(dec, "—") {
		t.Fatalf("decoded title should round-trip the em-dash separator, got %q", dec)
	}
}

func TestUIHost_PageSessionCookieUsesSecureFlag(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if !strings.Contains(rec.Header().Get("Set-Cookie"), "Secure") {
		t.Fatalf("SECURITY: [uihost-cookie] session cookie missing Secure flag: %q", rec.Header().Get("Set-Cookie"))
	}
}

func TestLoopbackDevCookieRoundTrips(t *testing.T) {
	ds := newTestUIHost()

	// A plaintext loopback origin must mint a cookie that the browser
	// will actually send back: no __Host- prefix, no Secure flag.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "localhost:8090"
	ds.ServeHTTP(rec, req)

	set := rec.Header().Get("Set-Cookie")
	if !strings.Contains(set, sessionCookieDevName+"=") {
		t.Fatalf("expected dev session cookie %q, got %q", sessionCookieDevName, set)
	}
	if strings.Contains(set, "__Host-") || strings.Contains(set, "Secure") {
		t.Fatalf("loopback http cookie must not be Secure/__Host- (would not round-trip): %q", set)
	}

	var val string
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieDevName {
			val = c.Value
		}
	}
	if val == "" {
		t.Fatal("no dev session cookie value minted")
	}

	// The minted cookie must satisfy requireValidSession on a gated
	// endpoint — this is the path that was 401-storming the console.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/__gofastr/widgets?page=/", nil)
	req2.Host = "localhost:8090"
	req2.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: val})
	ds.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("widgets with dev cookie: got %d, want 200", rec2.Code)
	}
}

func TestStaleSessionCookieReminted(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "localhost:8090"
	// A cookie left over from a prior process whose in-memory sessions
	// are gone. Reusing it would embed a dead SSE id and 401 forever.
	req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: "sess-deadbeef"})
	ds.ServeHTTP(rec, req)

	set := rec.Header().Get("Set-Cookie")
	if !strings.Contains(set, sessionCookieDevName+"=sess-") {
		t.Fatalf("expected a freshly minted session cookie, got %q", set)
	}
	if strings.Contains(set, "sess-deadbeef") {
		t.Fatalf("stale session id must not be reused: %q", set)
	}
}

func TestUIHost_PageResponsesCarrySecurityHeaders(t *testing.T) {
	ds := newTestUIHost()
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("SECURITY: [uihost-headers] page response missing X-Frame-Options DENY: %#v", rec.Header())
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("SECURITY: [uihost-headers] page response missing Content-Security-Policy header.")
	}
}

func TestWithHeadHTML_StripsInlineScriptTags(t *testing.T) {
	application := app.NewApp("HeadHTMLSecurity")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	host := New(application, WithHeadHTML(`<script>alert("xss")</script><meta name="safe" content="ok">`))

	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()

	if strings.Contains(page, `<script>alert("xss")</script>`) {
		t.Fatalf("SECURITY: [uihost-head] WithHeadHTML injected raw script tag into page head.")
	}
}

func TestSEOScreen_HeadHTMLStripsInlineScriptTags(t *testing.T) {
	application := app.NewApp("SEOHeadSecurity")
	application.SetDefaultLayout(app.NewLayout("main"))
	application.RegisterScreen(app.NewScreen("/about", &seoTestComp{
		headHTML: `<script>alert("xss")</script><meta name="safe" content="ok">`,
	}).WithTitle("About"), nil)
	host := New(application)

	rec := httptest.NewRecorder()
	host.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/about", nil))
	page := rec.Body.String()

	if strings.Contains(page, `<script>alert("xss")</script>`) {
		t.Fatalf("SECURITY: [uihost-head] SEOScreen HeadHTML injected raw script tag into page head.")
	}
}

func TestUIHost_ServerActionRequiresSession(t *testing.T) {
	a := app.NewApp("action-security")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)

	handlerCalled := false
	ic := &actionTestComp{
		actions: func() {
			component.On("test-action", func(ctx *component.ComponentContext) {
				handlerCalled = true
			})
		},
	}
	ds.CompileActions("test-comp", ic)

	body := strings.NewReader(`{"action":"test-action","params":{},"componentId":"test-comp"}`)
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/action", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		var result map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &result)
		t.Fatalf("SECURITY: [uihost-action] unauthenticated server action returned %d and invoked=%v body=%v. Attack: server action execution without session/auth.", rec.Code, handlerCalled, result)
	}
}

func TestUIHost_SSERejectsUnknownSessionID(t *testing.T) {
	ds := newTestUIHost()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/sse?session=forged-session", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [uihost-sse] unknown session id was accepted with status %d. Attack: subscribe to SSE stream with attacker-chosen session token.", rec.Code)
	}
}

// TestSSEStreamIDMustMatchCookie pins the branch's load-bearing invariant:
// possession of a valid session token proves ownership of ONLY the id
// embedded in it. A caller holding a fully valid cookie for session A must
// NOT be able to attach to session B's SSE stream by naming B in ?session=.
// The stream id is deliberately public (embedded in page chrome), so a
// leaked-but-real id must still be useless without the matching cookie —
// the sibling of the forged-id hole the old in-memory map never closed
// (see handleSSE, "subscribing to another user's stream with a
// leaked-but-real id and no matching cookie"). Loopback origin so the
// relaxed dev cookie round-trips in the test harness.
func TestSSEStreamIDMustMatchCookie(t *testing.T) {
	ds := newTestUIHost()
	victim := ds.CreateSession()   // stream id we must not be able to reach
	attacker := ds.CreateSession() // our own valid credential
	if victim.ID == attacker.ID || victim.Token == "" || attacker.Token == "" {
		t.Fatalf("test setup: need two distinct valid sessions, got %q / %q", victim.ID, attacker.ID)
	}

	// Cross-stream: valid cookie for the attacker's own session, but the
	// query names the victim's public stream id. Must be rejected.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/sse?session="+url.QueryEscape(victim.ID), nil).WithContext(ctx)
	req.Host = "localhost:8090"
	req.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: attacker.Token})
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [uihost-sse] a valid cookie for session A attached to session B's stream (status %d). Attack: eavesdrop on another viewer's island updates using their public stream id.", rec.Code)
	}

	// Control: the same valid cookie against its OWN stream id must NOT be
	// rejected — otherwise the test would pass for the wrong reason.
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	req2 := httptest.NewRequest(http.MethodGet, "/__gofastr/sse?session="+url.QueryEscape(attacker.ID), nil).WithContext(ctx2)
	req2.Host = "localhost:8090"
	req2.AddCookie(&http.Cookie{Name: sessionCookieDevName, Value: attacker.Token})
	rec2 := httptest.NewRecorder()
	ds.ServeHTTP(rec2, req2)
	if rec2.Code == http.StatusUnauthorized {
		t.Fatalf("a valid cookie against its own stream id was rejected (status %d); the mismatch check must key on id equality, not reject everything", rec2.Code)
	}
}
