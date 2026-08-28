# WebMCP bridge (experimental)

`framework/experimental/webmcp` exposes server-declared tools to
in-browser AI agents through the [WebMCP proposal](https://developer.chrome.com/docs/ai/webmcp)
(`navigator.modelContext`). The server declares each tool's name,
description, JSON Schema, and the same-origin endpoint that implements
it; a mounted bridge script registers the set on `navigator.modelContext`
and proxies each `execute()` call back to that endpoint with the
visitor's own session credentials.

This is the browser-side twin of the server MCP surface (`WithMCP*`):
the server surface serves agents that connect to the app over HTTP with
a token; the WebMCP bridge serves agents that live in the visitor's
browser (Gemini in Chrome, the Model Context Tool Inspector extension,
CDP-driven automation) and act as the signed-in user.

Everything here is experimental twice over: the package lives under
`framework/experimental` (no stability contract), and WebMCP itself is a
Chrome origin-trial API. Chromium 146+ exposes it behind
`chrome://flags/#enable-webmcp-testing`; the origin trial runs from
Chrome 149. Automation and tests enable it with
`--enable-blink-features=WebMCP`. On any browser without the API the
bridge script feature-detects and does nothing.

## Declaring and mounting tools

<!-- gofastr:compile
import "log"
import "encoding/json"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/uihost"
var app *framework.App
var uiHost *uihost.UIHost
-->
```go
import (
    "github.com/DonaldMurillo/gofastr/framework/experimental/webmcp"
)

// app is the *framework.App, uiHost the mounted *uihost.UIHost.
h := webmcp.New()
if err := h.Register(webmcp.Tool{
    Name:        "create_note",
    Description: "Create a note for the signed-in user.",
    InputSchema: json.RawMessage(`{"type":"object","properties":{"body":{"type":"string"}},"required":["body"]}`),
    Method:      "POST",
    Path:        "/api/notes",
}); err != nil {
    log.Fatal(err) // misdeclared tool: fix it, don't swallow it
}

// Register every tool before Mount; the set freezes here. Mount serves
// the bridge + manifest and puts the script on every full-page render.
if _, err := h.Mount(app.Router(), uiHost); err != nil {
    log.Fatal(err)
}
```

`Mount` freezes the tool set, serves the bridge at
`/__gofastr/webmcp.js` (hash-versioned, immutable caching) and the
manifest at `/__gofastr/webmcp/tools.json` (for tests, introspection,
and server-side agents), and registers the script URL through the
`ScriptRegistrar` seam. Pass `nil` for the registrar to inject the
returned `scriptURL` into your own markup instead.

## Tool fields

| Field | Required | Meaning |
|---|---|---|
| `Name` | yes | Unique per page, constrained to the WebMCP spec grammar `[A-Za-z0-9_.-]{1,128}` (the browser rejects anything else asynchronously, which reads as a tool that never existed; `Register` fails loudly instead). The browser also refuses duplicate registrations, so it must not collide with tools the app registers from its own JS. |
| `Title` | no | Human-readable display name. |
| `Description` | yes | What the tool does and when to use it. An agent cannot pick an undescribed tool, so `Register` refuses a blank one. |
| `InputSchema` | no | JSON Schema object for the input. Defaults to `{"type":"object","properties":{}}`. |
| `Method` | no | `GET`, `POST`, `PUT`, `PATCH`, or `DELETE`. Defaults to `POST`. |
| `Path` | yes | Same-origin absolute path of the implementing endpoint. Query strings allowed; schemes, hosts, fragments, backslashes, control characters, and `.`/`..` segments are rejected. |

`Register` returns an error naming the exact field for any violation,
and refuses registration after `Mount` (the script and manifest are
frozen so every render ships the same set). `Mount` refuses a zero-tool
mount, a second mount, and a router that already holds the bridge
routes (mount one Host per router). A failed `Mount` registers nothing
and leaves the Host re-mountable.

## The endpoint contract

When an agent invokes a tool, the bridge issues a same-origin `fetch` to
the declared endpoint:

- **Credentials**: `same-origin`, so the visitor's session cookies ride
  along like any first-party fetch.
- **CSRF**: on unsafe methods (everything but `GET`) the bridge reads
  `meta[name="csrf-token"]` at dispatch time and sends it as
  `X-CSRF-Token`, the same double-submit convention the core runtime's
  RPCs use, so endpoints behind `middleware.CSRF` work unchanged. A
  page that renders no such meta tag sends no token; behind the CSRF
  middleware those calls 403 and surface to the agent as `isError`.
- **Marker header**: `X-Gofastr-WebMCP: 1` on every call. This is a
  hint any page script can forge, so use it for annotation (audit-log
  notes, differentiated rate limits), never for authorization.
- **Input mapping**: `GET` folds the input object into the query
  string: objects and arrays are JSON-encoded, other values are
  stringified, `null`/`undefined` values are skipped, and an input key
  overrides a baked-in query param of the same name on the declared
  `Path`. Every other method sends the input as a JSON body with
  `Content-Type: application/json`.
- **Result**: the response body is returned to the agent verbatim as
  one `text` content item. A non-2xx status sets the MCP `isError`
  flag; a network failure produces an `isError` result rather than a
  thrown exception, so the agent always gets a structured answer.

## Security model

A WebMCP tool call is exactly a `fetch` the page could already make:
it re-enters the app through normal HTTP with the user's cookies, so
auth, CSRF, owner scoping, tenant scoping, and rate limits all apply
unchanged, and the agent can do nothing the signed-in user couldn't do
from the devtools console.

## Testing with a real browser

The package's own e2e (`browser_test.go`) is the template: launch
Chromium with the flag, load a page carrying the bridge, and drive
`navigator.modelContext` directly.

chromedp:

```go
opts := append(chromedp.DefaultExecAllocatorOptions[:],
    chromedp.Flag("enable-blink-features", "WebMCP"),
)
```

Playwright:

```js
const browser = await chromium.launch({
  args: ['--enable-blink-features=WebMCP'],
});
```

Empirical notes on the Chromium 151 API surface (this is what the
bridge and tests are written against; it may change while the proposal
is in trial):

- The spec's IDL exposes `modelContext` on `Document`; Chromium
  additionally exposes the same object on `Navigator` (the legacy
  binding). The bridge probes `document.modelContext` first and falls
  back to `navigator.modelContext`.
- `registerTool(descriptor)` returns a promise resolving to
  `undefined`; tool handles come from `getTools()`.
- `executeTool(handle, input)` takes the handle from `getTools()` plus
  the input as a **JSON string**, and resolves with the tool's return
  value JSON-stringified.
- Duplicate names reject with `InvalidStateError`.
- The API only exists in secure contexts (`data:` URLs don't get it).

Chromium also ships a `WebMCP` CDP domain (`WebMCP.enable`,
`WebMCP.invokeTool`), so automation can invoke page tools without
evaluating script; the bridge does not depend on it.

## Common mistakes

- **Declaring an endpoint more powerful than the page**, on the theory
  that "only the agent will call it". Any page script can call the same
  endpoint. Only declare endpoints you are comfortable exposing to the
  signed-in user directly, because that is the actual trust boundary.
- **Registering tools after `Mount`.** The script and manifest freeze
  at `Mount` so every render ships the same set; late registrations
  return an error. Declare everything first, mount once.
- **Testing against a `data:` URL.** `modelContext` only exists in
  secure contexts, so a `data:` page reports the API absent even with
  the flag on. Use `http://localhost`, `file://`, or HTTPS.
- **CSRF-protected tools on a page without the token meta tag.** The
  bridge can only send what the page renders: if the shell does not
  emit `meta[name="csrf-token"]`, every unsafe-method tool behind
  `middleware.CSRF` 403s. Render the meta tag (the token is available
  server-side via `middleware.TokenFromContext`).
