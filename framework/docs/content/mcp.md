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
| `prompts/list` | One page of the prompts the caller may see, name order. |
| `prompts/get` | A prompt's messages by name and string `arguments`. |

Any other method returns `-32601` method not found. A request whose `jsonrpc`
field is not `"2.0"` returns `-32602`. The three error codes the server emits
are exported: `ErrMethodNotFound` (-32601), `ErrInvalidParams` (-32602),
`ErrInternalError` (-32603), carried in `Response.Error` as an `*RPCError`.

`initialize` advertises capabilities from the registries: `tools` always;
`resources` once any resource or template is registered (one capability covers
both, so a templates-only server advertises it); `prompts` once any prompt is
registered. All list capabilities report `listChanged: false` and resources
reports `subscribe: false`: the server emits no change notifications, so a
client that wants fresh state re-lists.

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
`resources/templates/list`, `prompts/list`, `prompts/get`); pass nil to clear
it. `initialize` and `ping` stay open on purpose: they carry only the protocol
version, capability booleans, and the server name, and a client that cannot
complete the handshake cannot present credentials in a way any MCP client
implements. In a framework app, `framework.WithMCPGate(framework.MCPRequireUser())`
is this same switch (see [agent-readiness](agent-ready.md)).

On `tools/call` the checks run in order: resolve the tool (unknown name:
method-not-found), the framework call gate (disabled module: a generic
internal error), the tool's own gate (invalid-params carrying the gate's
error), then the handler. The handler runs under a recover guard, so a panic
in handler code becomes an internal error with no detail echoed, instead of
unwinding the transport loop; the same guard covers resource contents funcs
and prompt handlers.

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
  with `Accept: text/event-stream` streams responses over SSE.
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
- **Forgetting that required prompt arguments are checked before the
  handler runs.** A `prompts/get` missing one is refused with invalid-params;
  the handler never sees the call. Handlers can assume required arguments are
  present.
