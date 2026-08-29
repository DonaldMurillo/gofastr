# Agent-readiness

AI agents (and scanners like [isitagentready.com](https://isitagentready.com/))
look for a small set of well-known discovery artifacts before they can use a
site: a curated `/llms.txt`, an A2A agent card, sitemap + robots, `Link`
response headers pointing at all of it, and markdown content negotiation.
GoFastr already ships the *plumbing*: MCP tools, an OpenAPI spec, per-screen
markdown docs, sitemap, robots. Getting agent-ready mostly means adding
the *discovery* layer that makes those capabilities findable.

Every piece below is **opt-in and additive**: existing robots/sitemap/openapi/
llm.md behavior is unchanged. Turn the sane defaults on in one call, or wire
each piece granularly.

## One-call bundle

`uihost.WithAgentReady` + `framework.WithMCP` is the full agent-ready shape:

```go
package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func main() {
	site := app.NewApp("Acme")
	// …register your screens on site…

	host := uihost.New(site, uihost.WithAgentReady(uihost.AgentReadyConfig{
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

### `/llms.txt`: curated markdown index  (llmstxt.org)

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/uihost"
-->
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
the app's `/llm-pages.md` index, which itself enumerates every screen and its
per-screen `/llm.md` doc.

The bundle adds three content pieces on top of the file-list sections:

- **`WhenToUse`** renders a `## When to use` section — one or two plain
  sentences naming the best-fit use cases. This is the phrasing scanners
  (and agents deciding whether your service fits their task) read first.
- **`CLI`** renders a `## CLI` section with the install command in a fenced
  code block plus a docs link, so an agent reaches for the scriptable path
  instead of scraping the UI:

  ```go
  uihost.WithAgentReady(uihost.AgentReadyConfig{
  	Title: "Acme",
  	WhenToUse: "Reach for Acme when you need invoices issued or " +
  		"voided programmatically.",
  	CLI: &uihost.CLIToolConfig{
  		Name: "acme", Install: "npm install -g @acme/cli", Docs: "/docs/cli",
  	},
  })
  ```

- **Auto-links**: when `OpenAPIEndpoint` is set the index gains an `## API`
  section linking it, and when the agent card's `MCPEndpoint` is set it gains
  an `## MCP` section linking the endpoint and `/.well-known/mcp.json`.
  Sections you wrote yourself win: if your `Sections` already link a path,
  the auto-section for it is not appended.

Setting `WhenToUse` or `CLI` alone turns `/llms.txt` on — both only render
inside the index, so configuring one is itself the opt-in.

### `/llms-full.txt`: the full-corpus tier

The llmstxt.org convention has two tiers: `/llms.txt` is the small index
an agent fetches first; `/llms-full.txt` is the whole docs corpus in one
markdown file, for agents that want everything in a single request
instead of following links. Serve it by passing the concatenated
markdown:

```go
uihost.WithLLMsFullTxt(fullCorpusMarkdown)
// or, via the bundle:
uihost.WithAgentReady(uihost.AgentReadyConfig{
	Title:    "Acme",
	FullText: fullCorpusMarkdown,
})
```

The content is served verbatim as `text/plain`. Nothing links it
automatically. Add a `Sections` entry pointing at `/llms-full.txt` so
agents reading the index can find it. The docs site in `examples/site`
does exactly this: its `/llms.txt` indexes every embedded framework
doc as a raw markdown URL, and its `/llms-full.txt` is the whole
corpus concatenated.

### A2A agent card  (Agent2Agent v1.0)

`/.well-known/agent-card.json` describes the agent's identity, service
endpoint, capabilities, and skills, conforming to the A2A v1.0 AgentCard
(camelCase JSON keys per ADR-001; `supportedInterfaces` and `skills` are
REQUIRED and always present). The service endpoint lives in
`supportedInterfaces[].url`. There is no top-level `url` in v1.0. When
`MCPEndpoint` is set, that endpoint is advertised as the JSON-RPC
interface (it genuinely speaks JSON-RPC: `initialize` and `tools/list`
work), and a derived `mcp` skill points agents at it.

| `AgentCardConfig` field | Purpose |
|---|---|
| `Name` *(required)* | Human-readable agent name. |
| `Description` | Short summary. |
| `Version` | Software version; defaults `1.0.0`. |
| `URL` | Fallback for the `supportedInterfaces[].url` when `MCPEndpoint` is unset; defaults to the resolved base URL. |
| `MCPEndpoint` | e.g. `"/mcp"`, advertised as `supportedInterfaces[].url` (baseURL + endpoint), plus a derived `mcp` skill + a `Link: rel="service"` header. |
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
main group, so they inherit the host's `Allow`/`Disallow` rules. A
standalone `Allow: /` group would shadow path-specific exclusions, since
RFC 9309 applies only a crawler's most-specific group. When denied,
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

While negotiation is enabled, **both variants of a page URL — the markdown
response *and* the HTML one — carry `Vary: Accept`**, so a shared cache keys
on the Accept header and never replays the markdown body to a browser (or
the HTML body to an agent). Pages on hosts without the opt-in carry no
`Vary`, since the URL has a single representation.

### Agent-friendly 404s  (RFC 9457 + markdown recovery)

A request that matches no route gets a 404 shaped like the client asked:

- `Accept: application/json` (or `application/problem+json`) → an
  `application/problem+json` body per RFC 9457: `{"type":"about:blank",
  "title":"Not Found","status":404,"detail":"…"}`. This arm is always on —
  a machine-readable error shape is error-format correctness, not an agent
  feature. A browser-style `Accept` (`text/html,…,*/*`) still gets the HTML
  404, custom `WithNotFoundScreen` included.
- `Accept: text/markdown`, when markdown negotiation is enabled → a
  `text/markdown` body with recovery links (`[Home](/)`,
  `[Site map](/sitemap.xml)`, `[llms.txt](/llms.txt)`).

Every 404 arm carries `Vary: Accept`. Neither machine arm reflects the
requested path back into the body — the markdown arm deliberately says
nothing about the URL, and the problem document omits it, so a hostile path
can never land unescaped in a machine-readable error.

### Organization JSON-LD  (`WithOrganization`)

Scanners (and agents doing due diligence) look for an Organization schema
with a `contactPoint` and a `PostalAddress` to verify the business behind a
site is real and reachable. `uihost.WithOrganization` declares it once; the
host embeds it as an `<script type="application/ld+json">` block in every
full page head:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/uihost"
-->
```go
uihost.WithOrganization(uihost.OrganizationConfig{
	Name:  "Acme Inc",
	URL:   "https://example.com",
	Email: "support@example.com", // and/or Phone
	Address: uihost.PostalAddress{
		Street: "1 Main St", Locality: "Springfield",
		Region: "IL", PostalCode: "62701", Country: "US",
	},
})
```

`Logo` and `SameAs` (GitHub/LinkedIn profile URLs) are optional. The JSON is
built with `encoding/json`, so host-supplied strings are escaped and cannot
break out of the data block; unset pieces (`contactPoint`, `address`) are
omitted rather than emitted empty, and `ContactType` defaults to
`"customer support"`.

### Per-screen `llm.md` carries the screen's SEO

The per-screen `/llm.md` document (and the negotiated-markdown response)
opens with a YAML front-matter block mirroring the same screen's HTML
`<head>` metadata: `title`, `description`, `canonical`, `robots`,
`og_title` / `og_description` / `og_image`, `twitter_card` /
`twitter_title`, `hreflang` (a list), and `schema_types` (the JSON-LD
`@type` names). The values are resolved from the same
`ScreenSEO` bundle + per-concern interfaces (`ScreenCanonical`,
`ScreenRobots`, `ScreenHreflangs`, `ScreenSchema`) the head renders from,
so a crawler and an LLM see one consistent metadata set per route.
Screens with no SEO declarations get no front-matter, and the markdown is
unchanged. See [SEO](/docs/seo) for the per-screen interfaces.

### Dynamic routes get per-URL docs

A dynamic route's concrete URLs serve real per-page docs, not one shared
pattern doc: `GET /products/42/llm.md` (or `/docs/getting-started/llm.md`
on a catch-all route) builds the same per-request instance the page render
uses, `SetParams` → DI → `Load`, so the markdown carries that page's
loaded title and rendered content. The static exporter does the same for
every URL a screen's `StaticPaths` enumerates, and SPA partial responses
carry the post-`Load` title in `X-Gofastr-Title`, so in-app navigation to
a dynamic page updates the browser title correctly. All of it sits behind
the same `WithPublicLLMMD` opt-in as the static per-screen handlers; a
`Load` failure degrades to the pattern-level doc rather than erroring.

Every markdown surface evaluates the screen's policy chain with the live
request: a non-Allow decision serves a metadata-free "withheld" doc (route
path and type only: no title, description, SEO front matter, or content),
and the `/llm-pages.md` index lists policy-gated screens path-only. An
authenticated agent whose request passes the policy sees the full docs.

### MCP auto-mount  (`framework.WithMCP`)

`framework.WithMCP()` exposes `app.MCP` at `/mcp` over Streamable HTTP (POST
JSON-RPC + GET Server-Sent Events), replacing the manual
`fwApp.Router().Handle("POST", "/mcp", fwApp.MCP)`. Combined with
`WithMCPIntrospection()`, the eleven tools that read the running app's state
are reachable at the canonical endpoint the agent card advertises:
`app_routes`, `app_plugins`, `app_batteries`, `app_modules`, `app_config`,
`app_readiness`, `app_goroutine_leaks`, `app_routines`, `framework_docs_list`,
`framework_docs_get`,
`framework_docs_search`. Alongside them sits the contract catalog
(`contracts_list`, `contracts_explain`, `contracts_capabilities`), which
describes what the framework requires of the app's own code. Under
`gofastr dev` the catalog gains a working half: `contracts_verify` runs
the analyzers over the app's source and returns structured findings, and
`contracts_fix` applies one rule's autofixes. Neither is registered
outside the dev loop, since both touch local source files.
Calling `WithMCP` **and** manually mounting `/mcp`
panics with a route conflict. Pick one. Blueprint-generated apps ship with
both options wired.

`framework.WithMCPControl()` adds the mutating counterpart:
`app_module_enable` / `app_module_disable` toggle registered modules on the
running app (persisted through the module store, dependency-checked). Keep
it off any `/mcp` reachable by untrusted callers.

When you opt in explicitly, both control tools require an
**authenticated caller**: they run behind an `mcp.WithToolGate`
precondition that refuses a request with no identity on its context. Make
sure the app's session/JWT middleware runs on the `/mcp` route, or every
call comes back asking for a caller. The gate asks only for an identity,
because the framework layer cannot know your role vocabulary. Pass
`auth.MCPRole("admin")` when you want more.

A gated tool is also **hidden from `tools/list`** for callers who cannot
invoke it. That matters more than it sounds: `tools/list` used to run with
no gate at all, so an anonymous POST came back with every tool's
`inputSchema`, and for entity CRUD tools those schemas are built from live
entity definitions, naming every entity and every non-`Hidden` field with
its type and enum set. The call refused; the schema was already out.

Prefer `mcp.WithToolGate(gate)` as a `RegisterTool` option over the older
`mcp.Gated(gate, handler)` wrapper. `Gated` wraps the handler, so it only
ever reached `tools/call`. The listing never consulted it.

```go
app.MCP.RegisterTool("orders_refund", "…", schema, refundHandler,
    mcp.WithToolGate(auth.MCPRole("support")))
```

When the whole endpoint is private, close it in one place instead:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
-->
```go
framework.NewApp(
    framework.WithMCP(),
    framework.WithMCPGate(framework.MCPRequireUser()),
)
```

`WithMCPGate` covers the whole data surface: `tools/list`, `tools/call`,
`resources/list`, `resources/read`, `resources/templates/list`,
`prompts/list` and `prompts/get`. The `initialize` handshake and `ping` stay open by
design: they carry only the protocol version, capability booleans and the
server name, and a client that cannot handshake cannot present credentials.

The `gofastr dev` loop is exempt: it turns these tools on with no auth
configured at all, so a gate would only lock the dev loop out of its own
app. Its exposure is bounded on the other axis instead: dev **refuses
to register the control tools when the listener is not loopback**. Bind
to `localhost`, or set `GOFASTR_DEV_MCP_EXPOSE=1` to accept the risk.
The transport's loopback `Host` pin is a browser control (it stops DNS
rebinding); it does nothing against a direct TCP client, which sets
`Host` freely. That is why the bind matters too.

Auth splits by tool kind: entity CRUD tools re-dispatch
through the router, so session/JWT auth, owner scoping, and RBAC apply
exactly as they do over HTTP (the caller's Cookie/Authorization from the `/mcp`
request carries through). Directly registered tools, meaning custom
`app.MCP.RegisterTool` handlers and `Endpoint.MCPHandler` twins, run
without route middleware, so they carry their own gate.

**`Endpoint.MCPHandler` twins default to requiring an authenticated
caller.** An `Endpoint` has two front doors for one operation: `Handler`
inherits the route's middleware chain, `MCPHandler` does not. An endpoint
behind `auth.RequireRole("editor")` was therefore role-checked over HTTP
and ungated over MCP. Declare something stricter with
`Endpoint.MCPGate`, or opt out with `Endpoint.MCPPublic: true` for an
endpoint that really is anonymous over HTTP too:

```go
entity.Endpoint{
    Method: "POST", Path: "{id}/publish", MCP: true,
    Handler:    publishHTTP,
    MCPHandler: publishTool,
    MCPGate:    auth.MCPRole("editor"),   // else: any authenticated caller
}
```

For your own `RegisterTool` calls, gate them per-caller with
`mcp.WithToolGate` + battery/auth's `auth.MCPUser()` / `auth.MCPRole(...)`
(see [plugins](plugins.md)).

Process modules (issue `#37`) add a third tool kind alongside the entity
CRUD tools and directly-registered handlers. Each tool a process module
exposes is registered under a reserved `module.` prefix,
`module.<name>.<tool>`, so two modules cannot collide and every call is
attributable to its owning module. A disabled module's tools are omitted
from `tools/list` and refused by `tools/call` (the composite call gate);
an enabled-but-down module's tools stay listed but return a retryable
temp-unavailable error while the child is not Ready. A tool call forwards
to the child through the same capability broker as the module's HTTP
routes. The agent's authority is delegated identically, and there is no
separate tool-permission vocabulary. See [process modules](process-modules.md).

**The dev loop implies all of it.** Under `gofastr dev` (`GOFASTR_DEV`),
`framework.NewApp` auto-enables the mount, introspection, and control;
battery/log auto-registers its `log_recent` / `log_filter` /
`log_metrics` / `log_set_level` debug tools; and every CRUD-enabled
entity serves its `{entity}_list/get/create/update/delete` data tools
without per-entity `mcp: true`, with zero options: the local dev loop
is livereload for agents. Opt out with `GOFASTR_DEV_MCP=0` (mirrors
`GOFASTR_DEV_LIVERELOAD=0`); a production `GOFASTR_ENV` always wins.
A dev-implied mount yields to a hand-wired `/mcp` route instead of
panicking, so older scaffolds keep working under `gofastr dev`.

### Rich tool results, resources, and MCP Apps

A tool handler returns `any`. By default a plain value is JSON-marshaled
into a single `{type:"text"}` block (unchanged). To emit richer content,
return one of `core/mcp`'s result types:

```go
// An image block: every MCP client renders it inline (no token bomb from
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
resources are **not** covered by the tool call gate: `mcp.Gated` /
`auth.MCPUser` gate tool handlers, not `resources/read`. Public content (an
MCP App's widget HTML) needs no gating; to serve sensitive or per-caller
data, add `mcp.WithResourceGate(gate)` (the resource-side analogue of
`mcp.Gated`, where `auth.MCPUser()` / `auth.MCPRole(...)` work as gates),
which runs before the contents func on every read.

**MCP Apps.** The [MCP Apps extension](https://modelcontextprotocol.io/extensions/apps/overview)
lets a tool declare an interactive HTML widget the host renders in a
sandboxed iframe. `framework.WithMCPApp` wires both halves in one call:
the `ui://` resource carrying the HTML, and the tool whose `_meta` links
to it (with the ChatGPT Apps SDK `openai/outputTemplate` compat alias).

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

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
-->
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
| MCP Manifest | `/.well-known/mcp.json` — flat `endpoint`/`transport` fields AND a nested `mcpServers` map, both naming `/mcp` with `streamable-http` | when `WithMCP` exposes `/mcp` |
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

All 11 scored isitagentready checks are covered: robots.txt, Sitemap,
Link headers, Markdown negotiation, AI bot rules, Content Signals, API
Catalog, OAuth Protected Resource, MCP Server Card, Agent Skills Index,
and OAuth Authorization Server (6 always-on via the bundle; the rest
opt-in / conditional). The production scanner also lists: A2A card
(covered: `/.well-known/agent-card.json`), Auth.md (`WithAuthMD`), Web
Bot Auth (`WithWebBotAuth`: the site publishes a JWKS at
`/.well-known/http-message-signatures-directory` so it can sign its own
outbound requests), UCP (`WithUCP` → `/.well-known/ucp`), and ACP
(`WithACP` → `/.well-known/acp.json`). Not buildable as served routes:
DNS-AID (DNS SVCB/HTTPS + DNSSEC), x402 (HTTP 402 payment middleware),
MPP (payment execution + an `x-payment-info` OpenAPI extension needing a
payment backend), WebMCP (client-side browser API), ap2 (server-only).

## Base URL resolution

All absolute discovery URLs (agent card `url`, `Link` header targets) use one
canonical origin, resolved in this order: `WithAgentReady{BaseURL}`, then
`WithSitemap{BaseURL}`, then the per-request scheme + the request's own `Host`.
Set one origin and every artifact stays consistent. Behind a proxy,
`X-Forwarded-Proto` is honored for the scheme, but `X-Forwarded-Host` is
deliberately **not**: it is client-settable and reflecting it into the
`Link: rel="service"` header would be a cache-poisoning primitive, so the
addressed `Host` is used instead. Set `BaseURL` explicitly when your proxy
rewrites the host.

## Granular options

| Option | Serves |
|---|---|
| `uihost.WithAgentReady(cfg)` | Bundle: llms.txt + card + AI-bot robots + Link headers (incl. OpenAPI `service-desc` when `cfg.OpenAPIEndpoint` is set, e.g. `"/openapi.json"`). |
| `uihost.WithLLMsTxt(title, summary, sections)` | `/llms.txt` only. |
| `uihost.WithLLMsFullTxt(content)` | `/llms-full.txt` only (full-corpus tier, served verbatim). |
| `uihost.WithAgentCard(cfg)` | `/.well-known/agent-card.json` + `agent.json` alias. |
| `uihost.WithAgentLinkHeaders()` | `Link:` headers on HTML only. |
| `uihost.WithMarkdownNegotiation()` | `Accept: text/markdown` → markdown. |
| `uihost.WithOrganization(cfg)` | Organization JSON-LD (`contactPoint` + `PostalAddress`) in every full page head. |
| `framework.WithMCP()` | Auto-mount `/mcp` (Streamable HTTP) + discovery well-knowns (server card, `/.well-known/mcp.json` manifest). |
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
  does *not* mount MCP for you. Call `framework.WithMCP()` alongside it.
- **Advertising markdown negotiation without `WithPublicLLMMD`.**
  `WithMarkdownNegotiation` renders via the per-screen LLM doc, which only
  exists when markdown rendering is public. Without it, the negotiated
  response falls through to HTML.
- **Hand-writing per-route `/llm.md` links in `/llms.txt`.** Non-screen routes
  (`/api/*`, `/healthz`, `/.well-known/*`) have no markdown. Link the
  `/llm-pages.md` index instead (the default does this).
- **Calling `WithMCP` and also mounting `/mcp` by hand.** Route conflict →
  panic at startup. Use one.
- **Serving `/llms-full.txt` without linking it from `/llms.txt`.** Agents
  start at the index; a full-corpus file nothing points to won't be found.
  Add a `Sections` entry with URL `/llms-full.txt`.
- **Mixing `WithAgentReady` with granular agent-ready options** is safe in any
  order. `WithAgentReady` *merges* into whatever a granular option
  (`WithMarkdownNegotiation`, `WithLLMsTxt`, `WithAgentCard`,
  `WithAgentLinkHeaders`) already installed: the bundle wins for every field
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
  framework code. Add them at your registrar/host.
- **No inbound Web Bot Auth verification.** `WithWebBotAuth` publishes the
  site's signing JWKS (so it can sign its own outbound requests); verifying
  RFC 9421 signatures on *inbound* requests is host middleware, not a served
  artifact.
- **No x402 / MPP payment.** These need real payment middleware (HTTP 402 +
  payment requirements) or a payment backend; the framework serves discovery
  docs (UCP/ACP) but not payment execution.
