package uihost

// agentready.go — the agent-discovery surface that makes a GoFastr site
// score well on scanners like isitagentready.com. It is purely additive:
// every endpoint is opt-in, and the existing robots.txt / sitemap.xml /
// llm.md / OpenAPI surfaces are untouched. The pieces:
//
//   - /llms.txt                            (llmstxt.org) — curated markdown index
//   - /.well-known/agent-card.json         (A2A v1.0) — agent identity + skills
//   - /.well-known/agent.json              (legacy alias) — older A2A clients
//   - Link: response headers on HTML pages — point agents at all of the above
//   - Accept: text/markdown negotiation    — markdown for any HTML page
//
// WithAgentReady turns on the sane defaults in one call; WithLLMsTxt /
// WithAgentCard expose each piece granularly. AI-bot-aware robots rules
// are merged into handleRobots (seo.go) from the bundle, so they compose
// with an existing WithRobots config regardless of option order.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// ─── Public config types ───────────────────────────────────────────

// AgentReadyConfig configures the WithAgentReady bundle. The zero value
// is a no-op; set fields for the surfaces you want. Sensible defaults
// apply inside WithAgentReady.
type AgentReadyConfig struct {
	// BaseURL is the canonical origin (scheme://host) used to
	// absolutize URLs in the agent card and Link headers. When empty,
	// handlers derive it per request from the forwarded Host + the
	// request TLS/scheme — correct behind a proxy that sets Host.
	BaseURL string

	// Title is the /llms.txt H1 and the agent-card name fallback.
	Title string
	// Summary is the /llms.txt blockquote lede and the agent-card
	// description fallback.
	Summary string
	// Sections are the /llms.txt file-list sections (H2 + links). When
	// nil, a default "Docs" section links the app's /llm-pages.md index
	// (requires WithPublicLLMMD) and the per-screen /llm.md docs.
	Sections []LLMsTxtSection

	// AgentCard, when non-nil, serves /.well-known/agent-card.json plus
	// the legacy /.well-known/agent.json alias. When nil but the bundle
	// is on, a minimal card is derived from Title/Summary/BaseURL.
	AgentCard *AgentCardConfig

	// AllowAIBots, when non-nil, augments robots.txt with explicit
	// rules for the common AI crawlers (GPTBot, ClaudeBot, Google-Extended,
	// …). true → allow; false → deny. nil → no AI-bot block.
	AllowAIBots *bool

	// ContentSignals, when set, emits a Content-Signal: directive line in
	// robots.txt declaring AI usage preferences (e.g.
	// "ai-train=no, search=yes, ai-input=yes"). See contentsignals.org.
	ContentSignals string

	// LinkHeaders, when non-nil, toggles Link response headers on every
	// HTML page. Default (nil → bundle sets true) advertises the sitemap,
	// llms.txt, agent card, and the per-page markdown alternate.
	LinkHeaders *bool

	// OpenAPIEndpoint, when set (e.g. "/openapi.json"), is advertised via
	// a Link: rel="service-desc" header so agents discover the API
	// catalog. Set it to the path the framework serves the OpenAPI spec
	// at (requires WithPublicOpenAPI so the spec is reachable).
	OpenAPIEndpoint string

	// ContentNegotiation, when non-nil, toggles serving a markdown
	// rendering of any HTML page when the request Accepts
	// text/markdown. Default off (nil → bundle leaves it off unless set).
	ContentNegotiation *bool
}

// LLMsTxtSection is one H2 file-list section of /llms.txt.
type LLMsTxtSection struct {
	// Title is the H2 heading. The special title "Optional" renders the
	// spec's skippable-context section.
	Title string
	// Links are the markdown `[name](url): notes` entries.
	Links []LLMsTxtLink
}

// LLMsTxtLink is one entry in an /llms.txt section.
type LLMsTxtLink struct {
	Name  string // hyperlink text
	URL   string // absolute or root-relative URL
	Notes string // optional text after the colon
}

// AgentCardConfig configures the A2A /.well-known/agent-card.json. The
// card is the discovery artifact: even without a full A2A task server, a
// valid card advertises the agent's identity, service URL, and skills.
type AgentCardConfig struct {
	// Name is the human-readable agent name (REQUIRED by A2A).
	Name string
	// Description is a short summary.
	Description string
	// Version is the agent/software version. Defaults to "1.0.0".
	Version string
	// URL is the agent's service endpoint (absolute). When empty,
	// derived from BaseURL.
	URL string
	// MCPEndpoint, when set (e.g. "/mcp"), advertises the MCP endpoint
	// as the card's JSON-RPC service interface and a skill pointing
	// agents at it.
	MCPEndpoint string

	// Skills are the agent's capabilities. When empty and MCPEndpoint is
	// set, a default "mcp" skill is emitted; hosts can declare richer
	// skills (one per domain capability).
	Skills []AgentSkill

	// Streaming / push-notification capability flags. Default false.
	Streaming         bool
	PushNotifications bool

	// SecuritySchemes, when non-nil, is emitted verbatim under
	// security_schemes (OpenAPI-style). nil omits the field.
	SecuritySchemes map[string]AgentSecurityScheme

	// DefaultInputModes / DefaultOutputModes are the MIME types the
	// agent accepts/emits. Default ["text/plain"].
	DefaultInputModes  []string
	DefaultOutputModes []string
}

// AgentSkill is one A2A AgentSkill.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Examples    []string `json:"examples,omitempty"`
}

// AgentSecurityScheme is an OpenAPI-style security scheme emitted in the
// card's security_schemes. The raw map lets hosts express any scheme
// shape (apiKey, http OAuth2, etc.) without the framework modeling each.
type AgentSecurityScheme map[string]any

// ─── Options ───────────────────────────────────────────────────────

// WithAgentReady turns on the agent-discovery surface in one call. It is
// the recommended entry point; pass a populated AgentReadyConfig. Each
// piece is also available as a granular Option (WithLLMsTxt,
// WithAgentCard) for fine control.
func WithAgentReady(cfg AgentReadyConfig) Option {
	ar := &agentReadyConfig{baseURL: cfg.BaseURL}

	// /llms.txt — on whenever a Title is set or sections are provided.
	if cfg.Title != "" || len(cfg.Sections) > 0 {
		ar.llms = &llmsCfg{title: cfg.Title, summary: cfg.Summary, sections: cfg.Sections}
	}

	// Agent card — explicit config, or a minimal derived card when the
	// bundle is on with a title.
	switch {
	case cfg.AgentCard != nil:
		ar.card = cfg.AgentCard
	case cfg.Title != "":
		ar.card = &AgentCardConfig{Name: cfg.Title, Description: cfg.Summary, Version: "1.0.0"}
	}

	// AI-bot robots block.
	if cfg.AllowAIBots != nil {
		b := *cfg.AllowAIBots
		ar.allowAIBots = &b
	}

	// Link headers — default on for the bundle.
	lh := true
	if cfg.LinkHeaders != nil {
		lh = *cfg.LinkHeaders
	}
	ar.linkHeaders = lh

	// Content negotiation — default off.
	if cfg.ContentNegotiation != nil {
		ar.contentNeg = *cfg.ContentNegotiation
	}

	// OpenAPI service-desc link — opt-in (path the host serves the spec at).
	ar.openAPI = cfg.OpenAPIEndpoint

	// Content-Signal robots directive — opt-in preference string.
	ar.contentSignals = cfg.ContentSignals

	return func(ds *UIHost) {
		// The zero value is a no-op (matches the doc): don't install the
		// surface unless something meaningful is configured. linkHeaders
		// alone — with nothing to link to — doesn't justify it.
		if ar.llms == nil && ar.card == nil && ar.allowAIBots == nil &&
			ar.baseURL == "" && !ar.contentNeg && ar.openAPI == "" && ar.contentSignals == "" {
			return
		}
		ds.agentReady = ar
	}
}

// WithLLMsTxt serves /llms.txt from the given title, summary, and
// sections. Granular alternative to the WithAgentReady bundle.
func WithLLMsTxt(title, summary string, sections []LLMsTxtSection) Option {
	return func(ds *UIHost) {
		if ds.agentReady == nil {
			ds.agentReady = &agentReadyConfig{}
		}
		ds.agentReady.llms = &llmsCfg{title: title, summary: summary, sections: sections}
	}
}

// WithAgentCard serves /.well-known/agent-card.json (+ legacy
// /.well-known/agent.json). Granular alternative to the bundle.
func WithAgentCard(cfg AgentCardConfig) Option {
	return func(ds *UIHost) {
		if ds.agentReady == nil {
			ds.agentReady = &agentReadyConfig{}
		}
		cc := cfg
		ds.agentReady.card = &cc
	}
}

// WithMarkdownNegotiation makes HTML pages serve a markdown rendering
// when the request Accepts text/markdown. Requires WithPublicLLMMD so
// the per-screen markdown renderers are available.
func WithMarkdownNegotiation() Option {
	return func(ds *UIHost) {
		if ds.agentReady == nil {
			ds.agentReady = &agentReadyConfig{}
		}
		ds.agentReady.contentNeg = true
	}
}

// WithAgentLinkHeaders emits Link response headers on HTML pages
// advertising the configured discovery artifacts (sitemap, llms.txt,
// agent card, markdown alternate). Granular alternative to the bundle.
func WithAgentLinkHeaders() Option {
	return func(ds *UIHost) {
		if ds.agentReady == nil {
			ds.agentReady = &agentReadyConfig{}
		}
		ds.agentReady.linkHeaders = true
	}
}

// ─── Internal config ───────────────────────────────────────────────

// agentReadyConfig is the resolved, internal form stored on *UIHost.
type agentReadyConfig struct {
	baseURL        string
	llms           *llmsCfg
	card           *AgentCardConfig
	allowAIBots    *bool
	linkHeaders    bool
	contentNeg     bool
	openAPI        string // OpenAPI spec path → Link: rel="service-desc"
	contentSignals string // Content-Signal: robots directive value
}

type llmsCfg struct {
	title    string
	summary  string
	sections []LLMsTxtSection
}

// ─── Route mounting ────────────────────────────────────────────────

// mountAgentReady registers the agent-discovery routes the bundle/granular
// options enabled. Called from Mount, next to the robots/sitemap block.
func (ds *UIHost) mountAgentReady(r *router.Router) {
	if ds.agentReady == nil {
		return
	}
	if ds.agentReady.llms != nil {
		r.Get("/llms.txt", http.HandlerFunc(ds.handleLLMsTxt))
	}
	if ds.agentReady.card != nil {
		// A2A v1.0 canonical path + the older agent.json alias so both
		// current and legacy clients resolve the card.
		r.Get("/.well-known/agent-card.json", http.HandlerFunc(ds.handleAgentCard))
		r.Get("/.well-known/agent.json", http.HandlerFunc(ds.handleAgentCard))
	}
}

// ─── /llms.txt handler ─────────────────────────────────────────────

func (ds *UIHost) handleLLMsTxt(w http.ResponseWriter, req *http.Request) {
	cfg := ds.agentReady.llms
	if cfg == nil {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(renderLLMsTxt(cfg, ds)))
}

// renderLLMsTxt builds the markdown per llmstxt.org: H1 title, blockquote
// summary, then one H2 section per file-list. A nil-sections default links
// the app's /llm-pages.md index + per-screen docs when public.
func renderLLMsTxt(cfg *llmsCfg, ds *UIHost) string {
	var b strings.Builder
	title := cfg.title
	if title == "" {
		title = "GoFastr Application"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	if cfg.summary != "" {
		for _, line := range strings.Split(strings.TrimSpace(cfg.summary), "\n") {
			fmt.Fprintf(&b, "> %s\n", line)
		}
		b.WriteString("\n")
	}

	sections := cfg.sections
	if len(sections) == 0 {
		sections = defaultLLMsSections(ds)
	}
	for _, sec := range sections {
		fmt.Fprintf(&b, "## %s\n\n", sec.Title)
		for _, l := range sec.Links {
			if l.Notes != "" {
				fmt.Fprintf(&b, "- [%s](%s): %s\n", l.Name, l.URL, l.Notes)
			} else {
				fmt.Fprintf(&b, "- [%s](%s)\n", l.Name, l.URL)
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// defaultLLMsSections builds a Docs section linking the app's existing
// /llm-pages.md markdown index, so a bundle host with WithPublicLLMMD
// gets a useful llms.txt for free. The index itself enumerates every
// screen + its /llm.md doc — we link just the index rather than every
// route, because non-screen routes (/api/*, /healthz, /.well-known/*,
// …) have no markdown counterpart and would produce dead links.
func defaultLLMsSections(ds *UIHost) []LLMsTxtSection {
	if !ds.llmMDPublic {
		return nil
	}
	return []LLMsTxtSection{{
		Title: "Docs",
		Links: []LLMsTxtLink{{
			Name:  "Documentation index",
			URL:   "/llm-pages.md",
			Notes: "Markdown index of every screen + its per-screen /llm.md doc",
		}},
	}}
}

// ─── Agent card handler ────────────────────────────────────────────

func (ds *UIHost) handleAgentCard(w http.ResponseWriter, req *http.Request) {
	cfg := ds.agentReady.card
	if cfg == nil {
		http.NotFound(w, req)
		return
	}
	base := ds.resolveBaseURL(req)
	doc := buildAgentCard(cfg, base)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(doc)
}

// buildAgentCard assembles the A2A v1.0 card as an ordered map so the
// JSON reads naturally (name, description, version, url, interfaces,
// capabilities, skills, security, modes).
func buildAgentCard(cfg *AgentCardConfig, baseURL string) map[string]any {
	name := cfg.Name
	if name == "" {
		name = "GoFastr Agent"
	}
	version := cfg.Version
	if version == "" {
		version = "1.0.0"
	}
	serviceURL := cfg.URL
	if serviceURL == "" {
		serviceURL = baseURL
	}

	in := []string{"text/plain"}
	if len(cfg.DefaultInputModes) > 0 {
		in = cfg.DefaultInputModes
	}
	out := []string{"text/plain"}
	if len(cfg.DefaultOutputModes) > 0 {
		out = cfg.DefaultOutputModes
	}

	skills := cfg.Skills
	if len(skills) == 0 && cfg.MCPEndpoint != "" {
		skills = []AgentSkill{{
			ID:          "mcp",
			Name:        "Model Context Protocol tools",
			Description: "Call the server's MCP tools over JSON-RPC at " + cfg.MCPEndpoint,
			Tags:        []string{"mcp", "tools"},
		}}
	}

	doc := map[string]any{
		"name":        name,
		"description": cfg.Description,
		"version":     version,
		"capabilities": map[string]bool{
			"streaming":         cfg.Streaming,
			"pushNotifications": cfg.PushNotifications,
		},
		"skills":             skills, // REQUIRED (proto field 12); emit [] when empty, never omit
		"defaultInputModes":  in,
		"defaultOutputModes": out,
	}
	// skills is REQUIRED in A2A v1.0 — a nil slice marshals to null, so
	// force a non-nil empty array when there are no skills.
	if len(skills) == 0 {
		doc["skills"] = []AgentSkill{}
	}
	// supportedInterfaces is REQUIRED (proto field 3) and is the ONLY place
	// the service endpoint lives in v1.0 (no top-level url). Advertise the
	// JSON-RPC endpoint: the MCP endpoint when configured (it genuinely
	// speaks JSON-RPC — initialize/tools-list work), else the service URL.
	ifaceURL := serviceURL
	if cfg.MCPEndpoint != "" {
		ifaceURL = strings.TrimRight(baseURL, "/") + cfg.MCPEndpoint
	}
	doc["supportedInterfaces"] = []map[string]any{{
		"url":             ifaceURL,
		"protocolBinding": "JSONRPC",
		"protocolVersion": "1.0",
	}}
	if len(cfg.SecuritySchemes) > 0 {
		doc["securitySchemes"] = cfg.SecuritySchemes
	}
	return doc
}

// ─── Helpers ───────────────────────────────────────────────────────

// resolveBaseURL returns the canonical origin for absolute discovery URLs:
// the WithAgentReady BaseURL if set, else the WithSitemap BaseURL (so a host
// that configured one origin gets it everywhere), else derived per request
// from the forwarded scheme + Host.
func (ds *UIHost) resolveBaseURL(req *http.Request) string {
	if ds.agentReady != nil && ds.agentReady.baseURL != "" {
		return strings.TrimRight(ds.agentReady.baseURL, "/")
	}
	if ds.sitemapConfig != nil && ds.sitemapConfig.BaseURL != "" {
		return strings.TrimRight(ds.sitemapConfig.BaseURL, "/")
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	if u := req.Header.Get("X-Forwarded-Proto"); u != "" {
		scheme = u
	}
	host := req.Host
	if h := req.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	return scheme + "://" + host
}

// acceptsMarkdown reports whether the request's Accept header asks for
// text/markdown. Used by the content-negotiation path in handlePage.
func acceptsMarkdown(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept"), ",") {
		ct := strings.TrimSpace(strings.Split(part, ";")[0])
		if strings.EqualFold(ct, "text/markdown") {
			return true
		}
	}
	return false
}

// writeAgentLinkHeaders emits the Link response header advertising the
// configured discovery artifacts. Called from handlePage for HTML pages.
func (ds *UIHost) writeAgentLinkHeaders(w http.ResponseWriter, req *http.Request) {
	if ds.agentReady == nil || !ds.agentReady.linkHeaders {
		return
	}
	base := ds.resolveBaseURL(req)
	var links []string
	if ds.sitemapConfig != nil {
		links = append(links, fmt.Sprintf(`<%s/sitemap.xml>; rel="sitemap"; type="application/xml"`, base))
	}
	if ds.agentReady.llms != nil {
		links = append(links, fmt.Sprintf(`<%s/llms.txt>; rel="llms-txt"; type="text/plain"`, base))
	}
	if ds.agentReady.card != nil {
		links = append(links, fmt.Sprintf(`<%s/.well-known/agent-card.json>; rel="agent-card"; type="application/json"`, base))
	}
	// MCP endpoint (Streamable HTTP). Advertised from the agent card's
	// MCPEndpoint so a client following the Link header reaches the
	// server's JSON-RPC tool surface.
	if ds.agentReady.card != nil && ds.agentReady.card.MCPEndpoint != "" {
		links = append(links, fmt.Sprintf(`<%s%s>; rel="service"`, base, ds.agentReady.card.MCPEndpoint))
	}
	// OpenAPI spec (API catalog). Opt-in via AgentReadyConfig.OpenAPIEndpoint.
	if ds.agentReady.openAPI != "" {
		links = append(links, fmt.Sprintf(`<%s%s>; rel="service-desc"; type="application/json"`, base, ds.agentReady.openAPI))
	}
	if ds.llmMDPublic {
		links = append(links, fmt.Sprintf(`<%s%s>; rel="alternate"; type="text/markdown"`, base, markdownAlternate(req.URL.Path)))
	}
	if len(links) == 0 {
		return
	}
	// Append so a host/middleware that set Link headers already keeps them.
	prev := w.Header().Get("Link")
	joined := strings.Join(links, ", ")
	if prev != "" {
		joined = prev + ", " + joined
	}
	w.Header().Set("Link", joined)
}

// markdownAlternate maps an HTML page path to its /llm.md markdown
// counterpart (the GoFastr convention served via WithPublicLLMMD).
func markdownAlternate(path string) string {
	if path == "" || path == "/" {
		return "/llm.md"
	}
	return strings.TrimRight(path, "/") + "/llm.md"
}

// serveMarkdownForPage renders the markdown for the requested path via
// the per-screen LLM doc renderer. Returns false if no screen matches.
func (ds *UIHost) serveMarkdownForPage(w http.ResponseWriter, r *http.Request) bool {
	if ds.App == nil {
		return false
	}
	screen, _, ok := ds.App.Router.Resolve(r.URL.Path)
	if !ok {
		return false
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(app.ScreenLLMMD(screen)))
	return true
}
