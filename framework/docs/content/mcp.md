# MCP server

`core/mcp` is GoFastr's Model Context Protocol (JSON-RPC 2.0) server. It manages tools, resources, resource templates, prompts, per-caller authorization gates, and both stdio and Streamable HTTP/SSE transports.

A `framework.App` creates one at `app.MCP` and mounts it at `/mcp` via `framework.WithMCP()`. Framework options (`WithMCPIntrospection`, `WithMCPControl`, `WithMCPApp`, `WithMCPInstructions`, `WithMCPTools(mcptools.Register)`) are covered in [Agent-readiness](agent-ready.md); this page is the `core/mcp` reference.

Used standalone without `framework.App`:

<!-- gofastr:compile
import (
	"context"
	"os"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)
var ctx context.Context
-->
```go
srv := mcp.NewServer()
srv.SetServerInfo("acme-agent", "1.2.3") // defaults: "GoFastr MCP", "1.0.0"
srv.ServeStdio(ctx, os.Stdin, os.Stdout)  // or mount srv as an http.Handler
```

Call `SetServerName(name)` to override only the name, `SetInstructions(text)` (or `framework.WithMCPInstructions(text)`) to populate `initialize.instructions`, and `ServerInfo()` / `Instructions()` to read them back.

## Supported JSON-RPC methods & protocol versions

`Server.HandleRequest` handles `initialize`, `ping`, `tools/list`, `tools/call`, `resources/list`, `resources/read`, `resources/templates/list`, `resources/subscribe`, `resources/unsubscribe`, `prompts/list`, and `prompts/get`.

- **Protocol negotiation:** Supports `2025-06-18` (`mcp.LatestProtocolVersion`) and `2025-03-26` (`mcp.SupportedProtocolVersions()`). Unknown or omitted client versions receive `2025-06-18`. JSON-RPC batches return `-32600` (`ErrInvalidRequest`).
- **Exported error codes:** `ErrParse` (-32700), `ErrInvalidRequest` (-32600), `ErrMethodNotFound` (-32601), `ErrInvalidParams` (-32602), `ErrInternalError` (-32603), carried on `*RPCError`.
- **Capabilities:** `initialize` advertises `tools` always, `resources` (with `subscribe: true`) once any resource or template is registered, and `prompts` once any prompt is registered—all with `listChanged: true`.

## Tools

<!-- gofastr:compile
import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)
-->
```go
srv := mcp.NewServer()
srv.RegisterTool(
	"orders_refund",
	"Refund an order by id",
	map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":     map[string]any{"type": "string"},
			"reason": map[string]any{"type": "string"},
		},
		"required": []string{"id"},
	},
	func(ctx context.Context, params map[string]any) (any, error) {
		id, _ := params["id"].(string)
		return mcp.ToolResult{
			Structured: map[string]any{"id": id, "refunded": true},
			Content:    []mcp.Content{mcp.TextContent("refund issued for " + id)},
		}, nil
	},
	mcp.WithOutputSchema(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":       map[string]any{"type": "string"},
			"refunded": map[string]any{"type": "boolean"},
		},
	}),
)
```

- **Schema enforcement:** `tools/call` and `CallTool` validate `arguments` against `inputSchema` (`type`, `required`, `enum`, `items`, `additionalProperties`) before calling your handler; opt out with `WithLaxArgs()`. Omitted `arguments` reach the handler as a nil map; `"arguments": null`, or any value that is not an object, answers `-32602` before the tool is looked up. Integers past ±2^53 arrive as `json.Number` so IDs aren't rounded.
- **Return values:** `mcp.ToolResult` (explicit content blocks and/or `Structured`, plus `IsError: true` for model-visible tool errors), `mcp.ImageResult`, `mcp.Content` / `[]mcp.Content` (`TextContent`, `ImageContent`, `AudioContent`, `ResourceContent`), `string`, or any JSON-marshalable value.
- **Errors:** `*RPCError` passes through verbatim; 4xx `*handler.Error` becomes `-32602` with `code`, `reason`, and `fields` in `data`; plain errors, 5xx errors, and panics are logged server-side and answered with a generic `"internal tool error"`.
- **Options:** `WithOutputSchema(schema)`, `WithToolMeta(meta)`, `WithToolGate(gate)`, `WithLaxArgs()`, `WithAnnotations(a)`.
- **Entity CRUD tools:** Entity list tools advertise `Pagination.MaxListLimit` (capped at 1000, default 100); entity create/update schemas omit server-controlled owner and tenant columns.
- **In-process helpers:** `CallTool(ctx, name, params)` runs per-tool gates only; `CallToolFor(ctx, name, params)` also runs the server-wide gate (`SetGate`); `ListToolsFor(ctx)` applies all gates (never expose raw `ListTools()` to remote callers); `HasTool(name)` checks registration; `UnregisterTool(name)` withdraws a tool.

### Tool annotations

`mcp.Annotations` supplies behavioral hints clients (and directory reviews like ChatGPT's) use to decide whether to prompt before calling a tool. Fields are `*bool` (`mcp.Bool(true)` / `mcp.Bool(false)`) so unset hints are omitted rather than sent as `false`:

<!-- gofastr:compile
import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)
var srv *mcp.Server
var handler func(context.Context, map[string]any) (any, error)
-->
```go
srv.RegisterTool("generate_barcode", "Generate a barcode image",
	map[string]any{"type": "object"}, handler,
	mcp.WithAnnotations(mcp.Annotations{
		Title:           "Generate a barcode",
		ReadOnlyHint:    mcp.Bool(true),
		DestructiveHint: mcp.Bool(false),
		IdempotentHint:  mcp.Bool(true),
		OpenWorldHint:   mcp.Bool(false),
	}))
```

- `SetAnnotationWarnings(true)` (enabled automatically in `gofastr dev`) warns once per non-dev-implied tool missing `readOnlyHint`, `destructiveHint`, or `openWorldHint`.
- Generated entity item tools (`_get`, `_update`, `_delete`, transitions) take the record key as `id`. A composite key takes one argument per key column, named by its wire name, and so does a single key column other than `id` when another field is called `id`, so `_update` can still write that field.
- Generated entity CRUD tools set annotations automatically: `_list`/`_get` are read-only + idempotent; `_create` is non-destructive; `_update`/`_delete` are destructive + idempotent; state transitions are destructive + non-idempotent. Set custom endpoint hints via `entity.Endpoint.MCPAnnotations` and app hints via `AppConfig.Annotations`.

### Request `_meta`

Read client-supplied `tools/call` `_meta` (such as `openai/locale` or `openai/subject`, capped at 64 KiB) with `mcp.RequestMeta(ctx)` (or inject in tests via `mcp.ContextWithRequestMeta`):

<!-- gofastr:compile
import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)
-->
```go
handler := func(ctx context.Context, params map[string]any) (any, error) {
	locale := "en"
	if meta := mcp.RequestMeta(ctx); meta != nil {
		if l, ok := meta["openai/locale"].(string); ok {
			locale = l
		}
	}
	return "reply in " + locale, nil
}
_ = handler
```

**Treat `_meta` as unauthenticated input.** Use it for locale or rate-limit sharding, never for identity or authorization—read authenticated users from your auth middleware.

## Resources, templates & prompts

### Resources

<!-- gofastr:compile
import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)
-->
```go
srv := mcp.NewServer()
srv.RegisterResource(
	"docs://api/quickstart",
	"API quickstart",
	"text/markdown",
	func(ctx context.Context) (mcp.ResourceContents, error) {
		return mcp.ResourceContents{Text: "# Quickstart\n\nPOST /api/orders …"}, nil
	},
	mcp.WithResourceDescription("The five-minute tour"),
)
```

Options: `WithResourceDescription`, `WithResourceMeta`, and `WithResourceGate` (hides from `resources/list`, blocks `resources/read`, and suppresses `notifications/resources/updated`):

<!-- gofastr:compile
import (
	"context"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/mcp"
)
-->
```go
srv := mcp.NewServer()
srv.RegisterResource(
	"app://invoices.csv", "Invoices export", "text/csv",
	func(ctx context.Context) (mcp.ResourceContents, error) {
		return mcp.ResourceContents{Text: "id,amount\n1,49.00"}, nil
	},
	mcp.WithResourceGate(auth.MCPRole("accounting")),
)
```

### Resource templates

Advertise RFC 6570 URI patterns (client expands them; concrete URIs must still be registered for `resources/read`):

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/mcp"
-->
```go
srv := mcp.NewServer()
srv.RegisterResourceTemplate(
	"help://docs/{topic}",
	"Framework doc by topic",
	"text/markdown",
	mcp.WithResourceTemplateDescription("Expand {topic} to a doc slug, then resources/read it"),
)
```

Options: `WithResourceTemplateDescription`, `WithResourceTemplateMeta`, `WithResourceTemplateGate`.

### Prompts

<!-- gofastr:compile
import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)
-->
```go
srv := mcp.NewServer()
srv.RegisterPrompt(
	"release_notes",
	func(ctx context.Context, args map[string]string) ([]mcp.PromptMessage, error) {
		return []mcp.PromptMessage{{
			Role:    "user",
			Content: mcp.TextContent("Draft release notes for version " + args["version"] + "."),
		}}, nil
	},
	mcp.WithPromptDescription("Draft release notes for a version"),
	mcp.WithPromptArguments(
		mcp.PromptArgument{Name: "version", Description: "e.g. 1.4.2", Required: true},
		mcp.PromptArgument{Name: "tone", Description: "blog post or changelog entry"},
	),
)
```

Required prompt arguments (and string types) are validated before your handler runs. Options: `WithPromptDescription`, `WithPromptArguments`, `WithPromptGate`.

## Gating

A gate is `func(ctx context.Context) error` (nil gates panic at registration). Gated items are hidden from list methods, excluded from pagination offsets, and answered identically to "not found" on direct access (wrap an error in `mcp.ShowRefusal(err)` to surface a custom message to the caller):

<!-- gofastr:compile
import (
	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/mcp"
)

var srv *mcp.Server
var inputSchema map[string]any
var shipHandler mcp.ToolHandler
var postmortemHandler mcp.PromptHandler
-->
```go
srv.RegisterTool("orders_ship", "Ship a pending order", inputSchema, shipHandler,
	mcp.WithToolGate(auth.MCPRole("support", "admin")))

srv.RegisterPrompt("incident_postmortem", postmortemHandler,
	mcp.WithPromptGate(auth.MCPRole("oncall")))

srv.RegisterResourceTemplate("help://docs/{topic}", "Internal doc by topic", "text/markdown",
	mcp.WithResourceTemplateGate(auth.MCPUser()))

// Whole endpoint private (initialize and ping stay open for handshake):
srv.SetGate(auth.MCPUser())
```

Prefer `WithToolGate` over `mcp.Gated(gate, handler)`—`Gated` only runs at `tools/call` and leaves the tool visible in `tools/list`. When wrapping an already-gated `ToolHandler`, use `mcp.Decorate(h, wrap)` so gates stay outermost before argument validation.

## Server-initiated notifications & pagination

### SSE notifications

`ServeSSE` streams `notifications/tools/list_changed`, `notifications/resources/list_changed`, `notifications/prompts/list_changed`, and `notifications/resources/updated` over `GET /mcp` (`Accept: text/event-stream`):

<!-- gofastr:compile
import (
	"github.com/DonaldMurillo/gofastr/core/mcp"
)

var srv *mcp.Server
-->
```go
srv.NotifyResourceUpdated("docs://api/quickstart")
```

- Call `NotifyToolsListChanged()`, `NotifyPromptsListChanged()`, or `NotifyResourcesListChanged()` manually when caller permissions change without a registration.
- Notifications respect both `SetGate` and per-item gates, re-checking caller principals every delivery and every 15s keep-alive (`handler.RecheckPrincipal`).
- Bounds: 16 buffered notifications per stream (slow clients are dropped), 16 concurrent streams per caller (`SetSSESeatCap`, `SetSSESeatOverflow(mcp.SeatOverflowEvictOldest)`), 1024 total streams (`SetSSEStreamLimit`), and per-process subscription caps. Notifications are process-local (and no-ops on stdio).

### Pagination

List methods return up to 100 items per page with an opaque, per-process HMAC-signed `nextCursor` (omitted when all results fit on one page):

<!-- gofastr:compile
import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)
-->
```go
cursor := ""
for {
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/list",
		"params": map[string]any{"cursor": cursor},
	})
	resp, err := http.Post("http://localhost:8080/mcp", "application/json", bytes.NewReader(body))
	if err != nil {
		return
	}
	var out struct {
		Result struct {
			Tools      []map[string]any `json:"tools"`
			NextCursor string           `json:"nextCursor"`
		} `json:"result"`
	}
	json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	for _, t := range out.Result.Tools {
		fmt.Println(t["name"])
	}
	if out.Result.NextCursor == "" {
		break
	}
	cursor = out.Result.NextCursor
}
```

Cursors are scoped to their list method and process signing key; restarts or non-sticky load balancers return `"invalid cursor"` so the client restarts from page 1.

## Transports & origin security

- **`ServeHTTP`:** `POST` `application/json` (1 MiB cap, strict JSON keys). Notifications (no `id`) return `202 Accepted` with no body.
- **`ServeSSE(path)`:** `POST` JSON-RPC + `GET` SSE notification stream (checks `SetGate` and origin/Host on both).
- **`ServeStdio(ctx, in, out)`:** Line-delimited JSON-RPC (1 MiB line cap, panic recovery, silent on notifications).
- **Origin & DNS-rebinding guards:** Browser `Origin` must be same-origin unless allowed via `SetAllowedOrigins`. Pin local hosts with `SetAllowedHosts` or `SetRequireLoopbackHost(true)` (automatic in `gofastr dev`).
- **Context helpers:** `WithRequest` / `RequestFromContext` expose the inbound `*http.Request` (used by CRUD tools to forward auth headers and `Host`); `StreamSSE(w, event, data)` writes a single SSE event safely.

## MCP Apps & widget client

`RegisterApp(mcp.AppConfig{...})` registers an interactive HTML `ui://` resource and its launching tool together. Configure iframe CSP allowlists via `mcp.AppCSP` (`ConnectDomains`, `ResourceDomains`, `FrameDomains`, `BaseURIDomains`).

Because chat hosts render widgets inside sandboxed iframes on their own origin, inline the widget client script using `mcp.WidgetDocument` (recommended—see [agent-host.md](agent-host.md)) or `mcp.WidgetClientJS()`:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/core/mcp"

var widgetHTML string
-->
```go
widgetHTML = "<script>" + string(mcp.WidgetClientJS()) + "</script>"
```

Inside the iframe, `window.__gofastrMcpApp` speaks ext-apps `2026-01-26` JSON-RPC over `postMessage` (`window.parent` only):

- **Handshake & theme:** `connect(appCapabilities, timeoutMs?, appInfo?)` negotiates `ui/initialize`, applies `hostContext.theme` (`data-theme`, `color-scheme`, CSS variables, fonts) automatically, and sends `ui/notifications/initialized`. Read merged context with `hostContext()`.
- **Widget → host:** `callTool`, `readResource`, `openLink`, `requestDisplayMode`, `message`, `updateModelContext`, `request(method, params)`, `sizeChanged`, `notify(method, params)`.
- **Host → widget:** `onToolInput`, `onToolInputPartial`, `onToolResult`, `onToolCancelled`, `onHostContextChanged`, `onMessage`, `on(method, handler)`, and `onResourceTeardown(handler)` (handles `ui/resource-teardown` before teardown).
- **Client rejection codes:** `E_TIMEOUT` (default 30s), `E_SATURATED` (64 in-flight cap), `E_SEND`, `E_TEARDOWN`.

## Common mistakes

- **Using `mcp.Gated` expecting tools to disappear from `tools/list`.** `Gated` only runs on `tools/call`; use `WithToolGate` to hide from listings too.
- **Checking auth inside a resource contents func instead of `WithResourceGate`.** Without `WithResourceGate`, the URI/description remain visible in `resources/list` and receive update notifications.
- **Serving `ListTools()` remotely.** It skips all gates—always use `ListToolsFor(ctx)` or the transport handler.
- **Loading the widget client via `<script src>`.** Sandboxed MCP App iframes resolve relative URLs against the chat host, not your server. Always inline `mcp.WidgetClientJS()` or use `mcp.WidgetDocument`.
- **Trusting `RequestMeta` (`openai/subject`) for authorization.** `_meta` is unauthenticated client input; authorize using your auth middleware.
- **Leaving annotation hints `nil` expecting `false`.** Unset hints are omitted from `tools/list`, and MCP clients default a missing `destructiveHint` to `true`. Set hints explicitly with `mcp.Bool`.
- **Assuming cursors or SSE notifications cross replicas.** Both are process-local; use sticky sessions for `/mcp` or tolerate reconnect/re-list.
