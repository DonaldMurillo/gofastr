# Agent-readiness

One option mounts `/mcp`; one more serves `/llms.txt`, an agent card, per-screen `llm.md`, markdown content negotiation, and `.well-known` discovery. During development, `gofastr dev` also wires dev MCP tools and embedded docs for your coding agent automatically.

Every production discovery surface is opt-in and additive: enable the full bundle in one call, or wire individual options.

## One-call setup

Pair `uihost.WithAgentReady` with `framework.WithMCP` (and optionally `framework.WithMCPTools(mcptools.Register)` to expose the embedded framework docs):

```go
package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

func main() {
	site := app.NewApp("Acme")
	// register screens on site...

	host := uihost.New(site,
		uihost.WithPublicLLMMD(),
		uihost.WithAgentReady(uihost.AgentReadyConfig{
			BaseURL: "https://example.com",
			Title:   "Acme",
			Summary: "Acme billing console. MCP tools live at /mcp.",
			AgentCard: &uihost.AgentCardConfig{
				Name:        "Acme Agent",
				Description: "Operator agent for the Acme billing console.",
				MCPEndpoint: "/mcp",
			},
		}),
	)

	fwApp := framework.NewUIHostApp(host, framework.WithMCP())
	fwApp.Start(":8080")
}
```

That mounts `/mcp` (Streamable HTTP JSON-RPC + SSE), `/llms.txt`, `/.well-known/agent-card.json`, `/.well-known/mcp.json`, AI-bot-aware `/robots.txt`, and `Link` response headers on every HTML page.

## Discovery & markdown surfaces

### `/llms.txt` and `/llms-full.txt`

Configure `/llms.txt` via `uihost.WithAgentReady` or standalone `uihost.WithLLMsTxt`:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/uihost"
-->
```go
uihost.WithLLMsTxt("Acme", "A billing console.",
	[]uihost.LLMsTxtSection{
		{Title: "Docs", Links: []uihost.LLMsTxtLink{
			{Name: "Pages", URL: "/llm-pages.md", Notes: "every screen"},
			{Name: "API", URL: "/openapi.json"},
		}},
		{Title: "Optional", Links: []uihost.LLMsTxtLink{
			{Name: "Changelog", URL: "/changelog.md"},
		}},
	})
```

- Set `WhenToUse` and `CLI` (`uihost.CLIToolConfig`) on `AgentReadyConfig` to add `## When to use` and `## CLI` sections directly in `/llms.txt`.
- When `WithPublicLLMMD()` is active and no sections are supplied, `/llms.txt` links `/llm-pages.md` automatically (plus `## API` / `## MCP` when `OpenAPIEndpoint` / `MCPEndpoint` are set).
- Serve a single-file full corpus at `/llms-full.txt` with `uihost.WithLLMsFullTxt(markdown)` or `AgentReadyConfig.FullText` (and link it from `Sections` so agents discover it).

### Per-screen `llm.md` and Markdown content negotiation

With `uihost.WithPublicLLMMD()`, every screen serves a per-URL markdown view (`/about/llm.md`, `/products/42/llm.md`) built through the same `Load`, locale, SEO metadata, and policy chain as the HTML page. Policy-blocked routes return a metadata-free withheld doc or `404` matching the HTML status, and all page-markdown responses send `Cache-Control: private, no-store` with `Vary: Cookie`.

Concrete `/llm.md` URLs and negotiated Markdown use the page's error response when a screen fails. An ordinary `Load` error answers `404`. A panic in `Load` or an unhandled panic in `Render` answers `500`. Route resolver failures answer `404` for not-found and `500` for other errors.

#### Markdown content negotiation

Add `uihost.WithMarkdownNegotiation()` (or `AgentReadyConfig.ContentNegotiation`) alongside `WithPublicLLMMD()` so any page URL serves markdown directly when a client sends `Accept: text/markdown` with weight at least as high as HTML. Both HTML and markdown variants emit `Vary: Accept` so shared caches never mix them.

Unmatched routes automatically return RFC 9457 `application/problem+json` for JSON clients, markdown recovery links for `Accept: text/markdown` (when enabled), or your HTML 404 for browsers.
Markdown recovery includes sitemap and `llms.txt` links for the discovery routes you enable.

### A2A agent card & signed cards

Setting `AgentCard` serves `/.well-known/agent-card.json` (Agent2Agent v1.0 schema):

| Field | Purpose |
|---|---|
| `Name` *(required)* | Agent display name |
| `Description`, `Version` | Summary and version (default `"1.0.0"`) |
| `MCPEndpoint` | Advertises `/mcp` in `supportedInterfaces` + derived `mcp` skill |
| `A2AEndpoint` | Advertises `/a2a` instead of MCP (pair with `framework.WithA2A`, see [a2a.md](a2a.md)) |
| `DefaultInputModes`, `DefaultOutputModes` | MIME types (default `["text/plain"]`; per-skill overrides supported) |
| `SecuritySchemes` | OpenAPI-style auth schemes |
| `SigningKeys` | Signs the card with JWS (A2A §8.4) and publishes `/.well-known/jwks.json` |

**Signed agent cards:** pass `Ed25519` or `ECDSA` (P-256/P-384/P-521) keys in `SigningKeys`. List multiple keys during rotation. Signing requires an explicit pinned `BaseURL` (panics at boot otherwise, preventing host-header spoofing on a signed card).

### Robots, `Link` headers, and Organization JSON-LD

- **AI-bot robots:** `AgentReadyConfig.AllowAIBots` merges explicit crawler rules (`GPTBot`, `ClaudeBot`, `Google-Extended`, etc.) into `/robots.txt` without clobbering existing `Disallow` rules. Set `ContentSignals` to emit a `Content-Signal:` directive.
- **`Link` headers:** `WithAgentReady` (or `WithAgentLinkHeaders`) attaches `rel="sitemap"`, `rel="llms-txt"`, `rel="agent-card"`, `rel="service"`, `rel="service-desc"`, and `rel="alternate"` (`text/markdown`) headers on HTML responses.
- **Organization JSON-LD:** embed an `Organization` schema block in every full page `<head>` via `uihost.WithOrganization`:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/uihost"
-->
```go
uihost.WithOrganization(uihost.OrganizationConfig{
	Name:  "Acme Inc",
	URL:   "https://example.com",
	Email: "support@example.com",
	Address: uihost.PostalAddress{
		Street: "1 Main St", Locality: "Springfield",
		Region: "IL", PostalCode: "62701", Country: "US",
	},
})
```

## MCP server, dev loop & auth

`framework.WithMCP()` mounts your app's MCP server at `/mcp` (Streamable HTTP) and serves `/.well-known/mcp.json` + MCP server cards.

- **Custom tools & docs:** pass `framework.WithMCPTools(fn)` to register custom `core/mcp` tools—or `framework.WithMCPTools(mcptools.Register)` (from `framework/docs/mcptools`) to expose `framework_docs_list`, `framework_docs_get`, and `framework_docs_search`. Set server instructions with `framework.WithMCPInstructions(text)`. See [MCP server](mcp.md) for annotations, resources, and prompts.
- **Introspection & control:** `framework.WithMCPIntrospection()` adds read-only runtime tools (`app_routes`, `app_plugins`, `app_batteries`, `app_modules`, `app_config`, `app_readiness`, `app_goroutine_leaks`, `app_routines`, `app_unmounted`) plus the contract catalog tools (`contracts_list`, `contracts_explain`, `contracts_capabilities`). `framework.WithMCPControl()` adds `app_module_enable`/`disable` gated behind `admin` by default (customize with `framework.WithMCPControlGate`).
- **Dev loop auto-wiring:** under `gofastr dev` (`GOFASTR_DEV`), `/mcp`, introspection, control, `contracts_verify`/`contracts_fix`, `battery/log` tools, and entity CRUD tools activate automatically on loopback listeners (`GOFASTR_DEV_MCP=0` disables; non-loopback binds refuse dev tools unless `GOFASTR_DEV_MCP_EXPOSE=1`).

### Gating MCP tools & resources

Entity CRUD tools run through the same router as your HTTP API, so an agent passes the same session/JWT auth, RBAC, and `OwnerField`/tenant checks a user does—and `tools/list` hides tools a caller cannot invoke.

For directly registered tools, `Endpoint.MCPHandler` twins, or private `/mcp` endpoints:

- **Per-tool gate:** prefer `mcp.WithToolGate(gate)` over the older `mcp.Gated` wrapper so unauthorized callers don't even see the tool schema in `tools/list`:

```go
app.MCP.RegisterTool("orders_refund", "Refund an order", schema, refundHandler,
	mcp.WithToolGate(auth.MCPRole("support")))
```
- **Whole-endpoint gate:** close the entire MCP data surface (`tools/*`, `resources/*`, `prompts/*`, keeping `initialize` and `ping` open) in one option:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework"
-->
```go
framework.NewApp(
	framework.WithMCP(),
	framework.WithMCPGate(framework.MCPRequireUser()),
)
```

- **Custom entity endpoint twins (`Endpoint.MCPHandler`):** require an authenticated caller by default; narrow with `Endpoint.MCPGate` or open explicitly with `Endpoint.Public: true`.
- **Resources & MCP Apps:** gate sensitive resources with `mcp.WithResourceGate(gate)`. Use `framework.WithMCPApp(mcp.AppConfig{...})` to register interactive HTML widgets (`ui://` resources + CSP allowlists; see [Agent host](agent-host.md)).
- **Rich tool results:** return `mcp.ImageResult` for inline images or `mcp.ToolResult{Structured: ..., Content: ...}` for structured output alongside `mcp.WithOutputSchema`.

## Well-known & OAuth discovery endpoints

| Endpoint | Option / trigger |
|---|---|
| `/.well-known/api-catalog` | Automatic when entities are mounted (`/openapi.json`) |
| `/.well-known/mcp.json` & `/.well-known/mcp/server-card.json` | `framework.WithMCP()` |
| `/.well-known/agent-skills/index.json` | Always served; populate via `framework.WithAgentSkills(...)` |
| `/.well-known/oauth-protected-resource` | `framework.WithOAuthProtectedResource(cfg)` (RFC 9728; requires HTTPS URLs) |
| `/.well-known/oauth-authorization-server` | `framework.WithOAuthAuthorizationServer(cfg)` (RFC 8414) |
| `/auth.md` | `framework.WithAuthMD(cfg)` |
| `/.well-known/http-message-signatures-directory` | `framework.WithWebBotAuth(cfg)` ([Web Bot Auth](web-bot-auth.md)) |
| `/.well-known/openai-apps-challenge` | `framework.WithOpenAIAppsChallenge(token)` (skipped when empty) |
| `/.well-known/ucp` / `/.well-known/acp.json` | `framework.WithUCP(cfg)` / `framework.WithACP(cfg)` |

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

## Base URL & proxy trust

Discovery URLs resolve in order: `WithAgentReady{BaseURL}`, then `WithSitemap{BaseURL}`, then request scheme + `r.Host`.

Behind a TLS-terminating proxy, set `BaseURL` or list the proxy in `AppConfig.TrustedProxies` (or `uihost.WithTrustedProxies`) so `X-Forwarded-Proto` is trusted. `X-Forwarded-Host` is deliberately **not** honored from any caller (reflecting it into `Link` headers or discovery docs enables cache poisoning); always set `BaseURL` when a proxy rewrites the host.

## Granular options

| Option | Serves |
|---|---|
| `uihost.WithAgentReady(cfg)` | Bundle: `/llms.txt`, agent card, AI-bot robots, `Link` headers |
| `uihost.WithLLMsTxt(...)` / `WithLLMsFullTxt(...)` | `/llms.txt` or `/llms-full.txt` standalone |
| `uihost.WithAgentCard(cfg)` | `/.well-known/agent-card.json` (+ `/.well-known/jwks.json` if signed) |
| `uihost.WithAgentLinkHeaders()` | `Link:` headers on HTML pages |
| `uihost.WithMarkdownNegotiation()` | `Accept: text/markdown` negotiation (needs `WithPublicLLMMD`) |
| `uihost.WithOrganization(cfg)` | Organization JSON-LD in `<head>` |
| `framework.WithMCP()` | `/mcp` + MCP server card & manifest |
| `framework.WithMCPIntrospection()` | Read-only `app_*` and `contracts_*` tools |
| `framework.WithMCPTools(register)` | Custom MCP tools (pass `mcptools.Register` for `framework_docs_*`) |
| `framework.WithMCPControl()` | Admin-gated `app_module_enable` / `app_module_disable` |
| `framework.WithMCPGate(gate)` | Endpoint-wide MCP auth gate |
| `framework.WithMCPApp(cfg)` | MCP App `ui://` widget resource + tool |
| `framework.WithA2A(cfg)` | A2A v1.0 task exchange at `/a2a` ([a2a.md](a2a.md)) |

## What this does not cover

- **A2A task execution requires `framework.WithA2A`.** The agent card alone advertises `/mcp`; add `WithA2A` if you want full A2A task streaming and push notifications.
- **DNS-AID** is a DNS TXT record managed at your registrar, not by your Go binary.
- **x402 / MPP payments** require an external payment backend and middleware beyond discovery docs.

## Common mistakes

- **Advertising `/mcp` on the agent card without calling `framework.WithMCP()`.** `WithAgentReady` only publishes discovery metadata; call `framework.WithMCP()` to actually mount `/mcp`.
- **Enabling `WithMarkdownNegotiation` without `WithPublicLLMMD`.** Negotiated markdown relies on the per-screen renderer and falls back to HTML without `WithPublicLLMMD`.
- **Calling `WithMCP()` and manually mounting `/mcp`.** Route conflict panics at startup—pick one.
- **Serving `/llms-full.txt` without linking it in `/llms.txt`.** Agents read `/llms.txt` first; add an entry pointing to `/llms-full.txt`.
- **Signing the agent card without setting `BaseURL`.** Signing panics at boot unless `BaseURL` is pinned.
- **Using `mcp.Gated` instead of `mcp.WithToolGate`.** `mcp.Gated` only blocks `tools/call` while leaving the tool's schema visible in `tools/list`; `mcp.WithToolGate` hides it from unauthorized callers too.
