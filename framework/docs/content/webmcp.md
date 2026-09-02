# WebMCP bridge (experimental)

`framework/experimental/webmcp` exposes server-declared tools to
in-browser AI agents through the [WebMCP proposal](https://developer.chrome.com/docs/ai/webmcp)
(`document.modelContext`; Chromium also exposes the legacy
`navigator.modelContext` binding). The server declares each tool's name,
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

## One call for declaration and route

`Register` leaves the endpoint wiring to you: the manifest entry and the
router registration are two facts an app must keep in sync by hand, and
they drift (a manifest that advertises the wrong path, a handler no
agent can reach, a missing auth wrapper). `Handle` makes them one fact:

<!-- gofastr:compile
import "log"
import "net/http"
import "github.com/DonaldMurillo/gofastr/core/router"
import "github.com/DonaldMurillo/gofastr/framework/experimental/webmcp"
var tools *webmcp.Host
var rt *router.Router
var drawArrow http.Handler
var requireSameOrigin router.Middleware
-->
```go
if err := tools.Handle(rt,
    webmcp.Tool{
        Name:        "draw_arrow",
        Description: "Point to a precise spot on a verified object.",
        Method:      http.MethodPost,
        Path:        "/api/tools/draw-arrow",
    },
    drawArrow,
    webmcp.WithHTTPMiddleware(requireSameOrigin),
); err != nil {
    log.Fatal(err) // same validation contract as Register, plus route conflicts
}
```

`Handle` validates the declaration exactly like `Register`, registers
`handler` on the router at the declaration's method and path, and adds
the tool to the manifest. From that one declaration:

- The manifest cannot advertise a method or path the router does not
  serve, and a bound route cannot exist without its manifest entry.
- A conditional tool — an `if` around the `Handle` call — appears in,
  and disappears from, manifest and router together.
- `WithHTTPMiddleware` (outermost first, like `Router.Use`) puts the
  authorization decision next to the declaration it protects. A tool
  registered without it reads as a choice, not an oversight.
- `Handle` fails cleanly, registering neither half, for everything
  `Register` rejects, for a method+path already on the router, and
  after `Mount` has frozen the set.

The route stays an ordinary route: the app's own UI can POST to
`/api/tools/draw-arrow` and reach the same handler, and a WebMCP call
differs only in carrying the marker header. A query string in the
declared `Path` (e.g. `/api/search?source=baked`) is baked into the
bridge's fetches but is not part of the route pattern — the handler
reads it through `r.URL.Query()` like any other query parameter.

Declaration-only `Register` remains available for endpoints the app
wires itself (existing routes, batteries, generated CRUD surfaces).

## Mount policies for gated surfaces

The zero-option mount is the public posture and stays the default:
hash-versioned script with immutable caching, manifest with no-cache.
When WebMCP is a signed-in capability, three `Mount` options cover the
framework-owned routes directly — no response-writer interception:

- `WithAssetAuthorization(mw...)` wraps the bridge script and manifest
  routes with your middleware (outermost first, like `Router.Use`).
  Both routes, because the manifest names every tool and endpoint:
  discovery is an authority surface even when the tool endpoints check
  permissions themselves.
- `WithPageScope(pred)` gates those routes per request: a fetch that
  fails the predicate gets an empty script, or an empty tool set for
  the manifest, with no tool bytes. The predicate runs on the asset
  request, so it can see request identity (session, role), not the
  page path, and it runs inside the authorization middleware, so an
  anonymous request still fails with the middleware's status.
- `WithPrivateAssets()` serves both assets under
  `Cache-Control: private, no-store` instead of the public policies.

Any of the three implies that private policy: the assets become
requester-dependent, and a shared cache must never be able to replay
an authenticated bridge or manifest to anonymous traffic. Passing
`WithPrivateAssets()` alongside is harmless and reads clearly.

Signed-in site-wide — every page carries the bridge, sessions only can
fetch it:

<!-- gofastr:compile
stmt: _ = scriptURL
stmt: _ = err
import "net/http"
import "github.com/DonaldMurillo/gofastr/core/router"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/experimental/webmcp"
import "github.com/DonaldMurillo/gofastr/framework/uihost"
var tools *webmcp.Host
var app *framework.App
var uiHost *uihost.UIHost
var requireSession router.Middleware
var _ = http.MethodGet
-->
```go
scriptURL, err := tools.Mount(app.Router(), uiHost,
    webmcp.WithAssetAuthorization(requireSession),
    webmcp.WithPrivateAssets(), // implied by the line above; explicit reads better
)
```

Role-scoped support console — landing and operator pages carry nothing.
On a `uihost` app, `WithDocumentScope` (next section) is the
first-class shape: the rail itself becomes page-scoped and the SPA
boundary is enforced. Hosts that render their own tags pass a nil
registrar (the script rail ships on every page, which is wrong for a
scoped surface), render the returned URL yourself on the pages that
need it, and keep the predicate and auth as the fetch-time backstop:

<!-- gofastr:compile
stmt: _ = err
import "fmt"
import "io"
import "net/http"
import "github.com/DonaldMurillo/gofastr/core/router"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/experimental/webmcp"
var tools *webmcp.Host
var app *framework.App
var requireSupport router.Middleware
var hasSupportSession func(*http.Request) bool
var w io.Writer
-->
```go
scriptURL, err := tools.Mount(app.Router(), nil,
    webmcp.WithAssetAuthorization(requireSupport),
    webmcp.WithPageScope(hasSupportSession),
)

// In the support console layout only — landing pages never emit this:
fmt.Fprintf(w, `<script defer src=%q></script>`+"\n", scriptURL)
```

Two rules hold across every shape. Page inclusion is not
authorization: a page that renders the tag without the asset routes
being gated serves the manifest to whoever asks. And the marker header
attributes a call; it never grants one.

## Capability lifetime across navigation

Removing the bridge script from the DOM does not unregister tools: a
tool registered on `navigator.modelContext` belongs to the document,
not to the tag. A partial (SPA) swap never runs a body script either,
so a public page that soft-swapped over a support page would keep the
support tools alive in the same document. When the bridge belongs to
one part of an SPA host, declare that part as a document scope:

<!-- gofastr:compile
stmt: _ = scriptURL
stmt: _ = err
import "strings"
import "github.com/DonaldMurillo/gofastr/framework"
import "github.com/DonaldMurillo/gofastr/framework/experimental/webmcp"
import "github.com/DonaldMurillo/gofastr/framework/uihost"
var tools *webmcp.Host
var app *framework.App
var uiHost *uihost.UIHost
-->
```go
scriptURL, err := tools.Mount(app.Router(), uiHost,
    webmcp.WithDocumentScope(func(path string) bool {
        return strings.HasPrefix(path, "/support/")
    }),
)
```

The bridge tag then ships only on pages the scope accepts (marked
`data-fui-doc`), the route manifest declares the set for those routes,
and the host's client runtime turns the scope's edge into a document
boundary: entering or leaving it performs a real navigation instead of
a partial swap, and Back/Forward across the edge loads the destination
fresh, so a document only ever carries its own tools. Navigations
between two in-scope pages stay partial.

The predicate receives the registered route pattern (`/session/:id`),
the same value at render time and when the manifest is built, so a
route cannot be in scope on one side and out on the other. The scope is
structural, not authorization: it decides which document carries the
bridge, never who may fetch it. Keep `WithAssetAuthorization` (or
endpoint auth) alongside, and `WithPageScope` when the assets
themselves are requester-dependent. The option needs the host registrar
(`*uihost.UIHost`); Mount refuses nil or a registrar without the
document-script rail, because a self-rendered tag cannot declare the
boundary in the route manifest.

For hosts that are not a `uihost`, the same contract holds by hand:
render the tag only on in-scope pages, give it `data-fui-doc`, and add
the script URL to the route manifest's `docScripts` for those routes —
or mark the routes `NoSPA`, which makes every link to them a full load.

## Observing tools without seeing inputs

A health endpoint cannot prove that an in-browser agent discovered a
tool, or that a successful command reached another participant's UI.
`WithObserver` exposes the metadata the framework already has — and
nothing it must not:

<!-- gofastr:compile
stmt: _ = host
import "log"
import "github.com/DonaldMurillo/gofastr/framework/experimental/webmcp"
-->
```go
host := webmcp.New(webmcp.WithObserver(func(ev webmcp.ToolEvent) {
    log.Printf("webmcp %s: %s %s %s -> %d %q %s %s",
        ev.Phase, ev.Name, ev.Method, ev.Path,
        ev.StatusCode, ev.ErrClass, ev.Duration, ev.InvocationID)
}))
```

A `ToolEvent` carries phase (`register` for refused declarations,
`invoke` for agent-driven calls), identity (`Name`), routing
(`Method`, `Path`), outcome (`StatusCode`, `ErrClass`), cost
(`Duration`), and on invocations a correlation `InvocationID`. It
deliberately carries no input body, no headers, and no query string —
tool inputs are exactly where secrets live — and `Path` is the declared
tool path, never the request URL. `ErrClass` is a stable token:
registration failures use the validation branch (`"path"`,
`"duplicate_name"`, `"after_mount"`, `"route_conflict"`, ...),
invocations use `http_<status>` or `panic`.

Three properties worth knowing:

- **Registration failures name the tool.** A misdeclared tool is an
  event with its name, method, path, and class — success produces no
  event at all.
- **Only agent calls are invocations.** The observer wraps
  `Handle`-registered routes and reports requests carrying the marker
  header (`MarkerHeader`). The same route called by hand serves
  identically but produces no event: the marker attributes a call
  without changing behavior or granting anything.
- **Correlation is one id.** A marked request gets a random
  `InvocationID` (also the `X-Gofastr-WebMCP-Invocation` response
  header). The handler reads it with `webmcp.InvocationID(r.Context())`
  and the observer's event carries it, so an app can log its own
  delivery and acknowledgement records against the same value.

One vocabulary warning goes with this: an HTTP 200 proves the command
was *accepted* by the endpoint. It says nothing about *delivery* to
another UI, the user *seeing* it, *approval*, or the *physical*
completion of the action. The framework observes exactly one link of
that chain; every later link needs its own signal, tagged with the
invocation id.

For the browser side, `WithBridgeDebug()` on `Mount` bakes a bounded
debug state into the served script: `window.__gofastrWebMCP` reports
feature support, tools attempted/registered, names that failed to
register, and the last invocation status — never inputs, headers, or
URLs. It answers "did the browser even register the tools?" from the
page itself. Everything here is opt-in: with no observer installed,
`Handle` routes carry no instrumentation, no ids, and build no events.

## Instructions, groups, and examples

Twenty-five individually valid tool descriptions do not teach an agent
how to operate the app: inspect before mutating, ground targets before
guidance, verify delivery from backend state instead of trusting
command success. Three declaration surfaces carry that contract:

<!-- gofastr:compile
stmt: _ = err
import "encoding/json"
import "github.com/DonaldMurillo/gofastr/framework/experimental/webmcp"
var host *webmcp.Host
var schema, outSchema json.RawMessage
-->
```go
host := webmcp.New(
    webmcp.WithInstructions("Inspect before mutating. Verify delivery from backend state."),
)

scene := host.Group("scene",
    webmcp.WithDescription("Ground targets before guidance."),
    webmcp.WithPreferredFirst("inspect_scene"),
)

err := scene.Register(webmcp.Tool{
    Name:        "draw_arrow",
    Description: "Point to a precise location on a verified object.",
    InputSchema: schema,
    Examples: []webmcp.Example{{
        Input:   json.RawMessage(`{"target":"power-button"}`),
        Summary: "Point to a confirmed power button",
    }},
    OutputSchema: outSchema,
})
```

**Instructions** (`WithInstructions`) are developer-authored — never
user content; they are served to everyone allowed to fetch the
manifest. The text is preserved verbatim in the manifest
(`"instructions"`), and because the browser proposal has no
app-instructions field yet, `Mount` also serves it at
`/__gofastr/webmcp/instructions.json` through a deterministic read-only
orientation tool (`get_app_instructions`), so in-browser agents can
read it without the app hand-rolling a tool. That name is reserved
while instructions are set. The route follows the same asset policies
as the script and manifest (page scope, authorization middleware,
private caching).

**Groups** organize a large catalog without renaming anything:
`Group.Register` and `Group.Handle` tag tools with the group name, and
the manifest carries the group's description and preferred-first tool.
Grouping is pure metadata — it never changes endpoints, middleware, or
hints. `Register` refuses a tool referencing an undeclared group, and
`Mount` fails when a group's `WithPreferredFirst` names a tool that
never joined the group.

**Examples** are validated against the input schema at registration
with a minimal structural check: the input must be a JSON object,
required keys present, declared property types respected (`"integer"`
accepts whole numbers; a list of types accepts any of them). An example
that contradicts its own schema fails at startup next to the
declaration. **Output schemas** are checked to be JSON objects and
preserved, but they are documentation: responses are returned to the
agent verbatim with no runtime validation, so do not treat the schema
as a guarantee. Keep `UntrustedContentHint` on tools whose outputs
include user-controlled content, with or without an output schema.

The browser proposal's `registerTool` accepts only name, title,
description, input schema, and annotations — so groups, examples,
output schemas, and instructions cannot be forwarded to the browser
today. They ride in the manifest (`/__gofastr/webmcp/tools.json`) for
server-side agents and any tooling that reads it, and the bridge
degrades safely: it ignores the fields it cannot forward, and
registration of the tool itself is unaffected.

## Tool fields

| Field | Required | Meaning |
|---|---|---|
| `Name` | yes | Unique per page, constrained to the WebMCP spec grammar `[A-Za-z0-9_.-]{1,128}` (the browser rejects anything else asynchronously, which reads as a tool that never existed; `Register` fails loudly instead). The browser also refuses duplicate registrations, so it must not collide with tools the app registers from its own JS. |
| `Title` | no | Human-readable display name. |
| `Group` | no | Tags the tool into a group declared with `Host.Group`. Set by `Group.Register`/`Group.Handle`; `Register` fails on a group that was never declared. Metadata only: no renaming, no routing or authorization change. |
| `Description` | yes | What the tool does and when to use it. An agent cannot pick an undescribed tool, so `Register` refuses a blank one. |
| `InputSchema` | no | JSON Schema object for the input. Defaults to `{"type":"object","properties":{}}`. |
| `Examples` | no | Usage examples, validated against `InputSchema` at registration (object shape, required keys, declared property types) and preserved in the manifest. |
| `OutputSchema` | no | Documents the response shape. Descriptive only — responses are returned verbatim with no runtime validation. |
| `Method` | no | `GET`, `POST`, `PUT`, `PATCH`, or `DELETE`. Defaults to `POST`. |
| `Path` | yes | Same-origin absolute path of the implementing endpoint. Query strings allowed; schemes, hosts, fragments, backslashes, control characters, and `.`/`..` segments are rejected. |
| `ReadOnlyHint` | no | Advertises the tool as non-mutating (`annotations.readOnlyHint`). Advisory only — never a substitute for endpoint authorization. |
| `UntrustedContentHint` | no | Warns the agent the result may carry content the app does not control (`annotations.untrustedContentHint`). Advisory only — never a substitute for output sanitization. |

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

## Reference example

`examples/webmcp-remote-assist` composes this whole page end to end:
role-cookie authorization beside a document-scoped mount, one typed
command behind both the manual button and the agent tools, the
observer's invocation ids correlated with an operator acknowledgement,
and a browser test that drops the transport and proves the sequenced
channel does not resurrect cleared state. It is the proof that the
mount policies, the marker contract, and the recovery-screen guard
pattern from [ui-wiring](ui-wiring.md) compose.
