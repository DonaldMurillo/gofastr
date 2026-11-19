package uihost

import (
	"cmp"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

// AgentReadyConfig configures the WithAgentReady bundle; the zero value
// installs nothing.
type AgentReadyConfig struct {
	BaseURL            string           // canonical origin; empty derives it per request
	Title              string           // the /llms.txt H1 and the card's name fallback
	Summary            string           // the /llms.txt lede and the card's description fallback
	Sections           []LLMsTxtSection // nil links /llm-pages.md when WithPublicLLMMD is on
	FullText           string           // served verbatim at /llms-full.txt
	AgentCard          *AgentCardConfig // nil derives a card from Title and Summary
	AllowAIBots        *bool            // adds an allow or deny robots.txt block for AI crawlers
	ContentSignals     string           // a robots.txt Content-Signal value, e.g. "ai-train=no"
	LinkHeaders        *bool            // discovery Link headers on HTML pages; nil means on
	OpenAPIEndpoint    string           // advertised as Link rel="service-desc" and in /llms.txt
	ContentNegotiation *bool            // markdown for Accept: text/markdown; nil means off
	WhenToUse          string           // the /llms.txt "## When to use" section
	CLI                *CLIToolConfig   // the /llms.txt "## CLI" section
}

// CLIToolConfig describes the official CLI advertised in /llms.txt.
type CLIToolConfig struct {
	Name    string // binary or package name
	Install string // copy-paste install command
	Docs    string // optional docs URL
}

// LLMsTxtSection is one H2 file-list section of /llms.txt.
type LLMsTxtSection struct {
	Title string // "Optional" is the spec's skippable-context section
	Links []LLMsTxtLink
}

// LLMsTxtLink is one entry in an /llms.txt section.
type LLMsTxtLink struct {
	Name  string // hyperlink text
	URL   string // absolute or root-relative URL
	Notes string // optional text after the colon
}

// AgentCardConfig configures the A2A v1.0 card at
// /.well-known/agent-card.json.
type AgentCardConfig struct {
	Name               string                         // defaults to "GoFastr Agent"
	Description        string                         // short summary
	Version            string                         // defaults to "1.0.0"
	URL                string                         // absolute service URL; empty uses the base URL
	MCPEndpoint        string                         // advertised as the JSON-RPC interface and a default "mcp" skill
	A2AEndpoint        string                         // advertised instead of MCPEndpoint; turns streaming and push on
	Skills             []AgentSkill                   // pass app.A2ASkills() with an A2AEndpoint
	Streaming          bool                           // the card's streaming capability
	PushNotifications  bool                           // the card's pushNotifications capability
	SecuritySchemes    map[string]AgentSecurityScheme // emitted as securitySchemes; nil omits it
	DefaultInputModes  []string                       // defaults to ["text/plain"]
	DefaultOutputModes []string                       // defaults to ["text/plain"]
	SigningKeys        []AgentCardSigningKey          // signs the card; needs a configured BaseURL or mount panics
}

// AgentSkill is one A2A AgentSkill; description and tags are always
// emitted, as "" and [] when unset.
type AgentSkill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	Examples    []string `json:"examples,omitempty"`
	InputModes  []string `json:"inputModes,omitempty"`
	OutputModes []string `json:"outputModes,omitempty"`
}

// MarshalJSON serializes tags as [] when nil instead of null.
func (s AgentSkill) MarshalJSON() ([]byte, error) {
	type plain AgentSkill
	if s.Tags == nil {
		s.Tags = []string{}
	}
	return json.Marshal(plain(s))
}

// AgentSecurityScheme is one OpenAPI-style security scheme, emitted as given.
type AgentSecurityScheme map[string]any

// WithAgentReady turns on the agent-discovery surface in one call. It
// merges with the granular options (WithLLMsTxt, WithAgentCard,
// WithMarkdownNegotiation, WithAgentLinkHeaders) in either order; a field
// the bundle sets wins.
func WithAgentReady(cfg AgentReadyConfig) Option {
	ar := &agentReadyConfig{baseURL: cfg.BaseURL}

	if cfg.Title != "" || cfg.Summary != "" || cfg.Sections != nil || cfg.WhenToUse != "" || cfg.CLI != nil {
		ar.llms = &llmsCfg{
			title:     cfg.Title,
			summary:   cfg.Summary,
			sections:  cfg.Sections,
			whenToUse: cfg.WhenToUse,
			cli:       cfg.CLI,
		}
	}

	switch {
	case cfg.AgentCard != nil:
		ar.card = cfg.AgentCard
	case cfg.Title != "":
		ar.card = &AgentCardConfig{Name: cfg.Title, Description: cfg.Summary}
	}

	if cfg.AllowAIBots != nil {
		b := *cfg.AllowAIBots
		ar.allowAIBots = &b
	}

	lh := true
	if cfg.LinkHeaders != nil {
		lh = *cfg.LinkHeaders
	}
	ar.linkHeaders = lh

	if cfg.ContentNegotiation != nil {
		ar.contentNeg = *cfg.ContentNegotiation
	}

	ar.openAPI = cfg.OpenAPIEndpoint
	ar.fullTxt = cfg.FullText
	ar.contentSignals = cfg.ContentSignals

	return func(ds *UIHost) {
		if ar.llms == nil && ar.card == nil && ar.allowAIBots == nil &&
			ar.baseURL == "" && !ar.contentNeg && ar.openAPI == "" && ar.contentSignals == "" &&
			ar.fullTxt == "" {
			return
		}
		cur := ds.agentReady
		if cur == nil {
			cur = &agentReadyConfig{}
		}
		if ar.baseURL != "" {
			cur.baseURL = ar.baseURL
		}
		if ar.llms != nil {
			cur.llms = mergeLLMsCfg(cur.llms, ar.llms)
		}
		if ar.card != nil {
			cur.card = mergeAgentCardCfg(cur.card, ar.card)
		}
		if ar.allowAIBots != nil {
			cur.allowAIBots = ar.allowAIBots
		}
		if cfg.LinkHeaders != nil {
			cur.linkHeaders = *cfg.LinkHeaders
		} else {
			cur.linkHeaders = cur.linkHeaders || ar.linkHeaders
		}
		if cfg.ContentNegotiation != nil {
			cur.contentNeg = *cfg.ContentNegotiation
		}
		if ar.openAPI != "" {
			cur.openAPI = ar.openAPI
		}
		if ar.contentSignals != "" {
			cur.contentSignals = ar.contentSignals
		}
		if ar.fullTxt != "" {
			cur.fullTxt = ar.fullTxt
		}
		ds.agentReady = cur
	}
}

func mergeLLMsCfg(previous, next *llmsCfg) *llmsCfg {
	merged := *next
	if previous != nil {
		merged.title = cmp.Or(merged.title, previous.title)
		merged.summary = cmp.Or(merged.summary, previous.summary)
		merged.whenToUse = cmp.Or(merged.whenToUse, previous.whenToUse)
		if merged.sections == nil {
			merged.sections = previous.sections
		}
		if merged.cli == nil {
			merged.cli = previous.cli
		}
	}
	return &merged
}

func mergeAgentCardCfg(previous, next *AgentCardConfig) *AgentCardConfig {
	merged := *next
	if previous != nil {
		merged.Name = cmp.Or(merged.Name, previous.Name)
		merged.Description = cmp.Or(merged.Description, previous.Description)
		merged.Version = cmp.Or(merged.Version, previous.Version)
		merged.URL = cmp.Or(merged.URL, previous.URL)
		merged.MCPEndpoint = cmp.Or(merged.MCPEndpoint, previous.MCPEndpoint)
		merged.A2AEndpoint = cmp.Or(merged.A2AEndpoint, previous.A2AEndpoint)
		merged.Streaming = merged.Streaming || previous.Streaming
		merged.PushNotifications = merged.PushNotifications || previous.PushNotifications
		if merged.Skills == nil {
			merged.Skills = previous.Skills
		}
		if merged.SecuritySchemes == nil {
			merged.SecuritySchemes = previous.SecuritySchemes
		}
		if merged.DefaultInputModes == nil {
			merged.DefaultInputModes = previous.DefaultInputModes
		}
		if merged.DefaultOutputModes == nil {
			merged.DefaultOutputModes = previous.DefaultOutputModes
		}
		if merged.SigningKeys == nil {
			merged.SigningKeys = previous.SigningKeys
		}
	}
	return &merged
}

// WithLLMsTxt serves /llms.txt from the given title, summary, and sections.
func WithLLMsTxt(title, summary string, sections []LLMsTxtSection) Option {
	return func(ds *UIHost) {
		if ds.agentReady == nil {
			ds.agentReady = &agentReadyConfig{}
		}
		ds.agentReady.llms = &llmsCfg{title: title, summary: summary, sections: sections}
	}
}

// WithLLMsFullTxt serves the given markdown verbatim at /llms-full.txt.
func WithLLMsFullTxt(content string) Option {
	return func(ds *UIHost) {
		if ds.agentReady == nil {
			ds.agentReady = &agentReadyConfig{}
		}
		ds.agentReady.fullTxt = content
	}
}

// WithAgentCard serves /.well-known/agent-card.json.
func WithAgentCard(cfg AgentCardConfig) Option {
	return func(ds *UIHost) {
		if ds.agentReady == nil {
			ds.agentReady = &agentReadyConfig{}
		}
		cc := cfg
		ds.agentReady.card = &cc
	}
}

// WithMarkdownNegotiation serves a page's markdown when the request
// accepts text/markdown; it needs WithPublicLLMMD.
func WithMarkdownNegotiation() Option {
	return func(ds *UIHost) {
		if ds.agentReady == nil {
			ds.agentReady = &agentReadyConfig{}
		}
		ds.agentReady.contentNeg = true
	}
}

// WithAgentLinkHeaders adds Link headers naming the configured discovery
// documents to HTML pages.
func WithAgentLinkHeaders() Option {
	return func(ds *UIHost) {
		if ds.agentReady == nil {
			ds.agentReady = &agentReadyConfig{}
		}
		ds.agentReady.linkHeaders = true
	}
}

// SetMCPManifestPath names the served MCP manifest for the discovery
// links; an empty path omits the link.
func (ds *UIHost) SetMCPManifestPath(path string) {
	ds.mcpManifestPath = path
}

type agentReadyConfig struct {
	baseURL        string
	llms           *llmsCfg
	card           *AgentCardConfig
	allowAIBots    *bool
	linkHeaders    bool
	contentNeg     bool
	openAPI        string
	contentSignals string
	fullTxt        string
	signing        *agentCardSigning
}

type llmsCfg struct {
	title     string
	summary   string
	sections  []LLMsTxtSection
	whenToUse string
	cli       *CLIToolConfig
}

func (ds *UIHost) mountAgentReady(r *router.Router) {
	if ds.agentReady == nil {
		return
	}
	if ds.agentReady.llms != nil {
		r.Get("/llms.txt", http.HandlerFunc(ds.handleLLMsTxt))
	}
	if ds.agentReady.fullTxt != "" {
		r.Get("/llms-full.txt", http.HandlerFunc(ds.handleLLMsFullTxt))
	}
	if ds.agentReady.card != nil {
		if len(ds.agentReady.card.SigningKeys) > 0 {
			keys, err := resolveSigningKeys(ds.agentReady.card.SigningKeys)
			if err != nil {
				panic(err)
			}
			base := ds.pinnedBaseURL()
			if base == "" {
				panic("uihost: agent card signing requires an explicit BaseURL " +
					"(WithAgentReady{BaseURL: ...} or WithSitemap{BaseURL: ...}): a signed " +
					"card whose URLs derive from the request Host header would hand an " +
					"attacker a validly-signed card pointing at their own host. Pin the " +
					"origin, or drop SigningKeys to serve an unsigned card.")
			}
			ds.agentReady.signing = &agentCardSigning{
				keys:    keys,
				jwksURL: base + jwksPath,
			}
			r.Get(jwksPath, http.HandlerFunc(ds.handleJWKS))
		}
		r.Get("/.well-known/agent-card.json", http.HandlerFunc(ds.handleAgentCard))
	}
}

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

func (ds *UIHost) handleLLMsFullTxt(w http.ResponseWriter, req *http.Request) {
	full := ds.agentReady.fullTxt
	if full == "" {
		http.NotFound(w, req)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write([]byte(full))
}

func renderLLMsTxt(cfg *llmsCfg, ds *UIHost) string {
	var b strings.Builder
	title := cfg.title
	if title == "" {
		title = "GoFastr Application"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	if cfg.summary != "" {
		for line := range strings.SplitSeq(strings.TrimSpace(cfg.summary), "\n") {
			fmt.Fprintf(&b, "> %s\n", line)
		}
		b.WriteString("\n")
	}
	if cfg.whenToUse != "" {
		fmt.Fprintf(&b, "## When to use\n\n%s\n\n", strings.TrimSpace(cfg.whenToUse))
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
	if cfg.cli != nil && cfg.cli.Install != "" {
		if cfg.cli.Name != "" {
			fmt.Fprintf(&b, "## CLI\n\nInstall `%s`:\n\n", cfg.cli.Name)
		} else {
			b.WriteString("## CLI\n\nInstall:\n\n")
		}
		fmt.Fprintf(&b, "```sh\n%s\n```\n\n", cfg.cli.Install)
		if cfg.cli.Docs != "" {
			fmt.Fprintf(&b, "Docs: [CLI documentation](%s)\n\n", cfg.cli.Docs)
		}
	}
	if ds != nil && ds.agentReady != nil {
		if ep := ds.agentReady.openAPI; ep != "" && !strings.Contains(b.String(), ep) {
			fmt.Fprintf(&b, "## API\n\n- [OpenAPI spec](%s): machine-readable API catalog\n\n", ep)
		}
		if ds.agentReady.card != nil && ds.agentReady.card.MCPEndpoint != "" &&
			!strings.Contains(b.String(), ds.agentReady.card.MCPEndpoint) {
			fmt.Fprintf(&b, "## MCP\n\n- [MCP endpoint](%s): Streamable HTTP JSON-RPC tool surface\n",
				ds.agentReady.card.MCPEndpoint)
			if ds.mcpManifestPath != "" && !strings.Contains(b.String(), ds.mcpManifestPath) {
				fmt.Fprintf(&b, "- [MCP manifest](%s): well-known discovery manifest\n", ds.mcpManifestPath)
			}
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func defaultLLMsSections(ds *UIHost) []LLMsTxtSection {
	if !ds.pageDocsOn() {
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

func (ds *UIHost) handleAgentCard(w http.ResponseWriter, req *http.Request) {
	cfg := ds.agentReady.card
	if cfg == nil {
		http.NotFound(w, req)
		return
	}
	base := ds.resolveBaseURL(req)
	doc := buildAgentCard(cfg, base)
	if ds.agentReady.signing != nil {
		sigs, err := signAgentCard(doc, ds.agentReady.signing)
		if err != nil {
			log.Printf("uihost: agent card signing failed: %v", err)
			i18n.HTTPError(w, req, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		doc["signatures"] = sigs
	}
	if !ds.baseURLPinned() {
		w.Header().Add("Vary", "Host")
		w.Header().Add("Vary", "X-Forwarded-Proto")
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		slog.Warn("uihost: encode agent card", "error", err)
		i18n.HTTPError(w, req, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(b)
	w.Write([]byte("\n"))
}

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

	streaming, push := cfg.Streaming, cfg.PushNotifications
	if cfg.A2AEndpoint != "" {
		streaming, push = true, true
	}
	doc := map[string]any{
		"name":        name,
		"description": cfg.Description,
		"version":     version,
		"capabilities": map[string]bool{
			"streaming":         streaming,
			"pushNotifications": push,
		},
		"skills":             skills,
		"defaultInputModes":  in,
		"defaultOutputModes": out,
	}
	if len(skills) == 0 {
		doc["skills"] = []AgentSkill{}
	}
	ifaceURL := serviceURL
	switch {
	case cfg.A2AEndpoint != "":
		ifaceURL = strings.TrimRight(baseURL, "/") + cfg.A2AEndpoint
	case cfg.MCPEndpoint != "":
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

func (ds *UIHost) resolveBaseURL(req *http.Request) string {
	if base := ds.pinnedBaseURL(); base != "" {
		return base
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	if ds.trustedFor(req).ContainsAddr(req.RemoteAddr) {
		if u := req.Header.Get("X-Forwarded-Proto"); u == "http" || u == "https" {
			scheme = u
		}
	}
	return scheme + "://" + req.Host
}

func (ds *UIHost) trustedFor(req *http.Request) clientip.Proxies {
	if len(ds.trustedProxies) > 0 {
		return ds.proxies
	}
	return clientip.TrustedFrom(req.Context())
}

func (ds *UIHost) baseURLPinned() bool {
	return ds.pinnedBaseURL() != ""
}

func acceptRangeMatches(accept string, wanted ...string) bool {
	for part := range strings.SplitSeq(accept, ",") {
		fields := strings.Split(part, ";")
		ct := strings.TrimSpace(fields[0])
		matched := false
		for _, w := range wanted {
			if strings.EqualFold(ct, w) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		for _, param := range fields[1:] {
			if k, v, ok := strings.Cut(strings.TrimSpace(param), "="); ok &&
				strings.EqualFold(strings.TrimSpace(k), "q") {
				if q, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil && q == 0 {
					matched = false
				}
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func acceptsMarkdown(r *http.Request) bool {
	return acceptRangeMatches(r.Header.Get("Accept"), "text/markdown")
}

func acceptsProblemJSON(r *http.Request) bool {
	return acceptRangeMatches(r.Header.Get("Accept"), "application/json", "application/problem+json")
}

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
	if ds.agentReady.card != nil && ds.agentReady.card.MCPEndpoint != "" {
		links = append(links, fmt.Sprintf(`<%s%s>; rel="service"`, base, ds.agentReady.card.MCPEndpoint))
	}
	if ds.agentReady.openAPI != "" {
		links = append(links, fmt.Sprintf(`<%s%s>; rel="service-desc"; type="application/json"`, base, ds.agentReady.openAPI))
	}
	if ds.pageDocsOn() {
		if scr, _, ok := ds.App.Router.Resolve(routePath(req)); ok && !scr.NoLLMMD {
			links = append(links, fmt.Sprintf(`<%s%s>; rel="alternate"; type="text/markdown"`, base, markdownAlternate(req.URL.EscapedPath())))
		}
	}
	if len(links) == 0 {
		return
	}
	if !ds.baseURLPinned() {
		w.Header().Add("Vary", "Host")
		w.Header().Add("Vary", "X-Forwarded-Proto")
	}
	prev := w.Header().Get("Link")
	joined := strings.Join(links, ", ")
	if prev != "" {
		joined = prev + ", " + joined
	}
	w.Header().Set("Link", joined)
}

func markdownAlternate(path string) string {
	var b strings.Builder
	b.Grow(len(path))
	for i := range len(path) {
		if c := path[i]; c >= 0x20 || c == '\t' {
			b.WriteByte(c)
		}
	}
	path = b.String()
	if path == "" || path == "/" {
		return "/llm.md"
	}
	return strings.TrimRight(path, "/") + "/llm.md"
}

func (ds *UIHost) pageDocsOn() bool {
	return ds.App != nil && ds.llmMDPublic && !ds.App.NoLLMMD
}

func setPageMarkdownHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Add("Vary", "Cookie")
}

func (ds *UIHost) serveMarkdownForPage(w http.ResponseWriter, r *http.Request) bool {
	if !ds.pageDocsOn() {
		return false
	}
	screen, _, ok := ds.App.Router.Resolve(routePath(r))
	if !ok || screen.NoLLMMD {
		return false
	}
	ctx, recorded := app.TrackStatus(app.WithRequest(r.Context(), r))
	res, ok := app.ScreenLLMMDForPath(ctx, ds.App, routePath(r))
	if !ok {
		return false
	}
	status := pageStatus(app.RenderResult{Component: res.Component}, recorded())
	switch res.Decision.Kind {
	case app.DecisionRedirect:
		w.Header().Add("Vary", "Accept")
		http.Redirect(w, r, scrubCtl(res.Decision.URL), http.StatusSeeOther)
		return true
	case app.DecisionBlock:
		status = res.Decision.Status
		if status < 400 || status > 599 {
			status = http.StatusInternalServerError
		}
	}
	md := ds.LLMMDWithSEO(ctx, routePath(r), screen, res)
	w.Header().Add("Vary", "Accept")
	setPageMarkdownHeaders(w)
	if status != 0 {
		w.WriteHeader(status)
	}
	w.Write([]byte(md))
	return true
}
