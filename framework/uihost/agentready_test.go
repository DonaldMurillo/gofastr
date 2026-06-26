package uihost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func boolp(b bool) *bool { return &b }

// newAgentReadyHost builds a standalone UIHost with the given options,
// mirroring the seed_test harness.
func newAgentReadyHost(opts ...Option) *UIHost {
	a := app.NewApp("agentready-test")
	return New(a, opts...)
}

func getBody(t *testing.T, url string) (string, *http.Response) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	b, _ := io.ReadAll(resp.Body)
	return string(b), resp
}

// ── /llms.txt ──────────────────────────────────────────────────────

func TestLLMsTxt_RenderStructure(t *testing.T) {
	cfg := &llmsCfg{
		title:   "My App",
		summary: "A thing that does stuff.",
		sections: []LLMsTxtSection{
			{Title: "Docs", Links: []LLMsTxtLink{
				{Name: "Guide", URL: "https://x/guide.md", Notes: "start here"},
				{Name: "API", URL: "https://x/api.md"},
			}},
			{Title: "Optional", Links: []LLMsTxtLink{
				{Name: "Extras", URL: "https://x/extra.md"},
			}},
		},
	}
	out := renderLLMsTxt(cfg, newAgentReadyHost())
	if !strings.HasPrefix(out, "# My App\n") {
		t.Errorf("missing H1 title:\n%s", out)
	}
	if !strings.Contains(out, "> A thing that does stuff.") {
		t.Errorf("missing blockquote summary:\n%s", out)
	}
	if !strings.Contains(out, "## Docs\n") || !strings.Contains(out, "## Optional\n") {
		t.Errorf("missing H2 sections:\n%s", out)
	}
	if !strings.Contains(out, "- [Guide](https://x/guide.md): start here") {
		t.Errorf("missing link with notes:\n%s", out)
	}
	if !strings.Contains(out, "- [API](https://x/api.md)") {
		t.Errorf("missing link without notes:\n%s", out)
	}
}

func TestLLMsTxt_HTTP(t *testing.T) {
	ds := newAgentReadyHost(WithLLMsTxt("Site", "lede",
		[]LLMsTxtSection{{Title: "Docs", Links: []LLMsTxtLink{{Name: "A", URL: "/a.md"}}}}))
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	body, resp := getBody(t, srv.URL+"/llms.txt")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type %q want text/plain", ct)
	}
	if !strings.Contains(body, "# Site") || !strings.Contains(body, "- [A](/a.md)") {
		t.Errorf("unexpected llms.txt body:\n%s", body)
	}
}

func TestLLMsTxt_NotConfigured404(t *testing.T) {
	ds := newAgentReadyHost() // no WithLLMsTxt
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)
	if _, resp := getBody(t, srv.URL+"/llms.txt"); resp.StatusCode != 404 {
		t.Errorf("want 404, got %d", resp.StatusCode)
	}
}

// ── A2A agent card ─────────────────────────────────────────────────

func TestAgentCard_JSON(t *testing.T) {
	doc := buildAgentCard(&AgentCardConfig{
		Name:        "Acme Agent",
		Description: "does things",
		Version:     "2.0.0",
		URL:         "https://acme.example/agent",
		MCPEndpoint: "/mcp",
	}, "https://acme.example")
	if doc["name"] != "Acme Agent" {
		t.Errorf("name: %v", doc["name"])
	}
	if doc["version"] != "2.0.0" {
		t.Errorf("version: %v", doc["version"])
	}
	if doc["url"] != "https://acme.example/agent" {
		t.Errorf("url: %v", doc["url"])
	}
	// MCP must be advertised as a skill, NOT as a misleading A2A
	// supported_interfaces binding (no A2A server exists).
	if _, ok := doc["supported_interfaces"]; ok {
		t.Errorf("card must not advertise supported_interfaces without an A2A server: %v", doc["supported_interfaces"])
	}
	skills, ok := doc["skills"].([]AgentSkill)
	if !ok || len(skills) != 1 || skills[0].ID != "mcp" {
		t.Errorf("expected single derived mcp skill, got %v", doc["skills"])
	}
	if _, ok := doc["capabilities"].(map[string]bool); !ok {
		t.Errorf("missing capabilities: %v", doc["capabilities"])
	}
}

func TestAgentCard_HTTPAndAlias(t *testing.T) {
	ds := newAgentReadyHost(WithAgentCard(AgentCardConfig{
		Name: "X", Description: "d", MCPEndpoint: "/mcp",
	}))
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	for _, path := range []string{"/.well-known/agent-card.json", "/.well-known/agent.json"} {
		body, resp := getBody(t, srv.URL+path)
		if resp.StatusCode != 200 {
			t.Errorf("%s: status %d", path, resp.StatusCode)
			continue
		}
		if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("%s: Content-Type %q", path, ct)
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(body), &doc); err != nil {
			t.Errorf("%s: invalid JSON: %v", path, err)
			continue
		}
		if doc["name"] != "X" {
			t.Errorf("%s: name %v", path, doc["name"])
		}
	}
}

// ── AI-bot-aware robots ────────────────────────────────────────────

func TestRobots_AIBotBlock(t *testing.T) {
	// allowAIBots merges into the robots config regardless of option order.
	ds := newAgentReadyHost(
		WithRobots(RobotsConfig{}),
		WithAgentReady(AgentReadyConfig{AllowAIBots: boolp(true)}),
	)
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	body, resp := getBody(t, srv.URL+"/robots.txt")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{"User-agent: GPTBot", "User-agent: ClaudeBot", "Allow: /"} {
		if !strings.Contains(body, want) {
			t.Errorf("robots missing %q:\n%s", want, body)
		}
	}
}

func TestRobots_AIBotDeny(t *testing.T) {
	ds := newAgentReadyHost(
		WithRobots(RobotsConfig{}),
		WithAgentReady(AgentReadyConfig{AllowAIBots: boolp(false)}),
	)
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)
	body, _ := getBody(t, srv.URL+"/robots.txt")
	if !strings.Contains(body, "User-agent: GPTBot\nDisallow: /") {
		t.Errorf("deny rule missing:\n%s", body)
	}
}

// ── Link headers ───────────────────────────────────────────────────

func TestWriteAgentLinkHeaders(t *testing.T) {
	ds := newAgentReadyHost(
		WithSitemap(SitemapConfig{BaseURL: "https://ex.com"}),
		WithLLMsTxt("T", "s", nil),
		WithAgentCard(AgentCardConfig{Name: "N", MCPEndpoint: "/mcp"}),
		WithAgentLinkHeaders(),
	)
	req := httptest.NewRequest(http.MethodGet, "/any", nil)
	rec := httptest.NewRecorder()
	ds.writeAgentLinkHeaders(rec, req)
	link := rec.Header().Get("Link")
	for _, want := range []string{
		`rel="sitemap"`, `rel="llms-txt"`, `rel="agent-card"`, `rel="service"`,
		`https://ex.com/sitemap.xml`, `https://ex.com/llms.txt`,
		`https://ex.com/.well-known/agent-card.json`, `https://ex.com/mcp`,
	} {
		if !strings.Contains(link, want) {
			t.Errorf("Link missing %q:\n%s", want, link)
		}
	}
}

// ── markdown negotiation helpers ───────────────────────────────────

func TestAcceptsMarkdown(t *testing.T) {
	cases := map[string]bool{
		"text/markdown":                 true,
		"text/markdown; charset=utf-8":  true,
		"text/html, text/markdown, */*": true,
		"application/json":              false,
		"":                              false,
		"text/html":                     false,
	}
	for accept, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Accept", accept)
		if got := acceptsMarkdown(req); got != want {
			t.Errorf("acceptsMarkdown(%q) = %v, want %v", accept, got, want)
		}
	}
}

func TestMarkdownAlternate(t *testing.T) {
	cases := map[string]string{
		"/":          "/llm.md",
		"":           "/llm.md",
		"/docs":      "/docs/llm.md",
		"/docs/sub/": "/docs/sub/llm.md",
	}
	for in, want := range cases {
		if got := markdownAlternate(in); got != want {
			t.Errorf("markdownAlternate(%q) = %q, want %q", in, got, want)
		}
	}
}

// mdPageScreen is a minimal Component for markdown-negotiation tests —
// it only needs to resolve via Router.Resolve; ScreenLLMMD derives
// markdown from screen metadata, not Render().
type mdPageScreen struct{}

func (mdPageScreen) Load(context.Context) error            { return nil }
func (mdPageScreen) Render() render.HTML                   { return "" }
func (mdPageScreen) RenderCtx(context.Context) render.HTML { return "" }

// ── Gap: markdown content negotiation end-to-end ───────────────────

func TestMarkdownNegotiation_EndToEnd(t *testing.T) {
	a := app.NewApp("md-neg-test")
	a.RegisterScreen(app.NewScreen("/", &mdPageScreen{}).WithTitle("Home"), nil)
	ds := New(a,
		WithPublicLLMMD(),
		WithMarkdownNegotiation(),
	)
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	req.Header.Set("Accept", "text/markdown")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Fatalf("Accept: text/markdown → Content-Type %q, want text/markdown", ct)
	}
}

func TestMarkdownNegotiation_NoAcceptHeaderIsHTML(t *testing.T) {
	// A normal request (no Accept: text/markdown) must still get HTML.
	a := app.NewApp("md-neg-test2")
	a.RegisterScreen(app.NewScreen("/", &mdPageScreen{}).WithTitle("Home"), nil)
	ds := New(a, WithPublicLLMMD(), WithMarkdownNegotiation())
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("no Accept header → Content-Type %q, want text/html", ct)
	}
}

// ── Gap: default llms.txt section links /llm-pages.md when public ──

func TestDefaultLLMsSections_LLMMDPublic(t *testing.T) {
	ds := newAgentReadyHost(WithPublicLLMMD(), WithLLMsTxt("T", "s", nil))
	srv := httptest.NewServer(ds)
	t.Cleanup(srv.Close)
	body, _ := getBody(t, srv.URL+"/llms.txt")
	if !strings.Contains(body, "## Docs") || !strings.Contains(body, "/llm-pages.md") {
		t.Errorf("default llms.txt should link /llm-pages.md index:\n%s", body)
	}
}

func TestDefaultLLMsSections_NoneWhenPrivate(t *testing.T) {
	// Without WithPublicLLMMD there are no default links — only title+summary.
	ds := newAgentReadyHost(WithLLMsTxt("T", "s", nil))
	out := renderLLMsTxt(&llmsCfg{title: "T", summary: "s"}, ds)
	if strings.Contains(out, "## ") {
		t.Errorf("no sections expected without public llm.md:\n%s", out)
	}
}

// ── Gap: WithAgentReady bundle default-resolution ───────────────────

func TestWithAgentReady_BundleDefaults(t *testing.T) {
	// Title set → llms + card on; linkHeaders defaults true; contentNeg false.
	ds := newAgentReadyHost(WithAgentReady(AgentReadyConfig{Title: "X", Summary: "y"}))
	if ds.agentReady == nil {
		t.Fatal("bundle left agentReady nil")
	}
	if ds.agentReady.llms == nil {
		t.Error("Title set should turn on llms")
	}
	if ds.agentReady.card == nil {
		t.Error("Title set should turn on a derived card")
	}
	if !ds.agentReady.linkHeaders {
		t.Error("linkHeaders should default true in the bundle")
	}
	if ds.agentReady.contentNeg {
		t.Error("contentNeg should default false")
	}
}

func TestWithAgentReady_ZeroValueIsNoOp(t *testing.T) {
	ds := newAgentReadyHost(WithAgentReady(AgentReadyConfig{}))
	if ds.agentReady != nil {
		t.Error("zero-value AgentReadyConfig should be a no-op (agentReady nil)")
	}
}

// ── Gap: resolveBaseURL three-branch resolution ────────────────────

func TestResolveBaseURL(t *testing.T) {
	// Branch 1: WithAgentReady BaseURL wins.
	ds := newAgentReadyHost(
		WithAgentReady(AgentReadyConfig{BaseURL: "https://ar.example"}),
		WithSitemap(SitemapConfig{BaseURL: "https://sm.example"}),
	)
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	if got := ds.resolveBaseURL(req); got != "https://ar.example" {
		t.Errorf("agentReady base: got %q", got)
	}

	// Branch 2: falls back to sitemap BaseURL when agentReady base unset.
	ds2 := newAgentReadyHost(
		WithAgentReady(AgentReadyConfig{}),
		WithSitemap(SitemapConfig{BaseURL: "https://sm.example"}),
	)
	// AgentReadyConfig{} is a no-op (agentReady nil), so resolveBaseURL
	// must consult the sitemap base.
	if got := ds2.resolveBaseURL(req); got != "https://sm.example" {
		t.Errorf("sitemap base fallback: got %q", got)
	}

	// Branch 3: neither set → per-request scheme + Host.
	ds3 := newAgentReadyHost()
	req3 := httptest.NewRequest(http.MethodGet, "https://req.example/x", nil)
	if got := ds3.resolveBaseURL(req3); got != "https://req.example" {
		t.Errorf("per-request: got %q", got)
	}
}
