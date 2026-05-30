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

type stubSignal struct {
	value any
}

func (s *stubSignal) GetAsInterface() interface{} { return s.value }
func (s *stubSignal) UpdateAsInterface(v interface{}) {
	s.value = v
}
func (s *stubSignal) Subscribe() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
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

func TestUIHost_SignalUpdateRequiresSession(t *testing.T) {
	ds := newTestUIHost()
	sig := &stubSignal{}
	ds.RegisterSignal("dangerous-signal", sig)

	body := strings.NewReader(`{"value":"<img src=x onerror=alert(1)>"}`)
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/signal/dangerous-signal", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [uihost-signal] unauthenticated signal update returned %d. Attack: global signal mutation without session/auth.", rec.Code)
	}
}

func TestUIHost_SignalUpdateRejectsForgedSessionQueryParam(t *testing.T) {
	ds := newTestUIHost()
	sig := &stubSignal{}
	ds.RegisterSignal("dangerous-signal", sig)

	body := strings.NewReader(`{"value":"tamper"}`)
	req := httptest.NewRequest(http.MethodPost, "/__gofastr/signal/dangerous-signal?session=fake-session", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("SECURITY: [uihost-signal] forged session query param accepted with status %d. Attack: signal mutation with attacker-chosen session id.", rec.Code)
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
