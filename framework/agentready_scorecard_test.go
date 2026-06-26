package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// TestAgentReady_Scorecard replicates all 11 isitagentready.com scanner
// checks (per PromptMention/isitagentready's packages/core/src/index.ts)
// against a FULLY-wired app+host, asserting 11/11. It is the definitive
// answer to "can the framework score 100%?" and a permanent regression
// guard: drop or break any check and this fails.
//
// The dogfood docs site (examples/site) legitimately scores lower — it has
// no API catalog (no entities) and isn't an OAuth issuer. This test wires
// EVERY surface so the maximum is provably reachable + guarded.

type scorecardScreen struct{}

func (scorecardScreen) Load(context.Context) error { return nil }
func (scorecardScreen) Render() render.HTML {
	return render.HTML("<html><head><title>Home</title></head><body><h1>Home</h1></body></html>")
}
func (s scorecardScreen) RenderCtx(context.Context) render.HTML { return s.Render() }

func TestAgentReady_Scorecard_AllElevenPass(t *testing.T) {
	botsOn := true
	mdNeg := true
	coreApp := app.NewApp("scorecard")
	coreApp.RegisterScreen(app.NewScreen("/", &scorecardScreen{}).WithTitle("Home"), nil)

	host := uihost.New(coreApp,
		uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://scorecard.test"}),
		uihost.WithRobots(uihost.RobotsConfig{}),
		uihost.WithPublicLLMMD(),
		uihost.WithAgentReady(uihost.AgentReadyConfig{
			Title:              "Scorecard",
			Summary:            "Scorecard test app.",
			AllowAIBots:        &botsOn,
			ContentSignals:     "ai-train=no, search=yes, ai-input=yes",
			ContentNegotiation: &mdNeg,
			AgentCard:          &uihost.AgentCardConfig{Name: "Scorecard", MCPEndpoint: "/mcp"},
		}),
	)

	fwApp := NewUIHostApp(host,
		WithConfig(AppConfig{Name: "scorecard"}),
		WithMCP(),
		WithOAuthProtectedResource(OAuthProtectedResourceConfig{Resource: "https://scorecard.test"}),
		WithOAuthAuthorizationServer(OAuthAuthorizationServerConfig{Issuer: "https://scorecard.test"}),
	)
	// An entity ⇒ hasAPI ⇒ /openapi.json + /.well-known/api-catalog mount.
	fwApp.Entity("posts", entity.EntityConfig{
		Table:  "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))

	a, cleanup := startApp(t, fwApp)
	defer cleanup()

	get := func(path, accept string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if accept != "" {
			req.Header.Set("Accept", accept)
		}
		req.Host = "scorecard.test"
		a.router.ServeHTTP(rec, req)
		return rec
	}

	robotsBody := get("/robots.txt", "").Body.String()
	aiBotRe := regexp.MustCompile(`(?i)gptbot|claude-web|anthropic-ai|google-extended|perplexitybot|ccbot|bytespider`)
	contentSignalRe := regexp.MustCompile(`(?im)^content-signal:`)

	checks := []struct {
		name string
		pass bool
	}{
		{"robots-txt", get("/robots.txt", "").Code == http.StatusOK &&
			strings.Contains(get("/robots.txt", "").Header().Get("Content-Type"), "text/plain") &&
			strings.Contains(robotsBody, "User-agent:")},
		{"sitemap", func() bool {
			r := get("/sitemap.xml", "")
			return r.Code == http.StatusOK && strings.Contains(r.Header().Get("Content-Type"), "xml")
		}()},
		{"link-headers", get("/", "").Header().Get("Link") != ""},
		{"markdown-negotiation", func() bool {
			r := get("/", "text/markdown")
			return r.Code == http.StatusOK && strings.Contains(r.Header().Get("Content-Type"), "text/markdown")
		}()},
		{"ai-bot-rules", aiBotRe.MatchString(robotsBody)},
		{"content-signals", contentSignalRe.MatchString(robotsBody)},
		{"api-catalog", get("/.well-known/api-catalog", "").Code == http.StatusOK},
		{"oauth-protected-resource", get("/.well-known/oauth-protected-resource", "").Code == http.StatusOK},
		{"mcp-server-card", get("/.well-known/mcp/server-card.json", "").Code == http.StatusOK},
		{"agent-skills-index", get("/.well-known/agent-skills/index.json", "").Code == http.StatusOK},
		{"oauth-authorization-server", get("/.well-known/oauth-authorization-server", "").Code == http.StatusOK},
	}

	passed := 0
	for _, c := range checks {
		if c.pass {
			passed++
		} else {
			t.Errorf("FAIL: %s", c.name)
		}
	}
	t.Logf("agent-ready scorecard: %d/11 passed", passed)
	if passed != 11 {
		t.Errorf("expected 11/11, got %d/11", passed)
	}
}
