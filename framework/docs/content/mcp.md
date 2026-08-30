# MCP server

`core/mcp` is the Model Context Protocol server behind `/mcp`: a JSON-RPC 2.0
dispatcher with registries for tools, resources, resource templates, and
prompts, per-caller gates over all of them, and stdio plus HTTP/SSE transports.
A `framework.App` creates one at `app.MCP`, and `framework.WithMCP()` mounts it
at `/mcp` over Streamable HTTP (POST JSON-RPC + GET Server-Sent Events).
Entities declared with `mcp: true` get CRUD tools on it automatically; see
[entity declarations](entity-declarations.md). The framework-level options
(`WithMCPIntrospection`, `WithMCPControl`, `WithMCPApp`, discovery endpoints)
and the auth model per tool kind are covered in
[agent-readiness](agent-ready.md). This page is the `core/mcp` reference.

Used standalone, with no framework around it:
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

`SetServerName` overrides just the name. `ServerInfo()` reads the pair back;
the MCP server card mirrors it.

## The wire surface

`Server.HandleRequest` dispatches these JSON-RPC methods:

| Method | Serves |
|---|---|
| `initialize` | The handshake: protocol version `2025-06-18`, capabilities, `serverInfo`. |
| `ping` | Liveness check; empty result. |
| `tools/list` | One page of the tools the caller may see, name order. |
| `tools/call` | Invokes a tool by name with an `arguments` object. |
| `resources/list` | One page of registered resources, URI order, metadata only. |
| `resources/read` | A resource's contents by `uri`. |
| `resources/templates/list` | One page of the templates the caller may see, uriTemplate order. |
| `resources/subscribe` | Arms `resources/updated` notifications for a `uri`. Empty result. |
| `resources/unsubscribe` | Releases one `resources/subscribe`. Empty result. |
| `prompts/list` | One page of the prompts the caller may see, name order. |
| `prompts/get` | A prompt's messages by name and string `arguments`. |

Any other method returns `-32601` method not found. A request whose `jsonrpc`
field is not `"2.0"` returns `-32602`. The three error codes the server emits
are exported: `ErrMethodNotFound` (-32601), `ErrInvalidParams` (-32602),
`ErrInternalError` (-32603), carried in `Response.Error` as an `*RPCError`.

`initialize` advertises capabilities from the registries: `tools` always;
`resources` once any resource or template is registered (one capability covers
both, so a templates-only server advertises it); `prompts` once any prompt is
registered. Every list capability reports `listChanged: true`, and resources
reports `subscribe: true`: the server pushes change notifications over the
SSE stream (see [notifications](#server-initiated-notifications)).

## Registering a tool

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

`RegisterTool(name, description, inputSchema, fn, opts...)` returns an error
on an empty name, a nil handler, or a duplicate name. The `inputSchema` is a
JSON Schema as `map[string]any`, serialized verbatim in `tools/list`.

A `ToolHandler` returns `any`, normalized by type:

| Return | `tools/call` result |
|---|---|
| `mcp.ToolResult` | Explicit content blocks and/or `structuredContent` |
| `mcp.ImageResult` | One base64 image block, rendered inline by clients |
| `mcp.Content` / `[]mcp.Content` | The block(s), built with `TextContent`, `ImageContent`, `AudioContent`, `ResourceContent` |
| `string` | One text block |
| anything else | JSON-marshaled into one text block |

A non-nil error becomes a JSON-RPC error. For a failure the caller should see
as tool output rather than a transport error, return
`mcp.ToolResult{IsError: true}`. The canonical rich-result examples (images,
structured output mirroring a text block) live in
[agent-readiness](agent-ready.md).

Options:

| Option | Effect |
|---|---|
| `WithOutputSchema(schema)` | Declares the JSON Schema of the tool's `structuredContent`; served as `outputSchema` in `tools/list`. |
| `WithToolMeta(meta)` | Attaches a `_meta` object, serialized verbatim in `tools/list`. MCP Apps use it to link a tool to its UI resource, e.g. `{"ui": {"resourceUri": "ui://app/widget.html"}}`. |
| `WithToolGate(gate)` | Per-caller precondition; see [gating](#gating). A nil gate panics. |

Three more calls matter when you hold the server in-process:

- `CallTool(ctx, name, params)` invokes a tool without the JSON-RPC transport.
  It runs the tool's own gate (`WithToolGate`) against `ctx`; it does not run
  the server-wide gate (`SetGate`), because an in-process caller is not coming
  through `/mcp`. Every remote transport dispatches via `HandleRequest`, which
  does apply it.
- `ListToolsFor(ctx)` is the listing `tools/list` serves: per-tool gates and
  the server-wide gate both applied. `ListTools()` applies neither per-caller
  gate; it is introspection for code that already holds the server, and its
  output must never be served to a remote caller.
- `HasTool(name)` reports registration truth regardless of gates (a gated
  tool's name is still taken).

`SetRegisterHook` and `SetCallGate` are framework wiring: the framework uses
them to attribute tools to modules and to refuse tools owned by a disabled
module. A disabled module's tool returns a generic internal error
("tool unavailable") from `tools/call`.

## Registering a resource

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

The contents func runs per `resources/read`, receiving the request context
(auth/tenant enriched). `ResourceContents` holds exactly one of `Text`
(UTF-8) or `Blob` (arbitrary bytes, base64 on the wire); a set `MimeType`
overrides the resource's declared one for that read. Registering any resource
makes `initialize` advertise the `resources` capability. `RegisterResource`
returns an error on an empty `uri` or `name`, a nil contents func, or a
duplicate `uri`.

`resources/list` serves metadata (uri, name, description, mimeType, `_meta`)
in URI order; contents funcs never run for a listing. `resources/read`
resolves the `uri` with a map lookup, no filesystem access, and an unknown
`uri` is an error.

Options:

| Option | Effect |
|---|---|
| `WithResourceDescription(desc)` | Human/agent-readable description, listed. |
| `WithResourceMeta(meta)` | Attaches `_meta`, serialized verbatim in `resources/list`. MCP Apps ride `csp`/`permissions` on `_meta.ui`. |
| `WithResourceGate(gate)` | Refuses unauthorized reads; see below. |

`WithResourceGate` runs before the contents func on every read and refuses the
read on error. It protects the contents, not the listing: the resource's uri,
name, and description still appear in `resources/list`, which has no
per-caller filter and pages the whole registry. That is deliberate, because a
concrete resource still has a read to refuse. If the metadata itself must stay
hidden, do not register it as a resource.

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

## Registering a resource template

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

A `ResourceTemplate` is an RFC 6570 URI template a client expands with its own
parameters to derive concrete resource URIs instead of listing them one by
one. The template is stored and advertised verbatim, never expanded
server-side, and registering one creates no readable resource by itself: the
concrete URIs it expands to are served through `resources/read` like any other
resource, so each one still has to be registered to be readable. Registering
at least one template also advertises the `resources` capability (the spec has
one capability for both). Errors: empty `uriTemplate` or `name`, duplicate
`uriTemplate`.

Options: `WithResourceTemplateDescription`, `WithResourceTemplateMeta`
(verbatim `_meta`), and `WithResourceTemplateGate`.

The template gate behaves differently from the resource gate, because a
template has no read path of its own. The listing entry is the whole
disclosure, so `WithResourceTemplateGate` filters `resources/templates/list`
per caller, the same contract prompts use: a caller the gate refuses never
sees the template.

## Registering a prompt

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

A prompt is a named template a client fills with arguments and feeds to a
model. Prompts are user-selected (the spec imagines them as slash commands);
tools are model-called. `PromptHandler` runs per `prompts/get` with the
request context and the caller's string arguments, returning `[]PromptMessage`,
each a `Role` ("user" or "assistant") plus a content block of the same type
`tools/call` uses.

Registering any prompt advertises the `prompts` capability. Errors: empty
`name`, nil handler, duplicate name.

Required arguments are enforced for you: a `prompts/get` missing a
`PromptArgument` marked `Required` is refused with an invalid-params error
before the handler runs. An unknown prompt name is the same error.

Options: `WithPromptDescription`, `WithPromptArguments(args...)`, and
`WithPromptGate` (per-caller; see below).

## Gating

The gating property, stated plainly: a gated tool, prompt, or resource
template is hidden from its list method for a caller the gate refuses, and
refused on access. A gated resource keeps its metadata listed and refuses the
read. In both shapes, pagination pages the post-gate set, so page shapes
cannot betray a hidden item: it never appears on a page, never shortens one,
and never bends the cursor offsets that would otherwise count it.

A gate is `func(ctx context.Context) error`. It receives the request context,
auth/tenant enriched, carrying the inbound `*http.Request`
(`RequestFromContext`), so it can read the Cookie or Authorization header the
caller presented at `/mcp`. battery/auth ships ready-made gates:
`auth.MCPUser()` requires any signed-in caller, `auth.MCPRole("a", "b")`
requires any of the roles. A nil gate panics, at registration time, because a
nil precondition would silently allow every caller.

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
// Per-item gates: hidden from the listing AND refused on access.
srv.RegisterTool("orders_ship", "Ship a pending order", inputSchema, shipHandler,
	mcp.WithToolGate(auth.MCPRole("support", "admin")))

srv.RegisterPrompt("incident_postmortem", postmortemHandler,
	mcp.WithPromptGate(auth.MCPRole("oncall")))

srv.RegisterResourceTemplate("help://docs/{topic}", "Internal doc by topic", "text/markdown",
	mcp.WithResourceTemplateGate(auth.MCPUser()))

// Whole endpoint private: one gate over every data method.
srv.SetGate(auth.MCPUser())
```

`mcp.Gated(gate, handler)` wraps a tool handler with the same gate signature.
It only ever reaches `tools/call`: the listing never consulted it, so a caller
who could not invoke the tool still saw its full `inputSchema`, which for
entity CRUD tools is built from live entity definitions. Prefer
`WithToolGate`, which applies the same predicate on both the listing and the
call. `Gated` remains for wrapping a handler before passing it somewhere that
takes only a `ToolHandler`.

`SetGate` installs one server-wide precondition over the data methods
(`tools/list`, `tools/call`, `resources/list`, `resources/read`,
`resources/templates/list`, `resources/subscribe`,
`resources/unsubscribe`, `prompts/list`, `prompts/get`); pass nil to
clear it. It also silences notifications for a caller it refuses — see
[notifications](#server-initiated-notifications). `initialize` and
`ping` stay open on purpose: they carry only the protocol version,
capability booleans, and the server name, and a client that cannot
complete the handshake cannot present credentials in a way any MCP
client implements. In a framework app,
`framework.WithMCPGate(framework.MCPRequireUser())` is this same switch
(see [agent-readiness](agent-ready.md)).

On `tools/call` the checks run in order: resolve the tool (unknown name:
method-not-found), the framework call gate (disabled module: a generic
internal error), the tool's own gate (invalid-params carrying the gate's
error), then the handler. The handler runs under a recover guard, so a panic
in handler code becomes an internal error with no detail echoed, instead of
unwinding the transport loop; the same guard covers resource contents funcs
and prompt handlers.

## Server-initiated notifications

The GET half of `ServeSSE` holds the connection open and streams
server-initiated notifications to the client, each as one SSE `message`
event carrying a JSON-RPC notification (a method, optional params, no id):

| Method | Payload | Raised |
|---|---|---|
| `notifications/tools/list_changed` | none | `RegisterTool` |
| `notifications/resources/list_changed` | none | `RegisterResource`, `RegisterResourceTemplate` |
| `notifications/prompts/list_changed` | none | `RegisterPrompt` |
| `notifications/resources/updated` | `{"uri": "..."}` | your code, via `NotifyResourceUpdated(uri)` |

A client subscribes by GETting the SSE endpoint and keeping it open; a
`list_changed` tells it to re-issue the corresponding list method. For
resource updates it must also send `resources/subscribe` with the `uri` —
`NotifyResourceUpdated` is a no-op until at least one subscription is
active. Subscriptions are refcounted per `uri`: every
`resources/subscribe` adds one, every `resources/unsubscribe` releases
one, and delivery stops only when the count for that `uri` reaches
zero. A `uri` longer than 2048 bytes is refused, the server retains at
most 1024 distinct subscribed uris (a `resources/subscribe` past that
fails), and when the last stream disconnects the retained
subscriptions are dropped — with no stream there is nobody to deliver
to. Raising one from server code:

<!-- gofastr:compile
import (
	"github.com/DonaldMurillo/gofastr/core/mcp"
)

var srv *mcp.Server
-->
```go
// the docs resource changed; tell every subscribed, eligible stream
srv.NotifyResourceUpdated("docs://api/quickstart")
```

Notifications are filtered per subscriber, so a stream cannot tell a
caller anything the matching methods would not:

- Every notification passes the server-wide gate (`SetGate`). A caller
  refused wholesale receives nothing at all — not even a payload-free
  `list_changed`, which is otherwise safe to broadcast.
- `notifications/resources/updated` additionally passes the resource's
  own `WithResourceGate`, so it reaches only streams whose caller may
  read that resource's contents. This does not hide the resource from
  `resources/list` — resource metadata stays listed by design, and the
  gate refuses the read, not the listing; it keeps the update notice
  from callers who could never read the result. Tools, prompts and
  resource templates are the other shape: their gates hide the item
  from its list method, and the `list_changed` a gated tool or
  resource template registration fires is likewise withheld from
  callers its gate refuses, who cannot see the item in that listing
  either.
- Both gates are evaluated at delivery time against the identity the
  stream's GET request presented, not once when the stream opened. A
  session revoked mid-stream stops receiving immediately.

A slow subscriber never blocks the server. Each stream has a bounded
buffer (16 notifications); a full buffer means the client stopped
reading, and the server drops that subscriber and closes its stream
rather than stalling the code that raised the notification — which
would otherwise stall every other subscriber too. `list_changed` is
idempotent and `resources/updated` sends the client back to
`resources/read` for current state, so a dropped client that reconnects
and re-lists is correct again.

Two transport limits, both inherited from the stream being per process:

- The HTTP transport has no session id linking a POST to a GET stream,
  so `resources/subscribe` is counted per `uri` across connections: a
  `uri` one client subscribed to gets updates on every eligible stream.
- The subscriber registry lives in the process that holds the
  connections. A notification raised on one replica does not reach
  clients connected to another — the same class of limit as the
  per-process cursor signing key (see [pagination](#pagination)). Route
  SSE sessions to one replica or accept that clients reconnect and
  re-list.

The stdio transport has no server-push channel, so the `Notify*` methods
are no-ops there.

## Pagination

Every list method (`tools/list`, `resources/list`, `resources/templates/list`,
`prompts/list`) serves pages of 100 by default. The client contract:

1. Send the list request, optionally with a `cursor` param.
2. If the response has `nextCursor`, repeat the request with it.
3. Stop when `nextCursor` is absent.

A listing that fits on one page has no `nextCursor` key at all, the
pre-pagination wire shape, so single-page clients see exactly what they saw
before pagination existed.

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
		return // transport failed
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
		break // last page, or the whole listing fit on page one
	}
	cursor = out.Result.NextCursor
}
```

Cursors are opaque and server-signed:
`v1.<base64url(payload)>.<base64url(HMAC)>`, the HMAC keyed by a random
per-process secret and covering the version and payload bytes. The payload
carries only the list method that minted the cursor and a resume offset into
that method's post-filter, sorted listing. No total count, no page size, so a
cursor cannot be turned into an oracle for how many items exist.

What a client sees at the edges:

- A malformed, tampered, or foreign cursor returns an invalid-params error
  ("invalid cursor"), generic on purpose: the cursor is client-supplied, so
  the error neither echoes it nor explains which check failed. A cursor from
  one list method is foreign to every other.
- A stale cursor, one whose offset is past the end because items vanished
  between pages, ends the walk: an empty page, no `nextCursor`, no error.
- The signing key is per process. A restart invalidates outstanding cursors
  and the client restarts the walk from page one, which the
  repeat-until-absent loop does anyway. The same limit applies to replicas: a
  load-balanced `/mcp` rejects another replica's cursor with the same
  invalid-params error, so route a paged session to one replica or tolerate
  restarted walks (see [scaling](scaling.md)).

## Transports

- `ServeHTTP` answers POST with one JSON-RPC request per call. It enforces an
  `application/json` content type (parameters allowed, plus the `+json`
  structured-suffix family) and caps the body at 1 MiB.
- `ServeSSE(path)` returns an http.Handler where POST handles JSON-RPC and GET
  with `Accept: text/event-stream` opens a stream the server holds open for
  the connection's life, carrying server-initiated notifications (see
  [notifications](#server-initiated-notifications)). The origin/Host gate
  runs on the GET half too: the stream it protects is a read.
- `ServeStdio(ctx, in, out)` reads line-delimited JSON-RPC from `in` and writes
  responses to `out`, blocking until EOF or context cancellation. The recover
  guard around handler code matters most here: stdio has no net/http
  per-request net, so a panic would otherwise crash the process.

The HTTP transports refuse a browser `Origin` that is not same-origin with the
request; an absent Origin passes, because curl, stdio bridges, and native MCP
clients never send one. `SetAllowedHosts` pins the Host authorities the server
answers on (the DNS-rebinding control), `SetRequireLoopbackHost` restricts it
to loopback authorities on any port, and `SetAllowedOrigins` permits specific
foreign Origins for tunnels and split-origin dev clients.
[Security defaults](security.md) covers when to set each.

`WithRequest` stashes the inbound `*http.Request` in the context the
transports hand to handlers; `RequestFromContext` reads it back. Tool handlers
that re-dispatch through an HTTP router copy the caller's Cookie /
Authorization header from it so session middleware re-resolves the same user.
`StreamSSE(w, event, data)` is the hardened entry point for streaming a tool
result as one SSE event.

`RegisterApp(mcp.AppConfig{...})` registers an MCP App, an interactive HTML
widget plus the tool that launches it, as a `ui://` resource and a linking
tool in one call. The canonical example is in
[agent-readiness](agent-ready.md).

`AppConfig.CSP` is the spec's structured `_meta.ui.csp` object (an
`mcp.AppCSP`), not a policy string. The host assembles the iframe's actual
Content-Security-Policy from its origin allowlists:

```go
CSP: mcp.AppCSP{
	ConnectDomains:  []string{"https://api.openweathermap.org"}, // connect-src
	ResourceDomains: []string{"https://cdn.jsdelivr.net"},        // img/script/style/font/media-src
},
```

The remaining fields are `FrameDomains` (frame-src) and `BaseURIDomains`
(base-uri; the wire name is `baseUriDomains`, the spec's spelling). An empty
or omitted field allows no external origins, and a zero `AppCSP` emits no
`csp` key at all.

## MCP Apps widget client

`RegisterApp` ships the server half of an MCP App: the `ui://` resource and
the linking tool. The widget half — the JavaScript that runs inside the
host's sandboxed iframe and talks to the chat host — is the widget client,
a plain embedded script with no imports and no external URLs. It speaks the
ext-apps widget protocol (spec 2026-01-26): JSON-RPC 2.0 over `postMessage`,
no extra framing. GoFastr serves the widget side of MCP Apps and is not a
host: nothing here renders another server's widget, and nothing in the
framework speaks the host half of this protocol.

A framework app serves the script by itself: as soon as one MCP App is
registered with `WithMCPApp`, `Start` mounts `mcp.WidgetClientHandler()`
at `mcp.WidgetClientScriptURL` on the app router. The condition follows
the server's capability rule (`resources` and `prompts` are advertised
only when something is registered): no widget, no public script route.
The mount does not require `WithMCP()`; a host that wired `/mcp` by hand
still gets it, because the widget's HTML references this URL on the app's
public router either way. A router already serving the URL (you mounted
the handler yourself) keeps that route: the automatic mount steps aside
with a warning, and the bytes are the same. In a role-split deployment
the agent role (`framework.WithRole(framework.RoleAgent)`) forwards the
script URL to the app router along with `/mcp`, so widgets keep loading
when the agent process is the origin the MCP host resolved.

Assembling that HTML is what `mcp.WidgetDocument` is for: it emits the
document around your markup and script with the client's `<script src>`
taken from [WidgetClientScriptURL] by construction — the one-line typo
that kills a widget silently cannot happen. Escaping rules, the script
guards, and the end-to-end walkthrough live in
[agent host](agent-host.md).

Two ways to get the script into your widget's HTML:

<!-- gofastr:compile
import (
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/router"
)

var rt *router.Router
var widgetHTML string
-->
```go
// Hot-link: reference the script URL from a <script src> in the app's
// HTML. A framework app that uses WithMCPApp serves it automatically
// (see above); mounting the handler yourself is for a router assembled
// by hand (core/mcp standalone, or RegisterApp without WithMCPApp). The
// handler serves the script with Cross-Origin-Resource-Policy:
// cross-origin so the opaque-origin iframe can fetch it; whether the
// HOST's iframe CSP allows the fetch is governed by the app's declared
// AppCSP.ResourceDomains, like any cross-origin script.
rt.Get(mcp.WidgetClientScriptURL, mcp.WidgetClientHandler())

// Or fold the bytes into the HTML you hand to RegisterApp, and the widget
// carries its own client with no route and no CSP dependency.
widgetHTML = "<script>" + string(mcp.WidgetClientJS()) + "</script>"
```

The script URL is `/__gofastr/mcp/app/widgetclient.js`
([WidgetClientScriptURL]). Inside the widget document the client exposes
itself as `window.__gofastrMcpApp`:

```js
var app = window.__gofastrMcpApp;

app.connect({ availableDisplayModes: ["inline"] }).then(function (host) {
  // host.protocolVersion, host.hostCapabilities, host.hostInfo,
  // host.hostContext (theme, locale, displayMode, …)
});

app.callTool({ name: "search", arguments: { q: "gofastr" } })
  .then(function (result) { /* result.content, result.isError */ });

app.onToolResult(function (params) { /* a finished tool call */ });
app.sizeChanged({ width: 320, height: 240 });
```

### The handshake

`connect(appCapabilities, timeoutMs?)` sends the `ui/initialize` request
carrying your `appCapabilities` (`experimental`, `tools.listChanged`,
`availableDisplayModes`), resolves with the host's result — `protocolVersion`,
`hostCapabilities`, `hostInfo`, `hostContext` — and only then sends the
`ui/notifications/initialized` notification. Call it once, on load. If the
host never answers, the promise rejects after `timeoutMs` (default 30s) and
you may call `connect` again.

### The method surface

Widget → host requests, each a named method plus the generic escape hatch
(`params` is the spec's params object, passed through verbatim):

| Client call | Wire method |
|---|---|
| `callTool(params, timeoutMs?)` | `tools/call` |
| `readResource(params, timeoutMs?)` | `resources/read` |
| `openLink(params, timeoutMs?)` | `ui/open-link` |
| `requestDisplayMode(params, timeoutMs?)` | `ui/request-display-mode` |
| `message(params, timeoutMs?)` | `ui/message` |
| `updateModelContext(params, timeoutMs?)` | `ui/update-model-context` |
| `request(method, params, timeoutMs?)` | any spec request method |

Widget → host notifications: `sizeChanged(params)` sends
`ui/notifications/size-changed`; `notify(method, params)` sends any other.

Host → widget notifications, each with a named registrar plus the generic
`on(method, handler)`:

| Registrar | Wire method |
|---|---|
| `onToolInput(handler)` | `ui/notifications/tool-input` |
| `onToolInputPartial(handler)` | `ui/notifications/tool-input-partial` |
| `onToolResult(handler)` | `ui/notifications/tool-result` |
| `onToolCancelled(handler)` | `ui/notifications/tool-cancelled` |
| `onHostContextChanged(handler)` | `ui/notifications/host-context-changed` |
| `onMessage(handler)` | `notifications/message` |

A notification with no registered handler is ignored, never thrown; a
handler that throws is logged to `console.error` and does not break the
dispatch loop.

### Host teardown

`ui/resource-teardown` is the one host → widget **request** (ext-apps
2026-01-26): the host sends it with a request id and SHOULD wait for the
response before tearing the resource down, to prevent data loss. Register
a request handler with `onResourceTeardown(handler)`; `params.reason`
says why the widget is going away. The handler may return a promise —
the client answers the request only after it settles, with `result: {}`
on success or a JSON-RPC error if the handler throws — and then fails
every request still in flight with `E_TEARDOWN`, because the host that
is removing the widget will never answer them.

### Failures

Every request returns a Promise. It rejects with the host's JSON-RPC error
object when the host answers with an error, or with one of the client's own
codes:

| Code | Meaning |
|---|---|
| `E_TIMEOUT` | No response within `timeoutMs` (default 30s — pass a larger value for slow `tools/call` turns). |
| `E_SATURATED` | 64 requests already in flight; rejected before posting. |
| `E_SEND` | `postMessage` threw — usually params that are not structured-cloneable. |
| `E_TEARDOWN` | The widget was torn down (`ui/resource-teardown`) or the page hidden. |

Responses that arrive after a timeout or teardown are dropped by request id,
so a late answer never resolves an already-rejected Promise. Inbound
messages are accepted only from `window.parent` (`event.source`, not
`event.origin` — the sandboxed widget's origin is opaque and reports as
`"null"`), and only when the envelope says `jsonrpc: "2.0"`.

### Host theme

The client consumes the host's theme signals so the widget does not
invent a palette: on `connect()` and on every
`ui/notifications/host-context-changed` (partial updates merged into the
running state), it applies `hostContext.theme` to the document root as
`<html data-theme="light|dark">` plus `color-scheme` (which makes the
host's `light-dark()` variable values resolve), writes
`hostContext.styles.variables` to the root as inline custom properties,
and injects `hostContext.styles.css.fonts` as one replaced-never-stacked
`<style>` element. Application runs before any registered
`onHostContextChanged` handler, and `app.hostContext()` returns a copy of
the merged state. The convention and its boundaries:
[agent host](agent-host.md).

## Common mistakes

- **Wrapping a handler in `mcp.Gated` and expecting the tool to disappear from
  `tools/list`.** `Gated` wraps the handler, so it only reaches `tools/call`;
  the listing still shows the tool and its full `inputSchema`. Use the
  `WithToolGate` registration option, which gates both.
- **Expecting `WithResourceGate` to hide the resource.** It protects the
  contents. The uri, name, and description stay in `resources/list`, which has
  no per-caller filter. A resource whose metadata is itself sensitive is the
  wrong shape; do not register it.
- **Reusing a cursor across list methods.** The minting method is inside the
  signed payload, so `tools/list`'s cursor is invalid-params everywhere else.
  Mint a fresh walk per method.
- **Paging a load-balanced `/mcp` without sticky routing.** The HMAC key is
  per process, so a replica rejects another replica's cursor. Either pin a
  paged session to one replica or treat the invalid-params error as "restart
  the walk".
- **Serving `ListTools()` output to a remote caller.** It applies no
  per-caller gates. The JSON-RPC path serves `ListToolsFor(ctx)`; reach for
  that (or let the transport do it) whenever the listing leaves the process.
- **Calling `app.MCP.RegisterApp` directly and expecting the script
  route.** The automatic widget client mount keys on `WithMCPApp`
  registrations. Registering through the primitives, or running
  `core/mcp` standalone, means mounting `WidgetClientHandler()` yourself.
- **Hand-writing the widget client `<script src>`.** The URL must be
  exactly `mcp.WidgetClientScriptURL`; a one-character typo is a widget
  that renders and silently never receives anything. Build the document
  with `mcp.WidgetDocument`, which bakes in the constant
  ([agent host](agent-host.md)).
- **Forgetting that required prompt arguments are checked before the
  handler runs.** A `prompts/get` missing one is refused with invalid-params;
  the handler never sees the call. Handlers can assume required arguments are
  present.
- **Expecting notifications to cross replicas.** The subscriber registry is
  per process: a notification raised on one replica reaches only the clients
  connected to it. Route SSE sessions to one replica, or accept that a
  client reconnects and re-lists — which the spec's notification handling
  already assumes.
