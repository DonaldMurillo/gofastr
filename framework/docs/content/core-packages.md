# Core packages

This is a map of the exported `core/*` packages that don't have their own dedicated guide. Each entry explains what the package does, whether you call it directly or through `framework`, and where to start reading (`go doc`).

Most apps touch only `handler`, `router`, `render`, and `config` directly; `framework` composes the rest.

## Request handling

### `core/handler`
- **What:** Typed request handlers (`Handler[I, O]` + `HandlerAdapter`), request binding (`Bind`), strict JSON decoding (`DecodeStrict`, `UnmarshalStrict`, `CheckTopLevelKeys`, `CheckUTF8`), structured `*Error` / `WriteError`, and context identity accessors (`SetUser`, `GetUser`, `SetTenant`, `SetRequestID`).
- **Use:** Direct. Start at `core/handler/handler.go` (`HandlerAdapter`).
- **Key contracts:**
  - `Bind` applies `header` → `query` → `path` → JSON body (1 MiB cap; returns `413` over cap), including promoted fields on embedded structs and any `encoding.TextUnmarshaler`. Empty query/header values (`?q=`) are treated as absent unless tagged `,empty` (`query:"q,empty"`).
  - Strict JSON decoding refuses duplicate keys, case-folded keys (`FoldJSONKey`), unknown struct fields, trailing data, and invalid UTF-8 with `400` and clean JSON-path messages (never leaking Go struct names). Pass `handler.UseNumber()` to preserve integers past 2^53.
  - `Respond` buffers JSON before writing headers (encoding errors return `500`, never a partial `200`). Returning `nil` or a nil pointer answers `204 No Content`; a nil slice/map answers `200 null`.

### `core/router`
- **What:** Method-based HTTP router over Go 1.22's `http.ServeMux` (`METHOD /pattern` with `{id}` captures), route groups (`Group`, `Prefix`, `Root`, `ResourceGroup` / `RouteResource`), middleware chaining (`Use` outside-in, `UseInner` innermost), `ValidatePattern`, and sanitized path params (`Param`, `SanitizePathParam`).
- **Use:** Direct via `app.Router()`. Start at `core/router/router.go` (`Router`).
- **Key contracts:** `Param` strips control chars, decoded `/`, and `..` traversal, and panics if the matched pattern declares no wildcard of that name (`handler.Bind` leaves undeclared `path` tags zero so one input struct can serve both `POST /users` and `PUT /users/{id}`). Pre-chain hooks (`SetRouteGate`, `SetServeHook`, `PreRoute`) recover panics themselves since `Recovery` middleware hasn't run yet.

## Real-time

### `core/stream`
- **What:** SSE (`SSEWriter`, `SSEBroker`), RFC 6455 WebSockets (`Upgrade`, `CheckUpgradeRequest`, `WebSocketConn`, `Hub`, `WithUpgradeHook`), `ChunkedWriter`, and sequenced-reconnect `StateChannel` (`SnapshotSource`, `CloseOnOverflow`).
- **Use:** Direct when adding custom SSE/WebSocket endpoints; `battery/rtc` builds on `StateChannel`. Start at `core/stream/sse_broker.go` (`SSEBroker`).
- **Key contracts:**
  - `WebSocketConn` validates UTF-8 on both reads (`1007`) and text writes (`Write`/`WriteString` return `stream.ErrInvalidUTF8`; use `WriteBinary` / `Hub.BroadcastBinary` for arbitrary bytes), enforces `ReadLimit` (`1009`) and `MaxControlFramesPerSecond` (`1008`), drains queued writes on `Close`, and supports `CloseWithStatus(code, reason)`. A negative `WSConfig.PongTimeout` disables pong expiry and checks activity every quarter of `ReadIdleTimeout` (10ms floor).
  - `StateChannel` hydrates reconnecting clients with a role-filtered snapshot (`SnapshotFor` + `FilterEvent`) and strictly monotonic sequences paired with `__gofastr.createSequencedReducer`.

### `core/fanout`
- **What:** Lossy, best-effort pub/sub (`Fanout` interface: `Publish`/`Subscribe`) for cross-replica real-time fan-out, with an in-process default, a library-agnostic Redis adapter (`NewRedis(RedisPubSub)`), `UnwrapForeign` self-echo filtering, and non-blocking `PublishQueue` / `PublishQueueReporting`.
- **Use:** Mostly indirect (`framework.WithFanout` wires the event bus, island manager, and `SSEBroker`). Start at `core/fanout/fanout.go` (`Fanout`).

## API surface

### `core/mcp`
- **What:** Model Context Protocol JSON-RPC 2.0 server (tools, resources, templates, prompts, gates, stdio + HTTP/SSE transports, MCP Apps).
- **Use:** Direct via `app.MCP` or standalone. See dedicated guide: [mcp.md](mcp.md). Start at `core/mcp/server.go` (`Server`).

### `core/a2a`
- **What:** Agent2Agent v1.0 task-exchange JSON-RPC server (named Go skills, SSE streaming, SSRF-guarded push webhooks, owner-scoped memory/SQL stores).
- **Use:** Direct (`NewServer`). See [a2a.md](a2a.md). Start at `core/a2a/server.go` (`NewServer`).

### `core/openapi`
- **What:** OpenAPI 3.1 builder (`Spec`, `Build`, `SetDescription`, `Nullable`) plus `Handler` (auth-gated, supports per-request `RequestView` filtering), `PublicHandler`, and script-free HTML `DocsHandler` / `DocsHandlerWith`.
- **Use:** Mostly indirect (generated from entities/routes via `App.OpenAPISpec`). Start at `core/openapi/spec.go` (`Spec`).

### `core/schema`
- **What:** Entity field definitions, value validation (including precise `json.Number`), and JSON Schema / OpenAPI property generation (`WireName`, character bounds, patterns, enums).
- **Use:** Direct in entity declarations. Start at `core/schema/schema.go` (`Field`, `Validate`, `JSONSchema`).

### `core/jcs`
- **What:** Stdlib-only RFC 8785 JSON Canonicalization Scheme (`CanonicalizeJSON` for raw untrusted bytes with strict checks, `Canonicalize` for Go values).
- **Use:** Indirect (signs A2A agent cards); direct when hashing/signing JSON. Start at `core/jcs/jcs.go` (`CanonicalizeJSON`).

## Content & rendering

### `core/render`
- **What:** Auto-escaping HTML engine (`HTML`, `Tag`, `Text`, `Raw`, `Attr` with an allow-list that drops `on*` event attributes, `Component[T]`, `Layout`).
- **Use:** Direct for all SSR markup. Start at `core/render/html.go` (`HTML`).

### `core/markdown`
- **What:** Dependency-free CommonMark-subset renderer emitting `render.HTML` with all raw HTML escaped, safe URL filtering (`core-ui/urlsafe`), frontmatter stripping, table `\|` escapes, and `ParseFenceInfo` (`Lang` + `Meta` on `data-meta`, consumed by `ui.Markdown` / `CodeBlock`).
- **Use:** Direct. Start at `core/markdown/markdown.go` (`Render`).

### `core/static`
- **What:** Hardened `fs.FS` / `embed.FS` file server (`Handler`, `Mount`): content-hash ETags (background-hashed over 16 MiB), `Range`/`206` support on seekable files, `Cache-Control`, `SPA` fallback (returns `404` for missing asset extensions instead of HTML), `NotFoundFile`, symlink escape refusal on `os.DirFS`, and cross-OS traversal/dotfile/Windows 8.3/backslash checks (`Refuses`, `RefusesIn`, `IfNoneMatch`).
- **Use:** Direct for static assets. Start at `core/static/static.go` (`Handler`).

## App plumbing

### `core/config`
- **What:** Typed struct config loader (`config.Load(&cfg)`) reading `config:`, `default:`, `required:`, `sensitive:`, and `validate:` (`min`, `max`, `oneof`) tags from `EnvSource` or `MapSource`.
- **Use:** Direct at startup. Start at `core/config/config.go` (`Load`).
- **Key contracts:** Named nested structs get `SCREAMING_SNAKE_` prefixes; embedded structs promote keys without a prefix (like `encoding/json`); `encoding.TextUnmarshaler` fields bind via `UnmarshalText`; `sensitive` values are redacted from all error strings; integer overflow fails `Load`.

### `core/netguard`
- **What:** Shared SSRF & origin checks: `IsInternal` (loopback, private, link-local, CGNAT, metadata, IPv4-mapped/NAT64/6to4/Teredo), `GuardedTransport` (ignores `HTTP_PROXY`/`HTTPS_PROXY` so dial-time checks aren't bypassed), `GuardPlainTransport`, `GuardRoundTripper`, `CheckOutboundURL`, `OriginAllowed`, and `IsLoopbackAuthority`.
- **Use:** Indirect (used by webhooks, A2A, harness, Web Bot Auth, MCP). Start at `core/netguard/netguard.go` (`IsInternal`).

### `core/textsafe`
- **What:** Text safety primitives: invisible/bidi/C0/C1/tag-character checks (`IsInvisible`, `IsUnsafe`, `StripInvisible`, `StripUnsafe`, preserving valid flag tag sequences), terminal sanitization (`ScrubTerminal`), Postgres-safe text validation (`Storable`—valid UTF-8 without NUL), and panic logging (`Recovered`, `LogPanic`—call directly as `defer textsafe.LogPanic(ctx, msg)`).
- **Use:** Mostly indirect; call `Storable` before raw SQL binds and `ScrubTerminal` before printing untrusted output. Start at `core/textsafe/textsafe.go`.

### `core/webbotauth`
- **What:** Experimental RFC 9421 + Ed25519 inbound Web Bot Auth verification and SSRF-guarded key-directory fetching.
- **Use:** Indirect via `framework.WithWebBotAuth` / `framework.VerifiedAgent` (see [web-bot-auth.md](web-bot-auth.md)). Start at `core/webbotauth/webbotauth.go` (`Verifier`).

### `core/fuzzy`
- **What:** Dependency-free `Levenshtein` edit distance for "did you mean …" suggestions.
- **Use:** Indirect. Start at `core/fuzzy/fuzzy.go` (`Levenshtein`).

## Common mistakes

- **Conflating `render.HTML` and `handler.HTML`.** `render.HTML` is the safe HTML fragment value type; `handler.HTML` is a `ResponseType` that writes `text/html` through `Respond`.
- **Treating `core/fanout` as durable.** It is lossy and best-effort—disconnected subscribers miss messages. Use `battery/queue` or the outbox for guaranteed delivery.
- **Expecting `core/markdown` to pass through raw HTML.** It escapes all HTML by design (only standalone `<!-- ... -->` comment blocks are omitted); build custom HTML with `core/render`.
- **Adding `on*` handlers via `render.Attr`.** The allow-list drops them—use `data-cui-*` attributes via `core-ui/interactive` instead.
