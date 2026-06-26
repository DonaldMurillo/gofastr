# Agent-readiness

AI agents (and scanners like [isitagentready.com](https://isitagentready.com/))
look for a small set of well-known discovery artifacts before they can use a
site: a curated `/llms.txt`, an A2A agent card, sitemap + robots, `Link`
response headers pointing at all of it, and markdown content negotiation.
GoFastr already ships the *plumbing* — MCP tools, an OpenAPI spec, per-screen
markdown docs, sitemap, robots — so the agent-readiness surface is mostly the
*discovery* layer that makes those capabilities findable.

Every piece below is **opt-in and additive**: existing robots/sitemap/openapi/
llm.md behavior is unchanged. Turn the sane defaults on in one call, or wire
each piece granularly.

## One-call bundle

`uihost.WithAgentReady` + `framework.WithMCP` is the full agent-ready shape:

```go
package main

import (
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func main() {
	host := uihost.New(app, uihost.WithAgentReady(uihost.AgentReadyConfig{
		BaseURL: "https://example.com",
		Title:   "Acme",
		Summary: "Acme is a billing console. MCP tools live at /mcp.",
		AgentCard: &uihost.AgentCardConfig{
			Name:        "Acme Agent",
			Description: "Operator agent for the Acme billing console.",
			MCPEndpoint: "/mcp",
		},
	}))
	// WithMCP auto-mounts /mcp (POST JSON-RPC + GET SSE) so the host
	// doesn't hand-wire the route the agent card advertises.
	fwApp := framework.NewUIHostApp(host, framework.WithMCP())
	fwApp.Start(":8080")
}
```

That serves `/llms.txt`, `/.well-known/agent-card.json` (+ legacy
`/.well-known/agent.json`), AI-bot-aware `/robots.txt`, and emits `Link`
response headers on every HTML page. `WithPublicLLMMD` (already common) makes
the bundle's default `/llms.txt` link the per-screen markdown index, and
enables markdown content negotiation when `WithMarkdownNegotiation` is added.

## The pieces

### `/llms.txt` — curated markdown index  (llmstxt.org)

```go
uihost.WithLLMsTxt("Acme", "A billing console.",
	[]uihost.LLMsTxtSection{
		{Title: "Docs", Links: []uihost.LLMsTxtLink{
			{Name: "Index", URL: "/llm-pages.md", Notes: "every screen"},
			{Name: "API", URL: "/openapi.json"},
		}},
		{Title: "Optional", Links: []uihost.LLMsTxtLink{
			{Name: "Changelog", URL: "/changelog.md"},
		}},
	})
```

The file is markdown per the spec: an H1 title, a `>` blockquote summary, then
one `## Section` per file-list of `- [name](url): notes`. A section titled
`Optional` is the spec's skippable-context list. When no sections are passed
(and the bundle is on with `WithPublicLLMMD`), a default **Docs** section links
the app's `/llm-pages.md` index — which itself enumerates every screen and its
per-screen `/llm.md` doc.

### A2A agent card  (Agent2Agent v1.0)

`/.well-known/agent-card.json` describes the agent's identity, service
endpoint, capabilities, and skills, conforming to the A2A v1.0 AgentCard
(camelCase JSON keys per ADR-001; `supportedInterfaces` and `skills` are
REQUIRED and always present). The service endpoint lives in
`supportedInterfaces[].url` — there is no top-level `url` in v1.0. When
`MCPEndpoint` is set, that endpoint is advertised as the JSON-RPC
interface (it genuinely speaks JSON-RPC — `initialize` and `tools/list`
work), and a derived `mcp` skill points agents at it.

| `AgentCardConfig` field | Purpose |
|---|---|
| `Name` *(required)* | Human-readable agent name. |
| `Description` | Short summary. |
| `Version` | Software version; defaults `1.0.0`. |
| `URL` | Fallback for the `supportedInterfaces[].url` when `MCPEndpoint` is unset; defaults to the resolved base URL. |
| `MCPEndpoint` | e.g. `"/mcp"` — advertised as `supportedInterfaces[].url` (baseURL + endpoint), plus a derived `mcp` skill + a `Link: rel="service"` header. |
| `Skills` | Declared capabilities; one derived `mcp` skill when empty + `MCPEndpoint` set. `skills` is always emitted (possibly `[]`). |
| `Streaming`, `PushNotifications` | Capability flags (default false). |
| `SecuritySchemes` | OpenAPI-style schemes under `securitySchemes`; omitted when nil. |
| `DefaultInputModes`, `DefaultOutputModes` | MIME types; default `["text/plain"]`. |

### AI-bot-aware robots

`WithAgentReady{AllowAIBots: boolPtr(true)}` augments `/robots.txt` with
explicit per-crawler rules (GPTBot, ClaudeBot, Google-Extended, CCBot, …) so
the site reads as agent-friendly to scanners; `false` denies them. It merges
into the existing `WithRobots` config regardless of option order. When
allowed, the bots are listed as consecutive `User-agent:` lines in the
main group (so they inherit the host's `Allow`/`Disallow` rules — a
standalone `Allow: /` group would shadow path-specific exclusions, since
RFC 9309 applies only a crawler's most-specific group). When denied,
each bot gets its own `Disallow: /` group.

### `Link:` response headers

`WithAgentReady` (or `WithAgentLinkHeaders`) emits a `Link` header on every
HTML page advertising the configured artifacts: `rel="sitemap"`,
`rel="llms-txt"`, `rel="agent-card"`, `rel="service"` (the MCP endpoint),
`rel="service-desc"` (the OpenAPI spec, when `OpenAPIEndpoint` is set), and
`rel="alternate"` type `text/markdown` (the page's `/llm.md`). Absolute URLs
use the resolved base URL (see below).

### Markdown content negotiation

`WithMarkdownNegotiation()` makes any HTML page serve its markdown rendering
when the request's `Accept` header prefers `text/markdown` (the Cloudflare
convention). Requires `WithPublicLLMMD` so the per-screen renderers are
available. Requests without the `Accept` header are unaffected.

### MCP auto-mount  (`framework.WithMCP`)

`framework.WithMCP()` exposes `app.MCP` at `/mcp` over Streamable HTTP (POST
JSON-RPC + GET Server-Sent Events), replacing the manual
`fwApp.Router().Handle("POST", "/mcp", fwApp.MCP)`. Combined with
`WithMCPIntrospection()`, the server's tools (routes, plugins, batteries,
config, readiness, framework docs) are reachable at the canonical endpoint the
agent card advertises. Calling `WithMCP` **and** manually mounting `/mcp`
panics with a route conflict — pick one.

### OAuth Protected Resource  (RFC 9728)

When the app exposes OAuth-token-protected resources (e.g. battery/auth's JWT
bearer API), `framework.WithOAuthProtectedResource` serves
`/.well-known/oauth-protected-resource` so a client can discover which
authorization servers mint accepted tokens, the supported scopes, and how to
present a bearer token:

```go
framework.WithOAuthProtectedResource(framework.OAuthProtectedResourceConfig{
	Resource:             "https://api.example.com",
	AuthorizationServers: []string{"https://auth.example.com"},
	ScopesSupported:      []string{"read", "write"},
})
```

The framework serves the document; emitting the companion
`WWW-Authenticate: … resource_metadata=…` header on 401s (RFC 9728 §5) is left
to the host's auth middleware so it can be scoped to the exact token-protected
routes.

### Scanner-conformance endpoints  (isitagentready.com)

The framework auto-serves the well-known artifacts the isitagentready
scanner scores, so a host wiring the basics passes without per-route work:

| Check | Endpoint | When served |
|---|---|---|
| API Catalog (RFC 9727) | `/.well-known/api-catalog` (linkset+json) | when the app has entities (`/openapi.json` exists) |
| MCP Server Card (SEP-2127) | `/.well-known/mcp/server-card.json` + spec-reserved `/mcp/server-card` + `/.well-known/mcp/catalog.json` | when `WithMCP` exposes `/mcp` |
| Agent Skills Index | `/.well-known/agent-skills/index.json` | always (empty list passes; `WithAgentSkills` adds entries) |
| OAuth Authorization Server (RFC 8414) | `/.well-known/oauth-authorization-server` | opt-in (`WithOAuthAuthorizationServer`) |
| Content Signals | `Content-Signal:` line in robots.txt | `AgentReadyConfig.ContentSignals` |
| Auth.md (WorkOS profile) | `/auth.md` (markdown) + `agent_auth` block in the OAuth AS metadata | opt-in (`WithAuthMD`) |

```go
framework.WithAgentSkills([]framework.AgentSkillEntry{{
    Name: "code-review", Description: "Review code.",
    URL: "/.well-known/agent-skills/code-review/SKILL.md", Digest: "sha256:...",
}})
framework.WithOAuthAuthorizationServer(framework.OAuthAuthorizationServerConfig{
    Issuer: "https://auth.example", TokenEndpoint: "https://auth.example/token",
})
```

The 11 scored isitagentready checks — robots.txt, Sitemap, Link headers,
Markdown negotiation, AI bot rules, Content Signals, API Catalog, OAuth
Protected Resource, MCP Server Card, Agent Skills Index, OAuth Authorization
Server — are all covered (6 always-on via the bundle; the rest opt-in /
conditional). The production scanner also lists: A2A card (covered —
`/.well-known/agent-card.json`), Auth.md (`WithAuthMD`), Web Bot Auth
(`WithWebBotAuth` — the site publishes a JWKS at
`/.well-known/http-message-signatures-directory` so it can sign its own
outbound requests), UCP (`WithUCP` → `/.well-known/ucp`), and ACP
(`WithACP` → `/.well-known/acp.json`). Not buildable as served routes:
DNS-AID (DNS SVCB/HTTPS + DNSSEC), x402 (HTTP 402 payment middleware),
MPP (payment execution + an `x-payment-info` OpenAPI extension needing a
payment backend), WebMCP (client-side browser API), ap2 (server-only).

## Base URL resolution

All absolute discovery URLs (agent card `url`, `Link` header targets) use one
canonical origin, resolved in this order: `WithAgentReady{BaseURL}`, then
`WithSitemap{BaseURL}`, then the per-request scheme + forwarded `Host`. Set one
origin and every artifact stays consistent, including behind a proxy that sets
`X-Forwarded-Proto` / `X-Forwarded-Host`.

## Granular options

| Option | Surface |
|---|---|
| `uihost.WithAgentReady(cfg)` | Bundle: llms.txt + card + AI-bot robots + Link headers (incl. OpenAPI `service-desc` when `cfg.OpenAPIEndpoint` is set, e.g. `"/openapi.json"`). |
| `uihost.WithLLMsTxt(title, summary, sections)` | `/llms.txt` only. |
| `uihost.WithAgentCard(cfg)` | `/.well-known/agent-card.json` + `agent.json` alias. |
| `uihost.WithAgentLinkHeaders()` | `Link:` headers on HTML only. |
| `uihost.WithMarkdownNegotiation()` | `Accept: text/markdown` → markdown. |
| `framework.WithMCP()` | Auto-mount `/mcp` (Streamable HTTP). |
| `framework.WithOAuthProtectedResource(cfg)` | RFC 9728 metadata doc. |
| `framework.WithAuthMD(cfg)` | `/auth.md` + `agent_auth` block. |
| `framework.WithWebBotAuth(cfg)` | `/.well-known/http-message-signatures-directory` JWKS. |
| `framework.WithAgentSkills(skills)` | `/.well-known/agent-skills/index.json`. |
| `framework.WithOAuthAuthorizationServer(cfg)` | RFC 8414 AS metadata. |
| `framework.WithUCP(cfg)` / `framework.WithACP(cfg)` | `/.well-known/ucp` / `/.well-known/acp.json`. |

## Common mistakes

- **Forgetting `WithMCP` (or a manual `/mcp` mount).** The agent card can
  advertise `/mcp`, but if nothing serves it the endpoint 404s. The bundle
  does *not* mount MCP for you — call `framework.WithMCP()` alongside it.
- **Advertising markdown negotiation without `WithPublicLLMMD`.**
  `WithMarkdownNegotiation` renders via the per-screen LLM doc, which only
  exists when the markdown surface is public. Without it, the negotiated
  response falls through to HTML.
- **Hand-writing per-route `/llm.md` links in `/llms.txt`.** Non-screen routes
  (`/api/*`, `/healthz`, `/.well-known/*`) have no markdown — link the
  `/llm-pages.md` index instead (the default does this).
- **Calling `WithMCP` and also mounting `/mcp` by hand.** Route conflict →
  panic at startup. Use one.

## What this deliberately does not do

- **No full A2A task server.** The card advertises the JSON-RPC endpoint
  (`/mcp`) in `supportedInterfaces` and is structurally conformant, but
  GoFastr serves MCP tool calls (`tools/list`, `tools/call`), not the
  A2A task lifecycle (`tasks/send`, streaming, push notifications). A
  client connecting to the advertised endpoint completes `initialize`
  and calls tools; it is not a multi-turn A2A task agent.
- **No DNS-AID.** DNS TXT records for AI discovery are infra/DNS, not
  framework code — add them at your registrar/host.
- **No inbound Web Bot Auth verification.** `WithWebBotAuth` publishes the
  site's signing JWKS (so it can sign its own outbound requests); verifying
  RFC 9421 signatures on *inbound* requests is host middleware, not a served
  artifact.
- **No x402 / MPP payment.** These need real payment middleware (HTTP 402 +
  payment requirements) or a payment backend; the framework serves discovery
  docs (UCP/ACP) but not payment execution.
