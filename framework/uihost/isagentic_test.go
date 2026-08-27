package uihost

// isagentic_test.go: focused unit tests for the is-agentic wave's
// uihost surfaces — llms.txt content sections + auto-links, the
// content-negotiated 404 (RFC 9457 problem+json / markdown recovery,
// with the no-path-reflection guard), Vary: Accept on every negotiated
// response, and the Organization JSON-LD head block (with the escaping
// guard). The cross-package contract lives in
// framework/agentready_isagentic_test.go; these pin the same behaviors
// at the unit level so a regression names the broken arm.

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// isoScreen renders a full document shell so head chrome (and the
// Organization JSON-LD inside it) has <head>/</head> markers to inject
// against, mirroring the framework-level contract test's screen.
type isoScreen struct{}

func (isoScreen) Load(context.Context) error { return nil }
func (isoScreen) Render() render.HTML {
	return render.HTML("<html><head><title>Home</title></head><body><h1>Home</h1></body></html>")
}
func (s isoScreen) RenderCtx(context.Context) render.HTML { return s.Render() }

func newISOHost(opts ...Option) *UIHost {
	a := app.NewApp("isagentic-uihost")
	a.RegisterScreen(app.NewScreen("/", &isoScreen{}).WithTitle("Home"), nil)
	return New(a, opts...)
}

func doGet(ds *UIHost, path, accept string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	ds.ServeHTTP(rec, req)
	return rec
}

func varyHasAccept(rec *httptest.ResponseRecorder) bool {
	for _, v := range rec.Header().Values("Vary") {
		for _, part := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(part), "Accept") {
				return true
			}
		}
	}
	return false
}

// ── llms.txt: When to use / CLI / auto-links ───────────────────────

func TestLLMsTxt_WhenToUseSection(t *testing.T) {
	ds := newISOHost(WithAgentReady(AgentReadyConfig{
		Title:     "Svc",
		WhenToUse: "Reach for this service when you need barcodes generated.",
	}))
	rec := doGet(ds, "/llms.txt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "## When to use") {
		t.Errorf("missing When to use heading:\n%s", body)
	}
	if !strings.Contains(body, "Reach for this service when you need barcodes generated.") {
		t.Errorf("missing WhenToUse body:\n%s", body)
	}
}

func TestLLMsTxt_CLISection(t *testing.T) {
	ds := newISOHost(WithAgentReady(AgentReadyConfig{
		Title: "Svc",
		CLI: &CLIToolConfig{
			Name:    "svc",
			Install: "npm install -g @svc/cli",
			Docs:    "/docs/cli",
		},
	}))
	body := doGet(ds, "/llms.txt", "").Body.String()
	if !strings.Contains(body, "## CLI") {
		t.Errorf("missing CLI heading:\n%s", body)
	}
	if !strings.Contains(body, "```sh\nnpm install -g @svc/cli\n```") {
		t.Errorf("install command not in a fenced block:\n%s", body)
	}
	if !strings.Contains(body, "[CLI documentation](/docs/cli)") {
		t.Errorf("docs link missing:\n%s", body)
	}
}

func TestLLMsTxt_AutoLinksAPIAndMCP(t *testing.T) {
	ds := newISOHost(WithAgentReady(AgentReadyConfig{
		Title:           "Svc",
		OpenAPIEndpoint: "/openapi.json",
		AgentCard:       &AgentCardConfig{Name: "Svc", MCPEndpoint: "/mcp"},
	}))
	body := doGet(ds, "/llms.txt", "").Body.String()
	for _, want := range []string{
		"## API", "(/openapi.json)",
		"## MCP", "(/mcp)", "(/.well-known/mcp.json)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("auto-link %q missing:\n%s", want, body)
		}
	}
}

func TestLLMsTxt_AutoLinksSkipHostSectionsThatCoverThem(t *testing.T) {
	// Host sections already link both endpoints: no duplicate API/MCP
	// sections may be appended.
	ds := newISOHost(WithAgentReady(AgentReadyConfig{
		Title:           "Svc",
		OpenAPIEndpoint: "/openapi.json",
		AgentCard:       &AgentCardConfig{Name: "Svc", MCPEndpoint: "/mcp"},
		Sections: []LLMsTxtSection{{Title: "Docs", Links: []LLMsTxtLink{
			{Name: "API", URL: "/openapi.json"},
			{Name: "MCP", URL: "/mcp"},
		}}},
	}))
	body := doGet(ds, "/llms.txt", "").Body.String()
	if strings.Contains(body, "## API") {
		t.Errorf("duplicate API section appended though host links it:\n%s", body)
	}
	if strings.Contains(body, "## MCP") {
		t.Errorf("duplicate MCP section appended though host links it:\n%s", body)
	}
	if c := strings.Count(body, "/openapi.json"); c != 1 {
		t.Errorf("openapi.json linked %d times, want 1:\n%s", c, body)
	}
}

// ── 404 content negotiation ────────────────────────────────────────

func TestNotFound_JSONProblem(t *testing.T) {
	// No agent-ready config at all: the problem+json arm is an
	// error-shape fix, always on.
	ds := newISOHost()
	rec := doGet(ds, "/definitely-not-here", "application/json")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type %q, want application/problem+json", ct)
	}
	if !varyHasAccept(rec) {
		t.Error("problem+json 404 must carry Vary: Accept")
	}
	var doc struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Detail string `json:"detail"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("body is not valid JSON: %v\n%s", err, rec.Body.String())
	}
	if doc.Type != "about:blank" || doc.Title != "Not Found" || doc.Status != 404 || doc.Detail == "" {
		t.Errorf("RFC 9457 members wrong: %+v", doc)
	}
	// The request path must not appear anywhere in the machine body.
	if strings.Contains(rec.Body.String(), "definitely-not-here") {
		t.Errorf("404 problem body reflects the request path:\n%s", rec.Body.String())
	}
}

func TestNotFound_JSONProblemAcceptVariants(t *testing.T) {
	ds := newISOHost()
	for _, accept := range []string{
		"application/problem+json",
		"application/json, text/plain",
		"text/html, application/json;q=0.9",
	} {
		rec := doGet(ds, "/nope", accept)
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
			t.Errorf("Accept %q → Content-Type %q, want problem+json", accept, ct)
		}
	}
	// A browser-style Accept must keep the HTML 404.
	rec := doGet(ds, "/nope", "text/html,application/xhtml+xml,*/*;q=0.8")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("browser Accept → Content-Type %q, want text/html", ct)
	}
}

func TestNotFound_MarkdownRecovery(t *testing.T) {
	ds := newISOHost(WithMarkdownNegotiation())
	rec := doGet(ds, "/definitely-not-here", "text/markdown")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("Content-Type %q, want text/markdown", ct)
	}
	if !varyHasAccept(rec) {
		t.Error("markdown 404 must carry Vary: Accept")
	}
	body := rec.Body.String()
	for _, want := range []string{"[Home](/)", "[Site map](/sitemap.xml)", "[llms.txt](/llms.txt)"} {
		if !strings.Contains(body, want) {
			t.Errorf("recovery link %q missing:\n%s", want, body)
		}
	}
}

func TestNotFound_MarkdownNoReflectionGuard(t *testing.T) {
	// THE security guard of the markdown arm: a hostile path must
	// never land in the body, escaped or otherwise.
	ds := newISOHost(WithMarkdownNegotiation())
	rec := doGet(ds, "/<script>alert(1)</script>", "text/markdown")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "<script>") {
		t.Errorf("hostile path reflected into markdown 404 body:\n%s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "alert(1)") {
		t.Errorf("hostile payload text reflected into markdown 404 body:\n%s", rec.Body.String())
	}
}

func TestNotFound_MarkdownArmRequiresNegotiationOptIn(t *testing.T) {
	// Without WithMarkdownNegotiation (or the bundle's
	// ContentNegotiation), a text/markdown Accept still gets the HTML
	// 404: the recovery arm is an agent feature, opt-in like the rest.
	ds := newISOHost()
	rec := doGet(ds, "/nope", "text/markdown")
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type %q, want text/html without the opt-in", ct)
	}
}

func TestNotFound_HTMLArmCarriesVary(t *testing.T) {
	// The problem+json arm is always on, so even the HTML 404 varies
	// by Accept and must say so.
	ds := newISOHost()
	rec := doGet(ds, "/nope", "text/html")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
	if !varyHasAccept(rec) {
		t.Error("HTML 404 must carry Vary: Accept (json arm is always active)")
	}
	if !strings.Contains(rec.Body.String(), "404") {
		t.Errorf("HTML 404 body lost its status text:\n%s", rec.Body.String())
	}
}

// ── Vary: Accept on negotiated pages ───────────────────────────────

func TestNegotiatedPagesCarryVaryAccept(t *testing.T) {
	ds := newISOHost(WithPublicLLMMD(), WithMarkdownNegotiation())

	md := doGet(ds, "/", "text/markdown")
	if ct := md.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("markdown variant Content-Type %q", ct)
	}
	if !varyHasAccept(md) {
		t.Error("negotiated markdown response must carry Vary: Accept")
	}

	html := doGet(ds, "/", "")
	if ct := html.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("HTML variant Content-Type %q", ct)
	}
	if !varyHasAccept(html) {
		t.Error("HTML variant of a negotiable page must carry Vary: Accept")
	}
}

func TestNonNegotiatedPagesHaveNoVaryAccept(t *testing.T) {
	// Without the negotiation opt-in the URL has one representation;
	// no Vary is needed (and none may appear: over-emitting would
	// needlessly de-cache).
	ds := newISOHost()
	rec := doGet(ds, "/", "")
	if varyHasAccept(rec) {
		t.Error("non-negotiable page must not carry Vary: Accept")
	}
}
