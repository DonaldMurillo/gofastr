# Agent host (MCP Apps)

The agent-host surface is what a chat host (Claude, ChatGPT) talks to when it renders your app's MCP App widget: the `/mcp` endpoint, `ui://` widget resources, and the inline widget client script.

> Note: "widget" here means a chat-host MCP App. For interactive islands inside your own app's web pages, see [Widget builder](widgets.md).

## Three settled rules

1. **One binary, one role:** the agent surface runs in-process on `/mcp` or standalone via `GOFASTR_ROLE=agent` (`framework.WithRole(framework.RoleAgent)`), sharing the same entities, tools, and auth as your web server.
2. **The chat host owns the styling:** do not ship your app's brand CSS or design-system stylesheet into a chat widget. `mcp.WidgetDocument` emits clean structure and reads the host's theme signals (`HostContext`).
3. **All behavior re-enters through `/mcp`:** every interactive action calls an MCP tool so auth, owner scoping, and rate limits apply identically — no side-channel widget APIs and no server-side widget state.

## What `RoleAgent` serves

`GOFASTR_ROLE=agent` exposes only `/mcp`, `/a2a` (when `WithA2A` is configured), `/healthz`, and `/readyz`. Entity CRUD routes, browser pages, admin, OpenAPI, and background workers stay off — ideal when exposing agent traffic behind a dedicated tunnel or load balancer (see [Horizontal scaling](scaling.md)).

## Authoring a widget

Use `mcp.WidgetDocument` so the widget client (`window.__gofastrMcpApp`) is inlined automatically. Chat hosts render widgets in a sandboxed iframe on the host's origin, so a `<script src>` link would resolve against the host and silently fail.

<!-- gofastr:compile
import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

var studioTool = func(context.Context, map[string]any) (any, error) { return "ok", nil }
-->
```go
import (
	"log"

	"github.com/DonaldMurillo/gofastr/framework"
)

doc := mcp.WidgetDocument{
	Title:  "Studio",
	Body:   `<p id="status">idle</p>`,
	Script: `var app = window.__gofastrMcpApp; app.connect({});`,
}
html, err := doc.HTML()
if err != nil {
	log.Fatal(err)
}
fwApp := framework.NewApp(
	framework.WithMCP(), // mounts /mcp; required to publish MCP App widgets
	framework.WithMCPApp(mcp.AppConfig{
		Name:        "studio",
		Description: "Open the studio widget.",
		InputSchema: map[string]any{"type": "object"},
		Handler:     studioTool,
		ResourceURI: "ui://myapp/studio.html",
		HTML:        html,
		CSP:         mcp.AppCSP{ConnectDomains: []string{"https://api.example.com"}},
	}),
)
_ = fwApp
```

`Title`, `Lang`, and `RootID` are HTML-escaped; `Body` and `Script` pass verbatim, except `HTML()` rejects `</script` or `<!--` inside `Script` (escape as `"<\/script>"` inside a raw Go backtick string).

## Following the host's theme

On `connect()` and `ui/notifications/host-context-changed`, the inlined widget client automatically applies `hostContext.theme` (`data-theme` + `color-scheme`), `hostContext.styles.variables` (CSS custom properties on `:root`), and `hostContext.styles.css.fonts`. Write your widget CSS against those standard variables with `light-dark()` fallbacks so it fits naturally into light and dark chat hosts.

## Common mistakes

- **Loading the widget client with `<script src>`:** fails silently inside the chat host's sandboxed iframe. Always use `mcp.WidgetDocument` or inline `mcp.WidgetClientJS()`.
- **Shipping your app's brand palette into a chat widget:** breaks dark-mode chat hosts. Consume the host's `HostContext` CSS variables with fallbacks.
- **Adding a bespoke HTTP endpoint "just for the widget":** bypasses `/mcp` gating, auth, and owner scoping. Route all widget actions through MCP tools.
- **Storing widget session state in server RAM:** keep every `tools/call` stateless so it works across replicas.
