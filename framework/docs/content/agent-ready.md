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
`WithMCPIntrospection()`, the server's ten introspection tools —
`app_routes`, `app_plugins`, `app_batteries`, `app_modules`, `app_config`,
`app_readiness`, `app_routines`, `framework_docs_list`, `framework_docs_get`,
`framework_docs_search` — are reachable at the canonical endpoint the
agent card advertises. Calling `WithMCP` **and** manually mounting `/mcp`
panics with a route conflict — pick one. Blueprint-generated apps ship with
both options wired.

`framework.WithMCPControl()` adds the mutating counterpart —
`app_module_enable` / `app_module_disable` toggle registered modules on the
running app (persisted through the module store, dependency-checked). Keep
it off any `/mcp` reachable by untrusted callers.

Auth on the tool surface splits by kind: entity CRUD tools re-dispatch
through the router, so session/JWT auth, owner scoping, and RBAC apply
exactly as over HTTP (the caller's Cookie/Authorization from the `/mcp`
request carries through). Directly registered tools — custom
`app.MCP.RegisterTool` handlers and `Endpoint.MCPHandler` twins — run
without route middleware; gate them per-caller with `mcp.Gated` +
battery/auth's `auth.MCPUser()` / `auth.MCPRole(...)` (see
[plugins](plugins.md)).

Process modules (issue `#37`) add a third tool kind alongside the entity
CRUD tools and directly-registered handlers. Each tool a process module
exposes is registered under a reserved `module.` prefix —
`module.<name>.<tool>` — so two modules cannot collide and every call is
attributable to its owning module. A disabled module's tools are omitted
from `tools/list` and refused by `tools/call` (the composite call gate);
an enabled-but-down module's tools stay listed but return a retryable
temp-unavailable error while the child is not Ready. A tool call forwards
to the child through the same capability broker as the module's HTTP
routes — the agent's authority is delegated identically, and there is no
separate tool-permission vocabulary. See [process modules](process-modules.md).

**The dev loop implies all of it.** Under `gofastr dev` (`GOFASTR_DEV`),
`framework.NewApp` auto-enables the mount, introspection, and control;
battery/log auto-registers its `log_recent` / `log_filter` /
`log_metrics` / `log_set_level` debug tools; and every CRUD-enabled
entity serves its `{entity}_list/get/create/update/delete` data tools
without per-entity `mcp: true` — with zero options: the local dev loop
is livereload for agents. Opt out with `GOFASTR_DEV_MCP=0` (mirrors
`GOFASTR_DEV_LIVERELOAD=0`); a production `GOFASTR_ENV` always wins.
A dev-implied mount yields to a hand-wired `/mcp` route instead of
panicking, so older scaffolds keep working under `gofastr dev`.

### Rich tool results, resources, and MCP Apps

A tool handler returns `any`. By default a plain value is JSON-marshaled
into a single `{type:"text"}` block (unchanged). To emit richer content,
return one of `core/mcp`'s result types:

```go
// An image block — every MCP client renders it inline (no token bomb from
// a base64 string smuggled through text):
return mcp.ImageResult{Data: pngBytes, MimeType: "image/png"}, nil

// Structured output (validated against a declared outputSchema) plus
// explicit blocks. A structured-only result still mirrors a text block for
// clients that don't read structuredContent:
return mcp.ToolResult{
    Structured: map[string]any{"count": 3},
    Content:    []mcp.Content{mcp.TextContent("3 matches")},
}, nil
```

Declare a tool's output shape and attach `_meta` at registration with
options:

```go
app.MCP.RegisterTool(name, desc, inputSchema, handler,
    mcp.WithOutputSchema(schema),                    // → tools/list.outputSchema
    mcp.WithToolMeta(map[string]any{                 // → tools/list._meta (verbatim)
        "ui": map[string]any{"resourceUri": "ui://app/widget.html"},
    }),
)
```

**Resources.** `app.MCP.RegisterResource(uri, name, mimeType, contents)`
serves a resource via `resources/list` + `resources/read`; registering any
resource makes `initialize` advertise the `resources` capability. The
contents func runs per read and may return text or a binary blob (base64 on
the wire). Attach resource `_meta` with `mcp.WithResourceMeta(...)`. Note
resources are **not** covered by the tool call gate — `mcp.Gated` /
`auth.MCPUser` gate tool handlers, not `resources/read`. Public content (an
MCP App's widget HTML) needs no gating; to serve sensitive or per-caller
data, add `mcp.WithResourceGate(gate)` (the resource-side analogue of
`mcp.Gated` — `auth.MCPUser()` / `auth.MCPRole(...)` work as gates), which
runs before the contents func on every read.

**MCP Apps.** The [MCP Apps extension](https://modelcontextprotocol.io/extensions/apps/overview)
lets a tool declare an interactive HTML widget the host renders in a
sandboxed iframe. `framework.WithMCPApp` wires both halves — the `ui://`
resource carrying the HTML and the tool whose `_meta` links to it (with the
ChatGPT Apps SDK `openai/outputTemplate` compat alias) — in one call:

```go
framework.WithMCPApp(mcp.AppConfig{
    Name:        "studio",
    Description: "Open the studio widget.",
    InputSchema: schema,
    Handler:     studioTool,
    ResourceURI: "ui://myapp/studio.html",
    HTML:        studioHTML,            // self-contained, inline JS/CSS
    CSP:         "default-src 'self'",  // rides on the resource's _meta.ui
})
```

The widget HTML is the app author's job (a single vanilla-JS file needs no
build step). `WithMCPApp` is an explicit opt-in registered during
`InitPlugins`, so a duplicate tool name or resource uri is a hard build
error. Requires the `/mcp` server to be mounted (`WithMCP`, or the dev
auto-mount).

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
| `framework.WithMCPApp(cfg)` | Register an MCP App: a `ui://` HTML widget resource + its linking tool. |
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
- **Mixing `WithAgentReady` with granular agent-ready options** is safe in any
  order. `WithAgentReady` *merges* into whatever a granular option
  (`WithMarkdownNegotiation`, `WithLLMsTxt`, `WithAgentCard`,
  `WithAgentLinkHeaders`) already installed — the bundle wins for every field
  it explicitly sets, and a field it leaves unset preserves the granular value.
  So `WithMarkdownNegotiation()` before `WithAgentReady{Title: …}` keeps content
  negotiation on; you can equally enable it via the bundle's `ContentNegotiation`
  field. (Both still require `WithPublicLLMMD`, per the note above.)

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
