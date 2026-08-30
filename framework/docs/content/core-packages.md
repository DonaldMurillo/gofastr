# Core packages

This is a map of the exported `core/*` packages that have no dedicated
reference page. Each entry names what the package does, whether you reach
for it directly or through the framework, and one code anchor to start
reading. For the full surface of any package, run `go doc` on the anchor.

The `framework` package composes these; most app authors touch only a
few directly, commonly `handler`, `router`, `render`, and `config`.

## Request handling

### handler

The typed request-handler contract. You write a `Handler[I, O]` func and
wrap it with `HandlerAdapter`, which binds the request into `I` from
headers, query, path tags, and a strict-key JSON body, recovers panics, and
serializes `O`. It also owns the `*Error` shape and the context
accessors (`SetUser`, `GetUser`, `SetTenant`, `SetRequestID`) the
middleware chain populates. Author-facing: you write handlers against
this. Start at `core/handler/handler.go`: `HandlerAdapter`.

### router

A method-based HTTP router over Go 1.22's `http.ServeMux`: `METHOD
/pattern` registration with `{id}` path capture, concurrency-safe
middleware chaining after registration, route groups, and sanitized path
params (`Param` strips CR/LF/NUL and smuggled `..`). The framework owns
the root `*Router` and hands it to batteries and authors to mount
routes. Direct. Start at `core/router/router.go`: `Router`.

## Real-time

### stream

The HTTP-facing real-time primitives: SSE (`SSEWriter` for one response,
`SSEBroker` for multi-subscriber fan-out), a minimal WebSocket client
(`Upgrade`, `WebSocketConn`, `Hub`), and `ChunkedWriter`. Direct when you
add an SSE or WebSocket endpoint; the framework's island-push and event
bus layer on `SSEBroker`. Start at `core/stream/sse_broker.go`:
`SSEBroker`.

### fanout

The lossy, best-effort cross-replica transport behind real-time
delivery. A `Fanout` interface (`Publish` / `Subscribe`) with an
in-process backend and a Redis adapter (`NewRedis`), plus a node-id
envelope that stops broadcasts from re-broadcasting. Indirect by
default: the framework wires it under the event bus, the island
manager, and `SSEBroker`. Reach for it directly only to span a new
real-time surface across replicas. Start at `core/fanout/fanout.go`:
`Fanout`.

## API surface

### mcp

A Model Context Protocol server: a JSON-RPC 2.0 tool and resource
registry (`RegisterTool`, `RegisterResource`, `RegisterApp`), stdio and
HTTP/SSE transports, and per-caller gates (`WithToolGate`). Mostly
direct: you call `RegisterTool` on the framework-provided `*Server` to
expose tools; the framework owns the `/mcp` transport and lifecycle.
Start at `core/mcp/server.go`: `Server`.

### openapi

An OpenAPI 3.1 builder plus serving handlers. A `Spec` accumulates
paths, schemas, and security schemes and `Build()` emits the doc;
`Handler` serves it (auth-gated by default) and `DocsHandler` serves a
landing page whose `public` flag gates both the page and its nested
spec route (the framework passes its `PublicOpenAPI` setting, so a
public spec gets a public browse page). Mostly indirect: the framework generates the
spec from your routes and entities. Reach for it directly to add custom
operations or mount the docs endpoint. Start at `core/openapi/spec.go`:
`Spec`.

### jcs

RFC 8785 JSON Canonicalization Scheme (JCS), stdlib-only: the invariant
byte form of JSON for signing and hashing. `CanonicalizeJSON` parses raw
bytes strictly (duplicate keys, unpaired surrogate escapes, invalid
UTF-8, and Infinity-overflowing literals are errors) and `Canonicalize`
handles in-memory Go values. Keys sort by UTF-16 code unit and numbers
serialize exactly as ECMAScript `Number::toString`. Indirect: the
framework signs A2A agent cards with it. Direct when you need to hash or
sign JSON in your own code — read the package doc for the I-JSON number
caveats first. Start at `core/jcs/jcs.go`: `CanonicalizeJSON`.

## Content and rendering

### render

The type-safe, auto-escaping HTML engine the whole UI is built on. The
`HTML` value type plus builders (`Tag`, `Text`, `Raw`, `Attr` with a
strict attribute allow-list that drops `on*` handlers), and
`Component[T]` / `Layout` registries. Direct: this is the
HTML-construction API for pages and islands. Start at
`core/render/html.go`: `HTML`.

### markdown

A small, dependency-free Markdown renderer. It parses a constrained
subset (headings, fenced code, lists, tables, blockquotes, frontmatter;
inline bold/italic/code/links/images) and emits `render.HTML` with all
source HTML escaped, no raw-HTML passthrough. Direct; no framework
auto-wiring. Start at `core/markdown/markdown.go`: `Render`.

### static

A hardened file server for `embed.FS` / `fs.FS`: ETag caching,
configurable `Cache-Control`, MIME detection, SPA fallback, and
traversal / dotfile / forbidden-config rejection. Direct: call `Mount`
(or `Handler`) to serve embedded CSS/JS/images; the framework uses it
for its own assets too. Start at `core/static/static.go`: `Handler`.

## App plumbing

### config

A typed config loader. `Load` reflects `config:` / `default:` /
`required:` / `sensitive:` / `validate:` struct tags and binds from
pluggable `Source`s (default `EnvSource`; `MapSource` for tests),
recursing into nested structs with a `SCREAMING_SNAKE` prefix and
redacting `sensitive` values from errors. Direct: call
`config.Load(&cfg)` at startup. Start at `core/config/config.go`:
`Load`.

### netguard

The single "is this IP internal" predicate (loopback, private,
link-local, CGNAT, cloud-metadata), normalizing IPv4-mapped IPv6 first,
with a `Reason` helper. Tiny: two exported funcs. Indirect:
outbound-fetch surfaces (webhooks, the harness) enforce it at dial time;
app authors do not call it. Start at `core/netguard/netguard.go`:
`IsInternal`.

### webbotauth

Inbound Web Bot Auth verification: RFC 9421 signature-base assembly,
Ed25519 verification under the Web Bot Auth draft profile, and the
SSRF-hardened agent key-directory fetcher. Experimental and
draft-tracked (see `web-bot-auth.md`). Indirect: hosts turn it on with
`framework.WithWebBotAuth(framework.WebBotAuthConfig{Verify: …})` and read
`framework.VerifiedAgent`; reach for the package directly only to
verify outside a GoFastr app. Start at `core/webbotauth/webbotauth.go`:
`Verifier`.

### fuzzy

Shared string-similarity helpers, currently `Levenshtein` edit distance
(bytewise, two rolling rows). Dependency-free so both the framework and
the CLI import it. Indirect; used for "did you mean …" suggestions. Start
at `core/fuzzy/fuzzy.go`: `Levenshtein`.

## Common mistakes

- **Conflating the two `HTML` symbols.** `render.HTML` is the safe, auto-escaped HTML fragment value type; `handler.HTML` is a `ResponseType` that writes `text/html` through `Respond`. They share a name and little else. Check which one a function returns before passing it on.
- **Treating `fanout` as durable.** It is lossy and best-effort: a subscriber that is not connected when a message is published misses it. Use it for real-time fan-out, not for work that must be delivered.
- **Expecting `markdown` to pass through raw HTML.** It escapes all source HTML by design. If you need embedded HTML, build it with `render` directly rather than smuggling it through markdown.
- **Adding `on*` handlers via `render.Attr`.** The attribute allow-list drops them; use a real event binding (`data-fui-*` via `core-ui/interactive`) instead.
