package framework

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type isAgenticScreen struct{}

func (isAgenticScreen) Load(context.Context) error { return nil }
func (isAgenticScreen) Render() render.HTML {
	return render.HTML("<html><head><title>Home</title></head><body><h1>Home</h1><h2>What it does</h2><p>Generates barcodes.</p></body></html>")
}
func (s isAgenticScreen) RenderCtx(context.Context) render.HTML { return s.Render() }

// TestAgentReady_IsAgentic replicates the framework-reproducible checks of
// the is-agentic.com scanner (the successor to isitagentready.com, whose 11
// checks TestAgentReady_Scorecard_AllElevenPass guards). The live scan of
// barcode.donaldmurillo.com (2026-08-27, 61/100) named the gaps; this test
// wires EVERY surface and pins that a fully-configured app answers all of
// them, so the maximum is provably reachable and guarded:
//
//   - openapi-spec: a public /openapi.json whose operations carry
//     operationId + description + typed schemas (also the scanner's
//     api-schema-analysis and function-calling-compat checks)
//   - json-error-responses / agent-friendly-404: nonexistent paths answer
//     404 with application/problem+json under a JSON Accept and a markdown
//     recovery body (links, no raw path reflection) under text/markdown
//   - markdown-negotiation-vary: every Accept-negotiated response carries
//     Vary: Accept so CDNs never cache the wrong variant
//   - agent-instruction / cli-tool / discoverability: llms.txt renders a
//     "When to use" section, a CLI install section, and links the OpenAPI
//     spec and MCP endpoint
//   - mcp-server: the standard /.well-known/mcp.json manifest names the
//     /mcp endpoint and its streamable-http transport
//   - org-schema-completeness: Organization JSON-LD with contactPoint and
//     PostalAddress in the homepage HTML
func TestAgentReady_IsAgentic(t *testing.T) {
	botsOn := true
	mdNeg := true
	coreApp := app.NewApp("isagentic")
	coreApp.RegisterScreen(app.NewScreen("/", &isAgenticScreen{}).WithTitle("Home"), nil)

	host := uihost.New(coreApp,
		uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://isagentic.test"}),
		uihost.WithRobots(uihost.RobotsConfig{}),
		uihost.WithPublicLLMMD(),
		uihost.WithOrganization(uihost.OrganizationConfig{
			Name:        "IsAgentic Test Co",
			URL:         "https://isagentic.test",
			Email:       "hello@isagentic.test",
			ContactType: "customer support",
			Address: uihost.PostalAddress{
				Street:     "1 Test Way",
				Locality:   "Testville",
				Region:     "TS",
				PostalCode: "00001",
				Country:    "US",
			},
		}),
		uihost.WithAgentReady(uihost.AgentReadyConfig{
			Title:              "IsAgentic",
			Summary:            "Scorecard test app for the is-agentic checks.",
			AllowAIBots:        &botsOn,
			ContentNegotiation: &mdNeg,
			OpenAPIEndpoint:    "/openapi.json",
			WhenToUse: "Reach for this service when you need barcodes " +
				"generated or decoded programmatically.",
			CLI: &uihost.CLIToolConfig{
				Name:    "isagentic",
				Install: "npm install -g @isagentic/cli",
				Docs:    "/docs/cli",
			},
			AgentCard: &uihost.AgentCardConfig{Name: "IsAgentic", MCPEndpoint: "/mcp"},
		}),
	)

	fwApp := NewUIHostApp(host,
		WithConfig(AppConfig{Name: "isagentic"}),
		WithMCP(),
		WithPublicOpenAPI(),
	)
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
		req.Host = "isagentic.test"
		a.router.ServeHTTP(rec, req)
		return rec
	}

	varyHasAccept := func(rec *httptest.ResponseRecorder) bool {
		for _, v := range rec.Header().Values("Vary") {
			for _, part := range strings.Split(v, ",") {
				if strings.EqualFold(strings.TrimSpace(part), "Accept") {
					return true
				}
			}
		}
		return false
	}

	// openapi-spec + api-schema-analysis + function-calling-compat: the
	// public spec exists and is self-describing.
	specOK := func() bool {
		r := get("/openapi.json", "application/json")
		if r.Code != http.StatusOK || !strings.Contains(r.Header().Get("Content-Type"), "json") {
			return false
		}
		var spec struct {
			Paths map[string]map[string]struct {
				OperationID string         `json:"operationId"`
				Description string         `json:"description"`
				Summary     string         `json:"summary"`
				Responses   map[string]any `json:"responses"`
			} `json:"paths"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &spec); err != nil || len(spec.Paths) == 0 {
			return false
		}
		seen := map[string]bool{}
		for _, ops := range spec.Paths {
			for _, op := range ops {
				if op.OperationID == "" || seen[op.OperationID] {
					return false
				}
				seen[op.OperationID] = true
				if op.Description == "" && op.Summary == "" {
					return false
				}
				if len(op.Responses) == 0 {
					return false
				}
			}
		}
		return true
	}()

	json404 := get("/definitely-not-here", "application/json")
	md404 := get("/definitely-not-here", "text/markdown")
	// Reflection guard: a hostile path must never land unescaped in the
	// markdown 404 body.
	hostile404 := get("/<script>alert(1)</script>", "text/markdown")
	mdHome := get("/", "text/markdown")
	htmlHome := get("/", "")

	llms := get("/llms.txt", "").Body.String()
	mcpManifest := get("/.well-known/mcp.json", "")

	apiMiss := get("/api/posts/does-not-exist", "application/json")

	checks := []struct {
		name string
		pass bool
	}{
		{"openapi-spec-self-describing", specOK},
		{"json-404-problem", json404.Code == http.StatusNotFound &&
			strings.Contains(json404.Header().Get("Content-Type"), "json") &&
			strings.Contains(json404.Body.String(), "404")},
		{"markdown-404-recovery", md404.Code == http.StatusNotFound &&
			strings.Contains(md404.Header().Get("Content-Type"), "text/markdown") &&
			strings.Contains(md404.Body.String(), "](/")},
		{"markdown-404-no-reflection", hostile404.Code == http.StatusNotFound &&
			!strings.Contains(hostile404.Body.String(), "<script>")},
		{"vary-accept-on-negotiated-page", varyHasAccept(mdHome) && varyHasAccept(htmlHome)},
		{"vary-accept-on-negotiated-404", varyHasAccept(md404) && varyHasAccept(json404)},
		{"llms-when-to-use", strings.Contains(llms, "## When to use") &&
			strings.Contains(llms, "Reach for this service")},
		{"llms-cli-section", strings.Contains(llms, "npm install -g @isagentic/cli")},
		{"llms-links-api-and-mcp", strings.Contains(llms, "/openapi.json") &&
			strings.Contains(llms, "/mcp")},
		{"mcp-manifest", mcpManifest.Code == http.StatusOK &&
			strings.Contains(mcpManifest.Header().Get("Content-Type"), "json") &&
			strings.Contains(mcpManifest.Body.String(), "/mcp") &&
			strings.Contains(mcpManifest.Body.String(), "streamable")},
		{"api-json-error", apiMiss.Code != http.StatusOK &&
			strings.Contains(apiMiss.Header().Get("Content-Type"), "json")},
		{"org-jsonld", func() bool {
			body := htmlHome.Body.String()
			return strings.Contains(body, "application/ld+json") &&
				strings.Contains(body, `"Organization"`) &&
				strings.Contains(body, `"contactPoint"`) &&
				strings.Contains(body, `"PostalAddress"`)
		}()},
	}

	passed := 0
	for _, c := range checks {
		if c.pass {
			passed++
		} else {
			t.Errorf("FAIL: %s", c.name)
		}
	}
	t.Logf("is-agentic scorecard: %d/%d passed", passed, len(checks))
}
