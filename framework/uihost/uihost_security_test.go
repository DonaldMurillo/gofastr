package uihost

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

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
