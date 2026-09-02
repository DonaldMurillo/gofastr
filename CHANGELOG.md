# Changelog

All notable changes to GoFastr. Follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) with semver-ish
calendar versions (`YYYY-MM-DD` per substantive release until the API
stabilises). Breaking changes are clearly marked with **BREAKING**.

## [Unreleased]

### Security
- **OIDC discovery endpoints are transport-validated and fetches never
  follow redirects** (hardening): every endpoint the discovery document
  names (`token_endpoint`, `jwks_uri`, `userinfo_endpoint`,
  `authorization_endpoint`) must be `https://` — or plain `http://` on a
  literal-loopback host under a literal-loopback `http://` issuer — and
  must not carry userinfo; distinct hosts stay allowed. The provider's
  HTTP client now answers redirects as final responses instead of
  following them, so a token endpoint that answers 307/308 can no longer
  have the exchange POST body (`client_secret`, code) re-sent verbatim to
  the redirect target (the forwarded-body mechanics are pinned red-first
  by `TestOIDCSec_TokenRedirectKeepsSecret`). Mirrors the redirects-off
  posture of A2A push and webhook egress; classified hardening, not an
  attacker-reachable bug, since the endpoints come from the dev-anchored
  issuer's own document.
- **Auth decodes credentials strictly**: a login or register body that
  names `email` or `password` twice, or through a case-folded key, is a
  400 on both the JSON and the form surface. `net/url` kept the first
  duplicate and `encoding/json` the last, so one smuggled body
  authenticated a different identity depending on Content-Type.
- **The relay strips `Refresh` and `Clear-Site-Data` from vendor
  responses and drops `Link` headers with absolute targets**: a
  header-driven navigation to the vendor origin, a wipe of the visitor's
  app-origin storage, and a direct preload connection to the vendor all
  defeated the first-party posture the `Location` refusal exists for.
- **Kiln**: the world-disclosing GET routes refuse cross-site and
  DNS-rebound browser subscribers the way the POST family does; `ALTER
  TABLE` and `PRAGMA` identifiers are quoted, so a hostile column name can
  no longer run a second statement through the multi-statement SQLite
  driver; journal-derived strings with control characters are omitted
  from the agent's system prompt instead of rewriting it; graduation and
  the preview manifest refuse or coerce off-origin PWA `start_url` and
  `scope` values.
- **Contracts**: `Report.Apply` (and so `verify --fix` and the MCP fix
  tool) refuses a symlink that resolves outside the analysed root; a rule
  reference spelled with a Unicode fold-equivalent (`gofaſtr1003`) is an
  unknown rule, never a live suppression; the generated-source exemption
  needs the full `// Code generated … DO NOT EDIT.` line; report text
  strips control bytes; suggestion ranking caps its needle.
- **`framework.InjectTenantID` fails closed**: with no tenant in the
  context it deletes a body-supplied `tenant_id` instead of letting the
  caller name the row's tenant.
- **Host head and discovery URLs**: `WithFavicon` and `WithPreconnect`
  gate their URLs like every other head emitter (`javascript:`, `data:`,
  `file:`, `blob:` and protocol-relative values are dropped); a
  `preload`, `modulepreload` or `prefetch` rel in caller head HTML is
  detected on the parsed token list, not a literal spelling;
  `X-Forwarded-Proto` is honoured only as `http` or `https`; the
  alternate `Link` path strips control bytes.
- **A panicking handler or gate no longer kills a stdio process**:
  moduleproto recovers a handler panic into a paired internal error, and
  MCP evaluates listing gates outside the registry lock and fails closed
  on a panicking gate everywhere, including `tools/call`.
- **`netguard` classifies 0.0.0.0/8 and 240.0.0.0/4 as internal**, closing
  the `http://0/` SSRF bypass and the limited-broadcast address.
- **Generators refuse identifier injection**: a blueprint hook handler,
  a CLI field name, and an SDK entity or table that cannot survive the
  identifier or string-literal position they are emitted into are
  refused at spec build, and derived names that collide (`events` /
  `Events`, `blog_posts` / `blog-posts`) are refused instead of emitting
  a second definition.
- **Harness**: a WebSocket command body naming a session other than the
  socket's own is refused; the cost tracker's running total is monotone
  against a negative usage report; a PEM private key is redacted header
  through footer; tool progress and argument summaries are sanitized
  before they reach the terminal.
- **Browser runtime**: every DOM-borne value that reaches a selector is
  `CSS.escape`d, widget registry reads are own-property gated so
  `constructor` cannot resolve through the prototype chain, the kiln tool
  POST path is gated to an identifier, and the sortable list's conflict
  refresh mounts a response body only on a 2xx.
- **Eval reports escape candidate text**: RESULTS.md and leaderboard.md
  render evidence through HTML entities with newlines flattened, the CLI
  shim writes one log line per invocation, and workspace fingerprints
  hash through symlinks.

### Changed
- **BREAKING: process-module schema and role names for names containing
  `-`, `_` or an upper-case letter carry a digest suffix**
  (`module_billing_1_<12 hex>`): distinct operator-approved modules used
  to sanitize onto one schema behind one role, and the `REVOKE` fence
  then bounded each to the other's objects. Pure lowercase-alphanumeric
  names are unchanged. Re-provisioning a module with such a name creates
  a new schema; migrate its data before upgrading.
- **Custom endpoint tool names keep hyphens and mark path parameters**
  (`posts_get_feed-items`, `posts_get_items_-id-`), and entity and path
  spelling is no longer case-folded, so distinct routes keep distinct tool
  and operation ids. Names for endpoints without hyphens, parameters or
  upper case are unchanged.
- `Operation.AddParameter` keeps the first declaration of a `(name, in)`
  pair; the entity list operation can no longer declare `sort`, `page`,
  `cursor`, `q` or `trashed` twice when a field shares the name.
- `component.NewWidget` slugifies its ID onto `[A-Za-z0-9_-]`, the
  alphabet the browser's behavior-script gate honours; `island.Manager.
  Subscribe` returns a closed channel on a cap-refused connect instead
  of one that never delivers; `interactive.SetSignal` and friends, and
  a store slice declaration, panic on the reserved names `__proto__`,
  `constructor` and `prototype` the kernel refuses every write to.
- The fanout envelope carries a non-UTF-8 body base64-encoded in a new
  optional `x` field; UTF-8 bodies are byte-identical on the wire.
- Kiln's runtime database is derived from the journal: boot,
  `reset_session` and `undo` rebuild it, an approved `delete_entity`
  drops the table, a seed's rows are inserted before its entry is
  durable, and a crash-torn last journal line is healed at open.

### Fixed
- **Blueprint hooks reach the generated app**: a stub per handler in
  `stubs.go` and a `HookRegistry` registration in `main.go`; the hooks
  section used to decode and emit nothing. Blueprint validation also
  refuses `.json` duplicate keys, non-scalar `db.driver`/`db.url`,
  non-integer seed `count`/`weights`, nav icons carrying markup (icons
  are now emitted through `ui.Icon`, so a registered name renders its
  SVG and an unregistered one renders nothing), duplicate seed blocks, unknown seed row keys, forward
  seed references, and count seeds over required relations; complex seed
  values are emitted as Go literals instead of `nil`. A generated app
  carrying a hook is now compiled by the test suite, which caught the
  stub's own compile error.
- **CRUD**: a single-field cursor naming a foreign column is refused;
  repeating a relation in `?include=` with two filter sets unions the
  rows instead of AND-ing an impossible predicate; the in-process nested
  filter honours the IN cap; the streaming list survives `limit` 0; an
  upsert with a caller-supplied auto-increment key updates that row
  instead of inserting a duplicate; the MCP `_list` tool forwards array
  filter values one per element; `Define` refuses a cursor field that
  names no declared column.
- **OpenAPI and SDK docs**: documented endpoint paths drop dot segments,
  a versioned entity's group prefix replaces the API prefix instead of
  nesting under it (`/api/v1/posts`, never `/api/api/v1/posts`),
  reference pages address versioned entities at their real base path and
  render one section per registered version; `sdk.PackZip` refuses a
  prefix that escapes the extraction directory.
- **UI**: `NumberInput` emits each bound only when declared, `TagInput`
  caps server-rendered values at `MaxLength`, a `Section`'s heading id
  follows its explicit `ID`, filter pill ids get an ordinal suffix only
  when two options fold to one slug, `FontFaceCSS` neutralizes
  declaration breakers in config values, and `Router.Resolve` never
  binds an empty path segment to a parameter.
- **Batteries**: access-log entries percent-encode control bytes and
  `log_filter` errors on a timestamp of the wrong JSON type; Redis
  `Reclaim` quarantines a corrupt processing entry to the dead-letter
  list instead of deleting the job's only copy; a transient subscriber
  lookup error keeps a webhook delivery retryable and a stale
  non-terminal settle cannot regress a delivered or dead row; the hybrid
  keyword leg caps query terms at 64.
- **Core**: the YAML parser unquotes mapping keys so the duplicate guard
  sees `"title"` and `title` as one key, and refuses anchor, alias, tag
  and flow decorations on keys; `ValidateMIME` compares base media types
  so a `text/plain` allowlist matches the sniffed `text/plain;
  charset=utf-8`; `Force(v, true)` refreshes a drifted checksum; a2a
  stores a push config before its task so a refused send leaves no
  orphan, keys its memory store by struct so a NUL in an owner cannot
  leak another's rows, and reports backend errors generically; the
  idempotency shard is length-prefixed and injective and its failure
  log scrubs the key; span names and attributes scrub control bytes;
  `Int` refuses a `uint` above `MaxInt64` instead of wrapping negative;
  duplicate frontmatter keys resolve first-wins; the pack round trip
  keeps quoted keys.
- **Framework**: `AddQueue`'s stop is bounded by the drain deadline
  instead of hanging SIGTERM behind a job handler that ignores its
  context.
- **Kiln**: replay refuses `add_page` with an empty path and
  `update_page_element` with a null page; graduation refuses seeds the
  live runtime would refuse and names a handler stub even for an id with
  no identifier characters; `add_seed` for an unknown entity is a
  validation error at ingestion; `abs(MinInt64)` saturates.
- **Analyzers**: `mapwriter` resolves sinks reached without selector
  syntax (bound method values, bound package functions, dot imports,
  `WriteString`-only types), map-ordered range sources hidden behind a
  variable or `slices.Collect`, and binds the `len(m) == 1` exemption by
  variable identity rather than spelling. `stability.Classify` matches
  manifest prefixes on a path-segment boundary.
- **Harness TUI, contracts and the CSP linter**: the inline-style gate
  also catches `/`-separated attributes and unquoted values.

## [0.80.0] - 2026-09-02

### Fixed
- **The sandbox conformance-probe runner now enforces its own wall budget
  and reads its child's result correctly** (#136 audit probes). A child
  killed mid-attempt used to be filed as "unreachable" for every probe; the
  runner's own stdout-protocol comment says a kill is not a clean denial,
  so the denial probes (P1–P5, P7) now report FAIL and only the
  resource-limit probe (P6) stays unreachable. A sandbox wrapper's stderr
  line ahead of the child's `PASS` (sandbox-exec warns before it execs)
  no longer turns every probe into "unrecognized output": the parser
  finds the sentinel line rather than trusting the buffer's first line.
  And `cmd.WaitDelay` is set, so a descendant that left the process group
  and held the output pipe can no longer keep `cmd.Wait` blocked past
  `probeTimeout` — proven with a hostile backend whose grandchild calls
  `setsid`.
- **`core-ui/check`: the inline-style linter treats `"Style"` like
  `"style"`** (HTML attribute names are case-insensitive), and the no-var
  JS lint no longer flags a regex literal such as `/var\s+\w+/` as a
  declaration: the sanitizer blanks regex literals, telling a pattern from
  division by what precedes the `/`.
- **`core/a2a` review fixes**: an artifact-update event with `append`
  set now carries only the new parts (a receiver appends the event's
  parts, so the merged artifact would have doubled them); a non-streaming
  `SendMessage` no longer pins its goroutine for the whole task timeout
  when the client hangs up (the run continues, the wait does not); push
  delivery and the registration-time DNS check carry their own deadlines,
  so a caller-supplied client without a timeout or a stalling resolver
  cannot hold a goroutine or a request open; every store call the run
  makes is bounded.

### Added
- **Route matches on the request context, and status-aware recovery
  screens** (#379): `host.RouteMatchMiddleware()` stores an immutable
  `app.Match` (screen id, path, parameters from the authoritative screen
  router, trailing slash tolerant) on the request, so middleware guarding
  `/session/:sessionId` reads `match.Param("sessionId")` instead of
  re-parsing the path; the host populates the same match on the render
  context, so a screen Policy sees it with no middleware at all.
  `host.RenderScreen(w, r, screen, uihost.ScreenResponse{Status: 410})`
  answers a guard with a real screen at the status the caller names:
  full chrome on a full load, bare body on a client-side navigation, the
  same status and `Cache-Control: private, no-store` on both arms, no
  session minted. A path no screen matches carries no match, so the
  `WithNotFoundScreen` 404 stays truthful. ui-wiring.md carries the
  guard recipe and the status table (authentication failure, resource
  gone, unknown route); security.md says why the no-store default is
  not optional.
- **Trusted host-page workers with their own narrow CSP** (#380):
  `AssetServer.AddBytes` takes options. `WithWorkerCSP` marks a worker
  the app compiles in (an OpenCV or ONNX depth worker) and names the
  relaxation its own response carries, a fixed skeleton plus tokens
  matched byte-for-byte against a closed allowlist (`'unsafe-eval'`,
  `'wasm-unsafe-eval'`, connect `'self'`); `WithCache` picks one of four
  cache postures by enum. The host document's policy is untouched, and
  asserted byte-identical with a worker registered; invalid tokens, a
  worker profile on a framed asset, and an unknown cache profile panic
  at registration, and the assembler re-filters at serve time. Chrome
  proves the worker evals and compiles wasm under the profile and
  refuses both without it. plugin-platform.md carries the worker-versus-
  frame guidance and the same-origin pinning recipe; there is no proxy.
- **WebMCP: one call binds a tool and its route** (#378): `Host.Handle`
  declares the tool and registers the handler at its method and path in
  one call, so the manifest cannot advertise a path the router does not
  serve and a conditional tool appears in, and disappears from, both
  together. `WithHTTPMiddleware` puts the authorization decision beside
  the declaration it protects; a method and path already on the router
  fail before anything is registered. Declaration-only `Register` is
  unchanged.
- **WebMCP: scoped and authorized mounts** (#371): `Mount` takes options.
  `WithAssetAuthorization` wraps the framework-owned bridge script and
  manifest routes with middleware, `WithPageScope` gates them on request
  identity (an out-of-scope fetch gets an empty script or an empty tool
  set), and `WithPrivateAssets` serves them under
  `Cache-Control: private, no-store`; any of the three forces that
  policy, so a credential-gated bridge is never shared-cacheable. The
  authorization middleware runs outside the scope gate, so an anonymous
  request fails with the middleware's status. The zero-option mount is
  pinned byte-identical to before.
- **WebMCP: metadata-safe observability** (#374): `webmcp.New(WithObserver(fn))`
  reports refused declarations and agent-driven invocations of
  `Handle`-registered routes as `ToolEvent`s carrying name, method, the
  declared path, status, duration, and an error class, and never a body,
  header, or query string (a planted secret never reaches the observer).
  Only requests carrying the marker header are invocations, so a manual
  call and an agent call to the same handler stay distinguishable. A
  random invocation id rides the request context, the
  `X-Gofastr-WebMCP-Invocation` response header, and the event, so an
  app can correlate a command with its own delivery records.
  `WithBridgeDebug()` on `Mount` bakes a bounded, opt-in debug state into
  the served script. Everything is off by default.
- **WebMCP: instructions, groups, examples, output schemas** (#373):
  `WithInstructions` carries the cross-tool contract in the manifest and,
  because the browser proposal has no field for it, serves it through a
  deterministic read-only `get_app_instructions` tool. `Host.Group` tags
  tools with a description and a preferred-first tool without renaming
  them or touching routing or authorization. `Tool.Examples` are checked
  against the input schema at registration (object shape, required keys,
  declared property types) and `Tool.OutputSchema` is preserved as
  documentation with no runtime validation. The bridge degrades safely:
  it forwards what `registerTool` accepts and the manifest keeps the
  rest. The generated orientation tool carries the empty-object input
  schema `Register` would have defaulted: it joins the manifest without
  passing through `Register`, and Chromium's `registerTool` refuses a
  null `inputSchema`, so the first real-browser mount found it absent
  from `getTools()`; the package's Chromium test now registers and
  executes it.
- **`ui.Menu` takes a caller-owned trigger** (#369): `MenuConfig.TriggerElement`
  is the inline HTML of your own `<button>` (or `<a>`), rendered in a
  presentation wrapper beside a summary-less `<details>` that holds the
  panel, so a host-styled trigger no longer nests inside `<summary>`
  (axe `nested-interactive`, SERIOUS). The runtime makes the element the
  disclosure controller: `aria-haspopup`, `aria-controls` and
  `aria-expanded` wired at hydration, click, Enter and Space toggle with
  activation prevented, focus on the first menuitem on open, Escape one
  level at a time with focus returned to the element, Tab from the
  element closes the chain. The summary path is byte-identical to before.
  New runtime attribute `data-fui-menu-trigger`, documented in
  core-ui/ARCHITECTURE.md and runtime-contract.md.
- **Document-lifetime scripts, and a capability boundary the SPA router
  honours** (#372): `uihost.RegisterDocumentScript(src, scope)` puts a
  script on the rail with a document lifetime: the tag ships only on
  pages the scope accepts, marked `data-fui-doc`, and the route manifest
  carries each route's set as `docScripts`. The runtime compares the
  destination's set against the live document's tags at every
  soft-navigation entry point (the click hijack before `preventDefault`,
  `navigate()`, `popstate`, and the redirect leg) and performs a real
  document load across an edge, in either direction; same-set routes
  keep swapping at their deepest shared layer, and Back/Forward across
  an edge loads the destination fresh. The reason is a browser fact, not
  a policy: a script that installs capabilities into the document (the
  WebMCP bridge registering tools on `navigator.modelContext`) is not
  uninstalled by removing its tag, and a partial swap never runs a body
  script. `webmcp.WithDocumentScope(pred)` is the first consumer; a
  browser test proves `getTools()` is empty after leaving the scope and
  populated again after Back. The core runtime budget line moved by the
  gate's measured cost under the budget file's documented exception.
- **`stream.StateChannel`: sequenced snapshots and events above `Hub`**
  (#375): a reconnecting client is hydrated from one immutable
  `SnapshotFor` read, then receives events with strictly increasing
  sequences reconciled to every snapshot the channel sent, so a snapshot
  captured before a mutation cannot resurrect the state that mutation
  replaced. `FilterEvent` shapes each event per role before
  serialization, which is where data minimization has to happen: a field
  the source strips never crosses the transport, and hiding it in the UI
  is not minimization. Delivery is best-effort like `Hub` (a slow
  connection drops events; one that cannot accept its snapshot is closed
  to reconnect). `Hub` and the WebSocket API are unchanged and pinned.
- **Reconnect generations in the browser** (#377): the new `ws` runtime
  module, loaded on demand with no DOM marker, provides
  `__gofastr.createSequencedReducer` (applies only `sequence` greater
  than the last applied) and `__gofastr.connectWebSocket`, which gives
  every reconnect a distinct generation with idempotent
  `onGenerationStart`, `onHydrated`, and `onGenerationEnd` hooks and a
  bounded reason class (`closed`, `error`, `stop`), never a raw close
  reason, payload, or credential. Transport connected, state hydrated,
  protocol resynchronized, and application ready are distinct phases;
  the docs say plainly that a recovered socket proves nothing about a
  WebRTC or media protocol layered on it. `WSConfig.ConnectionID` and
  `WebSocketConn.ConnectionID` (random when unset) correlate a client's
  reconnects in server logs.
- **`examples/webmcp-remote-assist`: authenticated WebMCP plus WebRTC
  remote support** (#376): one binary, one origin, two roles. A shared
  sign-in key mints an HttpOnly support cookie; a one-time join link
  trades for an operator cookie scoped to `/session`. The WebMCP bridge
  ships only on signed-in support documents (`WithDocumentScope`, with
  the manifest and script behind the same role check as the tool
  endpoints), so leaving the console is a real navigation and the next
  document carries no tools. The console's manual button and the
  `send_instruction` / `clear_instruction` tools decode into one typed
  command; `inspect_session` reads backend state so an agent verifies
  delivery from the operator's acknowledgement, correlated with the
  observer's invocation id, which the operator's copy of every event
  never carries. Each session is a `stream.StateChannel`; every
  relayed WebRTC signaling frame advances the session version too, so
  a reconnect snapshot is never older than the events a page applied.
  The camera is video-only and peer-to-peer: the server relays SDP and
  ICE JSON, and the app's `Permissions-Policy` opens `camera=(self)`
  while keeping the framework's `microphone=()`. Cross-site mutations
  are refused the way `battery/auth` does it: `Sec-Fetch-Site` first,
  a null `Origin` (a top-level same-origin form post) allowed. A
  two-tab Chromium test with a fake camera covers discovery, the
  shared command path, the acknowledgement, two server-side socket
  drops (one with a mutation landing while the operator is offline,
  which only the reconnect snapshot can deliver), and the hard
  navigation out.
- Probe tests kept from the #136 audit as regression pins: the pack
  encode/decode round-trip property over a randomized hostile corpus, the
  Levenshtein scaling benchmark, `Timeout`'s deterministic boundary and an
  env-gated done-vs-timer race probe, and two refutations against the SQL
  idempotency store through the full default middleware chain.

## [0.79.0] - 2026-09-01

### Added
- **A2A v1.0 task exchange** (#289): `core/a2a` serves the protocol's
  JSON-RPC binding behind the signed agent card — `SendMessage`,
  `SendStreamingMessage`, `GetTask`, `ListTasks`, `CancelTask`,
  `SubscribeToTask`, the four push-notification-config operations, and
  `GetExtendedAgentCard`. `framework.WithA2A` mounts it on the app router
  (default `/a2a`), so session and bearer auth, owner context, recovery,
  and request logging apply exactly as they do to `/mcp`; `RoleAgent`
  forwards it. A GoFastr app is a deterministic agent: a skill is invoked
  by name (`message.metadata.skill`, or a data part carrying `"skill"`),
  never inferred from prose, and every entity with MCP tools contributes
  a skill whose data part names an operation and arguments and dispatches
  through the same tool the MCP surface runs, so owner scoping and the
  tool call gate hold on both. Tasks are rows (SQLite or Postgres, owner
  column in every predicate; a foreign task id answers `-32001` exactly
  like a missing one), so any replica serves any request and a subscribe
  on a replica that is not running the task polls the store instead.
  Streaming answers `text/event-stream` with one JSON-RPC response per
  event and closes on a terminal or interrupted state. Push notifications
  POST a `StreamResponse` with the `A2A-Notification-Token` header, never
  follow a redirect, refuse internal hosts at validation and again at
  dial time (`core/netguard`), and are not retried. `AgentCardConfig.A2AEndpoint`
  advertises the exchange as the card's JSONRPC interface and
  `App.A2ASkills()` is the one skill list the card and the server share.
  The wire form — PascalCase method names, camelCase fields,
  `TASK_STATE_*` and `ROLE_*` enum spellings, the flat `Part` discriminator,
  millisecond RFC 3339 timestamps — is pinned by tests against the
  canonical `a2a.proto` and the A2A project's Go SDK rather than recalled;
  the v0.x slash-form methods are wire-incompatible and deliberately
  absent. `framework/docs/content/a2a.md` is the contract.
- **Per-agent rate limits for verified Web Bot Auth traffic** (#290):
  `webbotauth.RateLimitKey` and `framework.AgentRateLimitKey` key a
  `framework/ratelimit` limiter by the verified agent's directory URL and
  fall back to the client IP otherwise, so a signed crawler has its own
  budget instead of sharing its egress address's, and an unverified
  caller never gains from the rule. The key is the directory, not the
  key id: rotating a key does not reset the window. The generators and
  `battery/webhook` still do not sign outbound requests, for the reason
  the ticket recorded: the draft is moving, and signing from every
  generated artifact would turn each revision into a breaking change for
  downstream apps.
- **`GOFASTR1808` `rendering/fallback-drift`** (#365): design-system CSS
  that writes `var(--spacing-md, 12px)` where the theme declares
  `--spacing-md` as `8px`. A themed page never renders the fallback, so
  the number is only ever read by the next author — and about 450 such
  fallbacks had been teaching a 4/8/16/24/32 spacing ladder the theme does
  not declare, until a review bot read one as the token's value. The rule
  judges the length and time scales (spacing, radii, text, duration),
  comparing rem to px by size, and leaves colour and font fallbacks
  alone, where `currentColor`, `inherit`, and a dark-surface hex are
  deliberate. Every design-system fallback now restates its token: 452
  sites in 75 files, and all 24 Meridian screenshots (six surfaces, two
  viewports, light and dark) are byte-identical before and after. The
  catalog is now 53 rules.
- **Browser e2e for the MCP Apps widget client** (#291): a host shim plays
  the chat host's half of the ext-apps 2026-01-26 protocol in a real
  browser — reads the `ui://` widget over `resources/read`, embeds it in
  a sandboxed opaque-origin iframe, answers `ui/initialize` with a dark
  host context, forwards the widget's `tools/call` to `/mcp`, and records
  the `ui/message` the widget sends back carrying the tool result and the
  applied theme. Two failure shapes nothing could see before are pinned
  by watching initialize never arrive: the client served without its
  `Cross-Origin-Resource-Policy` relaxation, and a one-character typo in
  the client script URL. It runs in the `chromium-ui` CI shard. It proves
  a spec-faithful host and this client agree on the wire; it does not
  prove interop with Claude or ChatGPT, which no test can.
- **`ui.MenuItem.Action` — form-POST menu rows.** For command rows that hit
  PRG endpoints (stop impersonating, sign out with server-side sessions)
  where a plain `Href` would widen a state-changing endpoint to GET. The
  row renders as a submit button inside `<form method action>` with
  `Fields` as hidden inputs, written in sorted order. CSRF contract: the
  framework mints nothing — but an Action with no `Fields` panics unless
  `Unsafe: true` acknowledges the endpoint protects itself, so a forgotten
  token fails at render time instead of shipping. Mutually exclusive with
  `Href`, `RPC`, `Radio`, and `Children`; incoherent combos panic.

### Fixed
- **A menu whose trigger is a real `<button>` opens under a real click.**
  Chrome does not run the summary's UA activation when the click target is
  an interactive descendant, so a `TriggerHTML` button opened nothing. The
  disclosure module now toggles for interactive descendants of a
  `data-fui-disclosure` summary (preventDefault stops engines that would
  double-toggle; plain `<details>` stays native), pinned by a
  real-pointer chromedp test. The panel's closed state is also hidden
  explicitly in CSS: the author `display:grid` was overriding the UA
  sheet's closed-details hiding, so the panel rendered while `open` was
  false.

- **Combobox options that are plain anchors navigate instead of dying.**
  `pickOption`'s preventDefault swallowed server-built link options
  (filters, sorters) that carry `href` without `data-fui-push-state`. The
  destination now falls back to the anchor's href and rides the same
  origin-gated SPA navigate path as push-state options.

- **`App.EntityCRUDMounted` is back.** The #358 rework that introduced the
  richer internal mount predicate deleted the exported wrapper while three
  shipped references still told callers to use it — `sdk.md`'s
  `CRUDMounted: fwApp.EntityCRUDMounted` example, the
  `sdkdocs.Config.CRUDMounted` field comment, and repolint's
  `crud-exposure-rederived` finding message — so the documented wiring did
  not compile in v0.78.0, and hosts had no exported way to hand sdkdocs
  the mount truth at all. Restored as a delegate to the mount predicate
  (a read-only view counts as mounted; its read routes are what a docs
  surface documents), with a test pinning the symbol and its answers.
  Found while confirming an old worktree's uncommitted duplicate of the
  original #266 fix was safe to discard.


## [0.78.0] - 2026-09-01

### Added
- **`GOFASTR1807`: CSS that hardcodes a value the theme already declares
  as a token.** Hard rule 7 names this as a tripwire — "setting a CSS
  property where a `var(--*)` token belongs" — and nothing caught it. The
  rule inverts the theme's token table into a value→token index and flags a
  declaration, in either the stylesheet-string or builder `"prop", "value"`
  shape, whose value is exactly a token's. Scoped to design-system files,
  since `rendering/bespoke-css` already owns apps and generators.
  Coincidence is filtered by distinctiveness: a value must carry a digit,
  hex, quote, comma or paren, which drops `box-shadow: none` matching the
  `shadow-none` token. Twenty-three real findings across `framework/ui`,
  `core-ui` and `battery/admin` are fixed rather than exempted, most via
  `var(--token, fallback)` so rendering is unchanged until a theme moves the
  token. The catalog is now 52 rules.
- **Six repo analyzers**, mined from this repo's own bug history and run by
  `make analyze`, the pre-commit hook, and CI's vet step. `unboundedbody`
  flags an inbound `*http.Request` body read or decoded with no size cap;
  `errleak` flags internal error text on a 5xx; `fieldtypeswitch` reads the
  `schema.FieldType` constant set at analysis time, so a new field type
  breaks every switch that ignores it; `reqparamlimit` flags a
  request-sourced integer reaching a limit-shaped parameter unclamped;
  `discardmutator` flags a security-state mutation whose error is discarded
  where the handler acknowledges success; `hygiene` holds empty error
  branches and timeout-less client calls at zero. Type information is what
  earns these the vet lane rather than the contracts pattern lane:
  `unboundedbody` tells `*http.Request` from `*http.Response`, which grep
  cannot — 28 candidates, 19 of them outbound.

  `fmtformat` (URL-encoder output becoming a `fmt` format) and `testgap`
  (validator enumeration arms no fixture exercises) ship written and
  fixture-tested but deliberately unregistered, with the measurements that
  justify holding them back recorded in `cmd/vettool/main.go`.
  `TestEveryAnalyzerIsWiredOrExplained` fails on an analyzer that is
  neither: the four analyzers added here sat unwired and unexplained until
  it existed, passing their own fixtures while never running over the repo.

- **`cmd/repolint` gained `crud-exposure-rederived`**, which flags code
  reading `Exposure.CRUD` to decide whether an entity's routes exist. That
  flag alone misses the no-DB case, where `App` mounts no CRUD while every
  entity still reads `auto`; the mounted predicate is
  `framework.App.EntityCRUDMounted`.

- **`queue.Job.UserID`**, persisted as `queue_jobs.user_id`, names the person
  a job's payload is about. It is what makes the row reachable by
  `App.EraseUserData`: `queue_jobs` lives outside the entity registry, so the
  erase plane can only find a row through a declared column. Set it whenever
  the payload is personal data; leave it empty for infrastructure work.
  Rows enqueued before the column existed carry `''`, which is the same answer
  as a job whose payload was never personal. Only `DBQueue` persists it.

- **`queue.CompareAndDeleter`**, an optional `RedisClient` capability
  (`HDelIfEqual`), lets `Ack`/`Nack` retire a processing entry atomically.
  A one-line Lua script on go-redis; a client without it falls back to a late
  re-read, which narrows the window rather than closing it. See
  [queue](queue.md).

- **`cache.KeyScanner`**, an optional `RedisClient` capability (`Keys`), lets
  `Clear` scope its wipe to one prefix instead of flushing the database. A
  prefixed `Clear` refuses without it rather than deleting a neighbour's keys;
  an unprefixed cache owns the database and still flushes.

- **`mcp.Server.UnregisterTool(name)`** removes a tool and reports whether it
  was there. For an opt-in that can only be withdrawn after registration: the
  dev MCP registers its control tools during `InitPlugins`, but whether they
  may be served depends on the listen address, which is not known until
  `Start`. Prefer deciding before `RegisterTool` where the information exists.

- **`router.SanitizePathParam`** exports the truncation `Param` applies, for
  readers that hold a name and a catch-all flag without a `*http.Request`.
  Prefer `Param`, which derives the flag from the route pattern.

- **`component.ComponentContext` is now a `context.Context`**, carrying the
  request context the action arrived on, plus `NewComponentContextFor` to
  build one. A server-action handler receives this value and nothing else, so
  anything it needs from the request — the caller above all — has to be
  reachable through it. A nil `Ctx` reads as `context.Background()`.

- **`codegen.GeneratedFile.Mode`** sets the permission a generated file is
  written with, and tightens a pre-existing file to it before any content
  lands. Zero keeps the 0644 default. Set it for anything carrying a secret.

- **A `hooks:` blueprint key** declares entity lifecycle hooks
  (`id`/`entity`/`when`/`handler`/`description`) alongside `endpoints`. Like
  endpoints it is a declaration rather than generated behaviour: the blueprint
  records which hook runs where and you write the handler. `kiln freeze` emits
  it from the world's hooks. See [blueprints](blueprints.md).

- **`migrate.RenderMigrationFileChecked`** refuses SQL containing a line that
  would parse as a `-- +migrate` directive, returning `ErrDirectiveInSQL`.
  `GenerateMigrationFile` uses it; `RenderMigrationFile` is deprecated in
  favour of it.

### Fixed

- **Live-bus emissions for ambient-transaction writes no longer probe the
  live `*sql.Tx`, and their Postgres confirm query actually parses.** A CRUD
  write joined to `App.InTx` held its `entity.created`/`updated`/`deleted`
  emission back by polling `SELECT 1` on the caller's transaction from a
  goroutine until it reported done. That statement raced the caller's own
  next statement on the transaction's single connection, and the interleaved
  wire protocol crossed their results: the caller's `INSERT … RETURNING`
  scanned `sql.ErrNoRows` (the intermittent `TestInTx_ComposesCommit`
  failure, #353), and a well-formed confirm query came back as `pq: syntax
  error at end of input`. Framework-owned transactions (`App.InTx`, crud's
  own) now attach a `db.CommitQueue` to the transaction context and drain it
  only after `Commit` succeeds, so held-back emissions fire without touching
  the transaction and rollback drops them exactly. Separately, the fallback
  confirm query for caller-owned transactions (`db.WithTx` around your own
  `Begin`) was built with `?` placeholders, which Postgres rejects outright —
  every such emission on Postgres was silently dropped as unconfirmable. It
  now uses the `$N` placeholders the query builders emit everywhere else.

- **Immediate ambient-tx emissions no longer hand the live `*sql.Tx` to
  bus subscribers.** The two `emitAfterAmbientTx` fallbacks that publish
  right away (handler bound to a transaction; record with no extractable
  primary key) passed the original context to `EmitAsync`, which hands it
  to a goroutine per subscriber — so a subscriber following the documented
  `db.TxFromContext` pattern got a live transaction in a goroutine running
  beside the transaction's owner, the same one-connection statements race
  as #353. The new `db.WithoutTx` masks the tx and its commit queue while
  keeping tenant/owner identity, and both call sites use it (#367).

- **The ui-quality eval's first screenshot no longer bills the browser
  launch to its 45s capture budget.** chromedp launches Chrome lazily on
  the first action, so shot one paid for the launch out of a budget meant
  for the capture — CI died at ~45.04s with the whole budget gone at the
  network-guard install while later shots used milliseconds (#342). The
  first shot now gets 150s, past chromedp's own 90s websocket allowance;
  every later shot keeps the tight 45s, and the guard diagnostic prints
  whichever budget applied.

- **crud's per-write transaction now rolls back when code inside it
  panics.** `App.InTx` has always carried a deferred rollback so a panic in
  `fn` cannot leak the pooled connection and its row locks; crud's own
  `inTx` — the wrapper every auto-CRUD write runs under — did not. A panic
  from anything inside the write path (hook panics are recovered, but
  nothing else is) unwound past both `Rollback` and `Commit` and pinned the
  connection until the finalizer; with `SetMaxOpenConns(1)` that is the
  whole pool. Found by the adversarial review of the ambient-tx fix;
  proven by driving `inTx` with a panicking fn on a one-connection pool.

- **Queue completions are fenced on the claim they were issued for.**
  `DBQueue` had no claim identity at all, and its `Nack` updated by bare job
  ID: a worker whose lease expired flipped the RE-CLAIMANT's live row to
  `pending` — a third worker then ran the handler alongside it — or to
  `failed`, dead-lettering a job someone was executing. Neither arm carried
  even the `status='claimed'` guard `Ack` had. `Dequeue` now mints a
  `claim_token` and every completion arm matches on it. `RedisQueue` fenced on
  `Job.ClaimToken` already, but its check and its delete were separate round
  trips, so a lease expiry plus `Reclaim` plus a re-`Dequeue` landing between
  them made the delete remove the NEW claimant's entry: the job sat on no
  list, invisible to `Reclaim`, while `Ack` returned nil.

- **`dequeueSQLite` and the outbox relay's claim take the write lock before
  the read.** Both documented themselves as serialised — "BEGIN IMMEDIATE
  serialises writers naturally" — and neither issued it: `database/sql` sends
  a plain `BEGIN`, which SQLite reads as DEFERRED, so the lock landed after
  the SELECT. Two claimants both picked the same row and the second met
  `SQLITE_BUSY`. 390 busy errors in the queue, 9 in the outbox, from a
  component documented as race-free.

- **`MemoryQueue.Replay` re-enqueues before it drops the terminal record.**
  Removing it first and then failing the enqueue (cancelled context, closed
  queue) destroyed the job: the caller saw an error and retried, and the retry
  matched no dead entry, so `Replay`'s documented idempotent no-op turned a
  visible failure into permanent silent loss.

- **A job whose type has no registered handler is dead-lettered, not
  dropped.** That is the producer-first deploy shape — the queue is fed a type
  the running binary does not know yet — and every such job was destroyed with
  `ListJobs("failed")` and `Stats` showing nothing to reconcile from. The DB
  backend already behaved this way.

- **`queue_jobs` is reachable by `App.EraseUserData`.** The battery registered
  it as a `DataExporter`, payload column included, and no `DataEraser`
  anywhere. Terminal rows make it acute: the only job-row DELETE is
  Ack-of-claimed and `failed` rows are retained on purpose, so a dead-lettered
  job holding the erased user's data survived erasure and was re-disclosed by
  every later `ExportData` dump. See `Job.UserID` above.

- **`truncateError` cuts on a rune boundary.** Slicing at byte 2000 split a
  multi-byte rune, and Postgres rejects the invalid UTF-8 on the settle
  UPDATE: the delivery never settled and its handler re-ran at backoff cadence
  forever, which is what `MaxAttempts` exists to bound.

- **`App.EraseUserData` deletes stored files, not just rows.** It was SQL-only,
  so it deleted the row holding an avatar's storage key and left the object:
  after a report saying `TotalErased=1`, the erased user's file was still
  downloadable at the `/uploads` route the docs tell you to mount — while the
  function's own contract claims parity with `ExportData`, which dumps those
  same columns. Keys are read before the rows go, objects deleted after the
  commit, and image renditions come too. A storage delete that fails is
  returned with the surviving keys named.

- **Batch envelopes reject duplicate, case-folded, and unknown top-level
  keys.** `encoding/json` accepts all three: a duplicated `items` is
  last-one-wins, so a proxy inspecting the FIRST array sees a different
  payload than the handler executes; field matching is case-insensitive, so
  `Items` binds and slips past any check keyed on the exact name.

- **`ServeStreamingList` refuses `AfterList` hooks like `List()` does.** It is
  exported and reachable directly, and enforced the owner/tenant gate for that
  reason — but not this refusal. Streaming never materialises the slice
  `AfterList` runs over, so a hook registered as a redactor was silently
  bypassed and a direct call put the stored values on the wire.

- **Lifecycle events wait for the ambient transaction to commit.** `inTx`'s
  ambient branch deliberately does not commit — the outer owner does — so
  emitting on the live bus at that point announced a row a rollback could
  erase, leaving SSE subscribers and `On`/`Subscribe` handlers holding the
  full record payload of a write that never existed. The outcome is now read
  from the database once the transaction is done, per event kind: a
  rolled-back update leaves the row exactly as present as a committed one, so
  an update is matched on the values the event announced.

- **`LocalStorage.Save` scrubs the absolute path out of its errors.** They are
  `os.PathError` values naming the full storage path, and the CRUD handlers
  echo an upload failure into the 400 body, so an ENAMETOOLONG or EACCES
  disclosed the storage layout. `Get` already scrubbed; the write side is the
  one an unauthenticated multipart POST reaches.

- **`upload.ext` lowercases, and the stored-XSS guard covers XML/XSLT.**
  `ext` promised "the lowercase file extension" and returned `filepath.Ext`
  verbatim, so `payload.SVG` skipped the guard's extension leg — which exists
  precisely because sniffing is unreliable. Separately, an XML document may
  carry an `xml-stylesheet` processing instruction naming an XSLT sheet whose
  transform emits arbitrary HTML, so two uploads compose into the stored XSS
  the HTML case forbids with neither leg looking scriptable alone.

- **`core/yaml` bounds inline-list recursion.** `parseScalar` and
  `parseInlineList` are mutually recursive on `[` and consulted nothing:
  `maxNestingDepth` guards indentation only, and nested inline lists are
  rejected AFTER the full descent, so an attacker picked the frame count and
  the goroutine stack went first. `gofastr generate cli --from <URL>` hands
  remote YAML straight to `Parse`.

- **`core/dotenv` bounds `${VAR}` expansion.** References resolve against keys
  defined earlier in the same file, so each line multiplies the one before it:
  twelve lines of three references is half a megabyte from a ~200-byte file
  where every line sits far under the scanner cap. Every app boot parses
  `.env` from the working directory, so this is startup time. Capped at 10x
  the bytes read; a legitimate chain stays under 3x.

- **`config.parseDuration` refuses an overflowing seconds value** instead of
  wrapping. A TTL of `math.MaxInt64` bound as `-1s`, and which way that fails
  depends on which side of zero the consumer tests.

- **`handler.Bind` sanitizes `path:"…"` tags** the way `router.Param` does.
  `GET /users/42%0Aadmin` bound `"42\nadmin"` straight into handler input, and
  from there into log lines, response headers, SSE frames, and file or
  database lookups.

- **A revoked grant no longer splits the replicas.** `access`'s `Revoke`
  narrowed the local code baseline on top of writing a shared tombstone. The
  tombstone alone already keeps a code-seeded grant revoked everywhere, so the
  narrowing was redundant — and being per-replica it was the one piece of
  revoke state peers could not see. Interleave a Grant on A with a Revoke on B
  and the shared tables end with no grant row and no tombstone: A kept its
  code seed and answered true while B answered false, permanently.

- **The inline-script linter no longer misses four evasions.** It scanned the
  SOURCE TEXT of a literal, so `"\x3cscript\x3e…"` contained no `<` anywhere
  and every pattern missed it; attribute names were anchored on `\b`, and a
  word boundary sits between the `-` and the `s` of `data-src`, so
  `<script data-src="/a.js">alert(1)</script>` read as external; the patterns
  were case-sensitive while HTML tag names are not; and only the FIRST
  `<script>` in a literal was classified, so an allowed leading tag masked
  everything after it. That last one was hiding a real violation in
  `core/mcp`'s widget document, which now carries the directive on purpose.

- **Pagination and DataTable substitute their placeholder literally instead of
  through `fmt`.** Both are documented as Sprintf patterns and their callers
  build them by concatenating a carry onto the placeholder, so
  `url.Values.Encode`'s own `%XX` triples read as flag/width/verb sequences: a
  search for "café" produced `?q=caf%!C(int=1)3%!A(MISSING)9&p=%!d(MISSING)`.
  Nothing failed loudly, because the literal `%d` the constructor checks for
  was still there.

- **`ui.SignOut` scheme-checks its `Action`.** It hand-rolls its `<form>` with
  `render.Escape`, which is HTML escaping and scheme-blind, so
  `action="javascript:alert(1)"` survived intact. Every sibling form sink runs
  its action through `urlsafe.CleanAnchor`; a rejected action now degrades to
  the `/auth/logout` default.

- **`loadModule` shape-checks the module name**, which arrives as a DOM
  attribute: a `../../../evil` token normalized out of the runtime serve route
  onto an arbitrary same-origin script. **The combobox's pre-boot navigation
  fallback is origin-gated** too — the SPA navigator applied its own check and
  the bare `location.href` assignment did not.

- **`framework_docs_search` enforces the hard cap it advertises.**
  `docs.SearchWithLimit` honours any positive value, so `limit=1e12` returned
  10,349 hits for a term as ordinary as "the" — against a tool whose purpose
  is answering agents with narrow contexts. Clamped at 200.

- **`Hidden` fields no longer reach a published tool schema.** The CRUD
  generators filter through `VisibleFields` first, but a custom
  `entity.Endpoint` hands its `InputSchema` straight in — which its docs
  invite. The hidden column's name reached every caller allowed to see the
  tool, listed as required.

- **The dev MCP's control tools are unregistered, not just un-flagged, on an
  exposed bind.** Clearing the intent flag works only when `Start` reaches the
  guard before `InitPlugins` registers them; a host calling `InitPlugins`
  itself pre-Start registers first and learns second, so an anonymous
  `tools/list` named `app_module_enable`/`_disable` and `tools/call` reached
  their handlers through a forged loopback `Host`.

- **Server-action handlers can see the caller.** The endpoint's contract says
  "a handler that mutates anything must check authorization itself", and with
  `r.Context()` dropped at the call site `handler.GetUser` had nothing to
  read, so that check was unimplementable through the API demanding it.

- **An action-id collision refuses at boot instead of cross-wiring.**
  `pathToActionID` turns `/` into `-`, so `/admin/users` and `/admin-users`
  derive the same id and `CompileActions` caches first-wins: the second
  screen's Go handlers were unreachable and a `data-action` click on that page
  resolved into the OTHER screen's registry.

- **A widget's `RequireSession` covers its RPCs.** It gated `/state` and the
  chrome and registered the RPCs bare, so the caller that got a 403 from
  `/state` got a 200 from the widget's own mutation routes in the same
  process.

- **The relay strips every client-claimed forwarding header and bounds the
  response direction.** `X-Forwarded-*` was stripped but not `X-Real-IP`,
  `X-Client-IP`, `True-Client-IP`, `CF-Connecting-IP`, `Fastly-Client-IP`, or
  `Forwarded` — the common CDN spellings an upstream may trust. And
  `MaxBodyBytes` braked requests only, so a vendor endpoint that never ended
  its response held a goroutine, a socket pair, and bandwidth for the full
  request deadline per open client request, at no cost to the vendor.

- **kiln's `POST` routes reject cross-site requests.** The loopback bind
  cannot see this caller: a page the operator visits POSTs to
  `/kiln/tool/approve_plan` from their own machine and the TCP peer is
  loopback either way. The plan gate's entire security leg is a human looking
  at a card. Non-browser callers send no `Origin` and are untouched.

- **kiln journal replay accepts exactly what the live API accepts.** Every
  rule existed on one path only, so a hand-authored `.kiln.session.jsonl`
  installed world state the API refuses and the server booted a world
  `kiln freeze` then rejects: the styling-prop ban on page trees, scaffold
  shape, `api_prefix` normalization, the route method enum, and a page/entity
  mount collision check. The shared rules now live in `kiln/world`, below both
  callers.

- **A poison world edit no longer wedges the runtime.** `net/http`'s pattern
  parser and `App.Mount` PANIC on input they cannot register, and that panic
  escaped `Live.Apply` entirely, so its rollback never ran: the poison stayed
  in the in-memory world and every later `Apply` re-panicked until restart —
  and `New()` had the same hole on a journal already containing one.

- **`kiln freeze` carries hooks into the graduation blueprint.** The live
  preview registers every world hook on the framework `HookRegistry`, so the
  operator watched a `before_create` validation reject bad rows and then
  shipped an app that silently had none. Only `world.json` kept it, and the
  generated app never reads `world.json`.

- **kiln's `sqlDefault` delegates to the framework renderer.** Its copy still
  carried the arm the framework deleted after a verified payload:
  `fmt.Sprintf("'%v'", v)` splices the rendering raw between unescaped quotes,
  and one quote closes the literal and appends arbitrary DDL to
  `ALTER TABLE … ADD COLUMN` — reached from kiln's own `add_entity` op.

- **`JSONL.Append` refuses a line its readers cannot scan.** Every reader caps
  its scanner at 16 MiB, so one oversized entry bricked the journal durably:
  the next `OpenJSONL` died in `countLines`, `Replay` failed in `Read`, and
  `Undo`/`ResetSession` failed inside `TruncateAfter`. Recovery meant
  hand-editing the file.

- **kiln's SSE writer scrubs the event kind.** It is journal-derived, so a CR
  or LF closed the `event:` line and let the rest write its own fields — up to
  a whole synthetic event the client dispatches as if the server sent it.

- **Generated `.env` and `pack -o` output are owner-only.** `os.WriteFile`'s
  mode applies only at creation, so a second `pack -o` over an earlier run's
  file kept 0644, and `.env` inherited whatever was there — while both carry
  the signing secret, the bootstrap admin password, and a credentialed DSN.

- **A `${JWT_SECRET}` reference no longer boots as the key itself.**
  `kiln freeze` emits it so the real secret stays out of the committed file;
  the generator then wrote `JWT_SECRET=${JWT_SECRET}` into `.env`, a
  self-reference dotenv returns verbatim, so the app signed session JWTs with
  the literal string printed in the committed `gofastr.yml`. Nothing failed
  closed: auth rejects only an EMPTY secret.

- **`pack`'s secret warning covers every DSN class the generator hides.** It
  tested for `@` while the generator used `dsnHasSecret`, so a keyword/value
  DSN (`host=db user=app password=hunter2`) was redacted with no
  do-NOT-commit warning printed.

- **Blueprint seed validation matches the boot validator on constraints.**
  Types are deliberately coercion-tolerant, but nothing re-encodes `"abc"`
  into `^[A-Z]{3}$`, so a row violating a pattern or a min length generated
  cleanly, compiled, and then aborted the shipped app's startup on a fresh
  database.

- **A provider stream that just stops is a failure, not a finished turn.** The
  channel closed after the last text delta and `CollectStream` returned the
  partial text with a nil error and an empty finish reason — exactly what a
  complete turn looks like — so a truncated answer was recorded as the
  assistant's turn. Both the openai adapter and the collector now require a
  terminal event. `ErrStreamClosed` already existed and nothing returned it.

- **A tool call cut off mid-arguments no longer bricks the session.**
  `flushTool` promised to "validate the accumulated JSON; if invalid, keep the
  raw bytes" and did neither, storing `{"path":"/et` as the Input: every later
  marshal failed, so the next provider request errored out and the persistence
  layer dropped the tool-intent row.

- **The harness TUI strips terminal escapes from agent-derived text.** Model
  output, tool results, and upstream error strings reached scrollback
  verbatim, where OSC 52 writes the user's clipboard, OSC 0 rewrites the
  window title, CSI ?1049h flips to the alternate screen, and a bare CR plus
  BEL overwrites the row just drawn.

- **The harness MCP and WebSocket transports enforce the token's scope.** The
  MCP HTTP handler verified the bearer and discarded the claims, so a token
  minted for session A drove session B by naming B in the `tools/call`
  arguments; the WebSocket checked `AllowsSession` at the upgrade and then
  dispatched whatever verb arrived. `rest.go` enforced both already.

- **A failed credential-store load stays a failure.** `loaded` was set on
  entry, so after a wrong-key decrypt the store held zero data and believed it
  was current: `Get` answered "not found" instead of repeating the error, and
  `Put` then re-encrypted that empty map under the wrong key and wrote it over
  the file, destroying every credential the real key protected.

- **The harness secrets loader refuses key-derivation vars from the walked
  file.** It walks UP from the working directory, so on a cloned repo the file
  is attacker-authored: delivering provider API keys is the contract, deciding
  how the credential store is ENCRYPTED is not.

- **The harness web sidecar pins the Host and checks the Origin.** `/input`
  decodes JSON regardless of Content-Type, so a `text/plain` fetch with no
  preflight injected a prompt into the live session at model authority from
  any page the operator visited.

- **`RedisCache` distinguishes a backend outage from a miss.** It wrapped
  EVERY client error in `ErrCacheMiss`, which the `Cache` interface documents
  strictly as not-found, so a caller that fails closed on a miss — negative
  caching, a revocation list — failed OPEN for the whole outage.

- **Cache prefixes are injective and `Clear` is scoped.**
  `WithPrefix("u:alice")` with key `admin:x` and `WithPrefix("u:alice:admin")`
  with key `x` both produced `u:alice:admin:x`, so one namespace could read,
  overwrite, or delete another's entries. And `Clear` ignored the prefix and
  issued `FlushDB`, so with the per-tenant prefixes this battery documents,
  one tenant's `Clear` was every other tenant's data-loss event. A `:` inside
  a prefix is doubled now; see `KeyScanner` above.

- **The cache middleware's `Vary` key uses the complete field value.**
  `Header.Get` returns the FIRST value of a repeatable field, so
  `X-Team: alpha` and `X-Team: alpha, omega` were one variant and whichever
  arrived first decided what both received.

- **The admin nav drawer mounts behind the admin authorizer.** Its widget
  routes went on the bare router, so the `/chrome` endpoint — which is the
  back-office entity map — answered any anonymous GET, against the package's
  own "There is no unauthenticated or self-service path".

- **`battery/log`'s read tools require an authenticated caller.** They hand
  out every caller's request paths, remote IPs, and request IDs, straight off
  `/mcp` with no route middleware in the way.

- **`battery/storage` returns typed errors**, so `ServeHandler`'s documented
  404/400 classification holds. Untyped errors fell to the 500 arm, making
  gone and broken indistinguishable to clients and caches.

- **`setup`'s headless `RunSteps` refuses on a completed install**, the
  re-run guard the interactive skin has enforced since `runStepSerialized` was
  written. `GOFASTR_SETUP=force` reaches it, where the bootstrap step INSERTs
  an admin-role user, so a redeploy with force still set silently minted a
  second admin.

- **The setup cookie carries a freshly minted secret, not the URL token.**
  The token was invalidated in its URL form and handed straight back as the
  cookie value, so anyone who saw only the URL — a proxy or access log —
  replayed it from a second client and held the wizard for the life of the
  process, which is the replay `first-run.md` rules out.

- **`GenerateMigrationFile` refuses SQL that would synthesize a directive.**
  A column DEFAULT spanning a line reading `-- +migrate Down` truncated the Up
  section mid-literal: the committed migration no longer parsed and the
  author's remaining bytes sat in Down awaiting a rollback.

- **A rejected upload no longer leaves its bytes in storage.**
  `ProcessFileField` saves the primary before deriving renditions, so a derive
  failure rejected the upload and kept the full-size object — repeat inert
  posts against an `Image` field and every request fails while storing
  another, invisible to row-driven cleanup. The deriver now receives a
  recording view of the store, so partial renditions are rolled back too.

- **`DeleteFileField` removes the renditions.** Only the primary was deleted,
  so every derived object outlived the row's cleanup and kept serving at the
  documented `/uploads/{key...}` route — while `ImageDerivatives`' own doc
  names "a storage delete" as a sink a variant ref reaches.

- **Variant storage refs are checked for control bytes**, which the primary
  always was, and **`ProcessFileField` sanitizes the `Filename` it stores** so
  it can no longer return a `FileField` its own `Validate` rejects.

- **The framed plugin CSP sets `form-action 'none'`.** A frame granted
  `allow-forms` submits by NAVIGATING, which `connect-src 'none'` does not
  cover, so it could POST whatever it had read to any origin.

- **The eval runners share one environment policy.** backend-adoption started
  its agent-built candidate server with `os.Environ()` whole — the operator's
  cloud keys, SCM tokens, and `DATABASE_URL` — while the ui-quality twin
  already passed an allowlisted environment. Their credential denylists were
  hand-copied and had drifted; neither recognised a bare `*_TOKEN`.

- **Generated llm.md describes the routes the mount serves** (#358,
  sibling of the #266 openapi/llm.md fix further down): `App.View`
  registers its entities read-only, but `EntityLLMMD` took only the
  entity — read-only lives on `CrudRouteOptions`, which no entity
  declaration carries — so every view served a doc telling agents to
  call POST, PUT, PATCH, DELETE and the three `_batch` routes, all of
  which answer 404/405, and the `/api/llm.md` index counted eight
  endpoints where three are served. The doc now takes its answer from
  the mount: a read-only mount's llm.md omits the write, batch and
  Create/Update-column sections, the index counts three endpoints and
  labels the row `read-only`, and the `limit` row prints the same cap
  the List route clamps `?limit` with (a hardcoded "max 100" told
  agents an entity with `MaxListLimit: 3` would serve 100).
  **BREAKING for direct callers**: `crud.RegistryLLMMD` /
  `crud.RegistryLLMMDHandler`'s mount predicate widened from
  `func(*entity.Entity) bool` to `func(*entity.Entity) crud.MountInfo`
  (nil still documents standard CRUD for every entity), and
  `crud.EntityLLMMD` / `crud.LLMMDHandler` / `crud.LLMMDHandlerFor`
  gained an optional `LLMMDOptions` — pass `LLMMDOptions{ReadOnly:
  true}` beside a read-only mount. The framework's own route
  registration passes both automatically; only hand-rolled mounts need
  the option.

## [0.77.0] - 2026-08-31

### Added

- **`ui.Menu` submenus and `menuitemradio` rows** (#319): `MenuItem` gained
  `Children`, `Radio`, and `Checked`. An item with `Children` renders a
  submenu — a `<summary role="menuitem" aria-haspopup="menu">` disclosing a
  nested `role="menu"` through the same `data-fui-disclosure` machinery as the
  top level — and the full keyboard contract ships with it: ArrowRight opens a
  submenu and moves focus in, ArrowLeft closes it and returns focus to the
  parent row (swapped in RTL), roving focus and type-ahead stay scoped to the
  item's own panel, Escape closes one level at a time, and Tab closes the whole
  chain. A submenu parent is purely a disclosure: `Children` alongside `Href`,
  `RPC`, or `Radio` panics at render time. An item with `Radio` renders
  `role="menuitemradio"` with `aria-checked` from `Checked`; activating a row
  checks it and unchecks its same-group siblings client-side, while RPC and
  href rows still fire and the server re-render stays authoritative. The check
  indicator and submenu caret are CSS pseudo-elements, so accessible names and
  type-ahead see the label alone. Zero-value output stays byte-identical,
  golden-pinned, including nested items and `ExtraAttrs` id-dropping at depth.
  The menu module is 1130 gzipped bytes and disclosure 1061, both well inside
  their 3072 budget; #319 itself added no core runtime bytes.

- **`registry.IsolateForTest(t)`** swaps the process-global style registry for
  a fresh one and restores it when the test finishes. Test-only seam for
  asserting registry-shaped behaviour — the SSR host's single-direct-link vs
  bundle `<link>` decision, exact bundle name sets — without assuming a clean
  process: any package linked into a test binary that registers styles at init
  (`framework/ui` registers `ui-button`, `ui-page-header`, and `ui-sidebar` as
  `LoadAlways`) otherwise changes the eager set every test in that binary sees
  (#331). Styles registered during isolation are dropped on restore, so they
  cannot leak into later tests. No runtime behaviour change.



- **`uihost.WithStrict` internal-link check** fails boot when the site chrome
  (each layout's header, sidebar, footer) links to a path nothing serves. The
  check renders the chrome and resolves every internal `href` against the
  app's full served surface — but only once that surface exists: it runs at
  boot, not at Mount. Batteries and plugins register their routes during
  `App.Start`'s InitPlugins phase and `App.Start` itself registers more after
  them, so a Mount-time table is partial and would flag working links (a
  sidebar "Back office" → `/admin` panic-boots an app that serves it).
  `App.Start` now calls a mounted host's `ValidateBoot` (the new
  `framework.BootValidator` seam) after the last route registration and
  before the listener binds — the latest point at which a finding can still
  refuse to serve. Resolving against the complete table also covers
  UIHost's own conditional endpoints (`/llm-pages.md`, `/llms-full.txt`,
  the agent card, `/.well-known/jwks.json`) and every config-gated
  artifact (`/sitemap.xml` without `WithSitemap` is still a dead link).
  Percent-encoded hrefs are decoded before matching, the same way
  `net/http` decodes before routing, so `/docs/caf%C3%A9` resolves against
  a screen at `/docs/café`; a malformed escape (`/bad%zz`) can never be
  served and is reported as a finding. One documented gap: a catch-all GET
  route (`/{path...}`) satisfies the check for every path it claims, so
  links under one are accepted, not verified — the handler may serve them,
  and probing it would mean executing the app at boot. A host mounted
  outside a `framework.App` never reaches the boot hook and never gets the
  link check. External URLs, anchors, query-only references, template
  placeholders, and relative references are out of scope; `ExemptScreens`
  entries also exempt links whose target falls under them. Tuned, like
  every strict check, through `StrictConfig.InternalLinks`
  (`enforce`/`warn`/`off`).

- **`ui.Menu` disabled rows no longer emit a malformed attribute** (#327):
  `aria-disabled` was concatenated straight onto `tabindex="-1"` with no
  separating space, so a disabled row rendered
  `tabindex="-1"aria-disabled="true"`. Browsers recover from it; strict
  parsers and DOM-diffing tools need not.



- **`Screen.NoSPA` and `data-fui-nav="off"`** exclude a destination from soft
  navigation, at two grains: `NoSPA` drops a route from the client route
  manifest so the runtime treats it as unknown and every link to it does a full
  document load; `data-fui-nav="off"` on an anchor declines the soft navigation
  for that one link. Both exist for ported pages that bind their behavior at
  script load — a soft swap never re-runs those initializers, so every handler
  on the destination dies. See [porting](porting.md).

- **`UIHost.PageHandler(path)`** renders a registered screen as a full page,
  chrome included, from a route mounted on the framework router. Needed when an
  app wildcard subtree (`{path...}`) claims a bare path: the mux redirects it to
  a trailing-slash form the NotFound dispatch never resolves, so the wrong
  screen answers. A dynamic pattern is passed through rather than forced, since
  rewriting it would hand the literal `{id}` to param capture.

- **`ScreenStatusCode`**, an optional screen interface, overrides the HTTP
  status of a page that rendered successfully. A screen whose route resolved but
  whose record is gone renders the not-found body through the layout while
  signaling 404 to clients and crawlers. Zero or 200 keeps the default.

- **`sdkdocs.Config.CRUDMounted` and `App.EntityCRUDMounted`** keep the SDK
  docs site to routes that exist. `Exposure.CRUD` alone cannot answer "were
  this entity's routes mounted": an app with no DB registers no CRUD while
  every entity still reads `nil` ("auto"), so the site published a reference
  page — and an SDK download — for paths that answer 404. The predicate that
  route registration, the startup banner, `openapi.json` and `/api/llm.md`
  already agree on is now exported, so a host-mounted surface can reach it
  too. Pass `CRUDMounted: fwApp.EntityCRUDMounted`; nil keeps the older
  `Exposure`-only behaviour.

- **`data-skip-link` on the skip link**, so a test suite can address it
  without pinning the `.skip-link` CSS class. The skip link is synthesized
  by the framework rather than configured by the app, so `ExtraAttrs` — the
  supported way to attach attributes to a component — cannot reach it.
  Inert for everyone else.

- **`ui.Tabs` porting knobs — `TabsConfig.StateAttrs`, `.ID`,
  `.VacateHidden`** — the contracts a tab strip ported from a component
  library needs. `StateAttrs` mirrors `data-state="active"/"inactive"`
  onto every tab button after client-side switches, the locator contract
  Radix-style ports pin their tests to (core already mirrors
  `aria-selected` on the same write); `ID` wires tab↔panel
  `id`/`aria-controls` semantics; `VacateHidden` ships hidden panels
  empty with their content parked in an adjacent `data-fui-tabs-stash`
  JSON script, so page-scoped test locators cannot match text inside
  hidden panels — DOM parity with a source component that unmounts
  inactive panels. A demand-loaded `tabs` runtime module (armed by
  `data-fui-prefetch="tabs"`, no core scanner entry) restores a panel's
  content on first show and moves the live nodes out/in on every later
  switch, so island-swapped content and form state survive re-show. All
  three knobs are opt-in; every zero value keeps the default output
  byte-identical. See [interactive patterns](interactive-patterns.md).

- **`ui.MenuItem.ID`** sets the rendered menu row's `id` attribute, so page JS,
  test suites, and aria wiring elsewhere on the page can address one exact row
  (a Help Mode toggle a script binds to, an Imports row a shortcut targets).
  Uniqueness is caller-owned like any HTML id, an empty value emits no `id`
  (output unchanged), separators ignore it, and `MenuItem.ExtraAttrs` still
  drops `id` — the field is the single owner.

- **`data-fui-ctx` trigger-carried chrome context** (#321): an open trigger
  (`data-fui-open="layout-remove" data-fui-ctx="inv-42"`) forwards its
  context on the chrome fetch as `?ctx=`, the chrome render reads it via
  `widget.ChromeContext(ctx)` in a slot's `RenderCtx`, and the runtime keys
  its chrome cache by `(name, ctx)` — two triggers with different contexts
  get two distinct chromes, a repeat of the same context is a cache hit.
  One widget definition now serves every per-entity dialog. The endpoint
  bounds `ctx` at 256 bytes and rejects control runes with a 400 rather
  than truncating (a truncated id would render chrome for the wrong row);
  the string stays opaque to the framework and authorising the entity it
  names belongs in the slot, against the request context. The context
  cache is capped at 32 entries per document (LRU, evicted entries
  refetch) so a row-per-dialog page cannot grow it without bound.

- **Combobox static filtering now hides non-matching options** (#337): the
  combobox stylesheet's `[role="option"] { display: block }` (re-set to
  `display: flex` under `@media (pointer: coarse)`) is author-origin, which
  beats the user agent's `[hidden] { display: none }` regardless of
  specificity — so the runtime set `opt.hidden` correctly, the attribute had no
  visual effect, and typing in a command palette left every row painted. An
  explicit `[role="option"][hidden] { display: none }` guard restores filtering
  on fine and coarse pointers. The chromium palette harness also named the
  combobox sheet at `/__gofastr/combobox.css`, which 404s, so every palette
  test had been running with no combobox CSS at all; the catalog now points at
  `/__gofastr/comp/combobox.css` and the test refuses to run if that sheet
  stops loading. The regression test counts painted rows via client rects,
  never `hidden` attributes — an attribute-level assertion passes against the
  broken behaviour, which is how this survived.

- **`ui.CommandPalette` gained a visible close control and a bounded mobile
  dialog** (#325): the palette rendered no dismiss control a touch user could
  reach — the `Esc` hint chip is an instruction a phone cannot follow, and the
  full-screen mobile sheet covers every backdrop pixel — and at
  `max-width: 540px` the `100dvh` sheet grew past the viewport on long command
  lists, clipping the input off the top. The footer now carries an icon-only
  close button at every breakpoint (`data-fui-action="close"`, the same
  declarative dismiss hook the section-menu drawer uses, with a 44px tap
  floor), and the dialog is bounded to the viewport at every size with the
  suggestion list scrolling inside the remaining space. Escape, backdrop
  dismissal, the focus trap, and reopen behaviour are unchanged, now pinned by
  chromedp tests at 390x844, 1280x800, and a 1280x240 short-viewport guard.

### Fixed


- **BEHAVIOUR: Escape now closes only the innermost open disclosure**, not every
  open one on the page. Previously an Escape anywhere closed them all; now, with
  focus inside an open `data-fui-disclosure`, only the deepest one containing
  focus closes, and focus outside any of them still closes all. The change came
  from nested menus needing one level per press, but `data-fui-disclosure` is
  shared, so it reaches every consumer: `disclosure.Render`,
  `html.Details{Disclosure: true}`, `ui.Collapsible`, `ui.Sidebar` groups,
  `ui.SiteHeader`'s hamburger drawer, `SectionMenu` groups, and the admin-sort
  panel. Concretely: a mobile drawer containing an expanded nav group now takes
  two Escapes to dismiss where it took one. One of two changes in this release that
  alter behaviour for an app upgrading from v0.76.0 without touching its own
  code; the other is `WithStrict`'s internal-link check, which can newly fail
  boot for an app already running strict mode.

- **Generated marketing chrome links and auth-gate redirects follow the
  blueprint's registered screens** (#312) (`gofastr generate`). The marketing
  header nav and footer shipped four literal hrefs (`/pricing`, `/about`,
  `/terms`, `/privacy`), so a marketing blueprint without those screens got a
  footer full of 404s; a chrome link is now emitted only when a screen
  registers its route. The auth gate's redirect — the screen mount, the
  entity-list island policy, the header's Sign in button, and the failed-login
  bounce — derives from the screen hosting the login form (nested in a section
  counts) instead of the hardcoded `/login`, which 404'd every gated page on a
  blueprint whose sign-in lives elsewhere.

- **A failed `data-fui-prefetch` fetch no longer strands the module for
  the page lifetime.** The bridge marked an element attempted on first
  hover even when the fetch failed, and a module without a scanner
  marker (`tabs`, deliberately) had no second loader — one transient
  failure left vacate panels empty for good, silently. An element is
  now marked attempted only once its fetch succeeds, so the next
  hover/focus retries; in-flight re-hovers cost nothing (the loader
  dedups). Cost: 5 gzipped bytes of core runtime.

- **Widget chrome cache no longer outlives a principal change** (#329):
  `NS._chromeCache` was keyed by widget name alone and never invalidated,
  while `serveChrome` renders with the request context — per-principal by
  construction. Cross-page navigation is client-side and keeps the
  document, so a sign-in/sign-out performed without a full page load
  (intercepted form, RPC + navigate) left the previous principal's chrome
  cached: the signed-out visitor reopening the widget was served the
  earlier principal's personalised chrome. No client-visible principal
  exists to key on (session cookies are HttpOnly), so the cache is cleared
  on every SPA navigation — the cost is one refetch of a previously-opened
  widget's chrome per navigation, the same no-store GET its first open
  made. The cache lives in one document's `window`; it cannot reach
  another session, this was staleness with a privacy edge, not a cross-user
  leak.

- **`gofastr pack` no longer drops `middleware`, `plugins`, and `helpers`
  declarations** (#318): the serializer's key list named all three while the
  serializer itself never emitted them, so a blueprint carrying any of them
  packed without them. The same class also took `app.description`,
  `app.base_url`, and `app.public_openapi`: decoded, then silently omitted.
  The serializer round-trip test now runs against every committed example
  blueprint instead of only Meridian — the one example that declares none of
  the affected constructs — and a reflection-driven guard keeps the key list
  and the serializer honest about every field `Blueprint` and `BlueprintApp`
  carry, so the next construct added to either side fails a test the day it
  diverges.

- **A quote character in an enum value no longer merges flow-list entries**
  (#323): the quoting predicate treated quotes as a first-character-only
  indicator, so `values: [60', 90']` was emitted with both apostrophes bare
  and re-parsed as a single member `"60', 90'"` — two enum values silently
  became one, and a lone `values: [60']` failed to parse at all. Values
  containing `'` or `"` are now double-quoted: `"` and backslash are escaped,
  invalid UTF-8 bytes and non-printable runes as `\xNN`/`\uNNNN` (so a quoted
  value re-parses byte-for-byte instead of gaining U+FFFD), and the apostrophe
  passes through verbatim — `\'` is not an escape `strconv.Unquote` knows, and
  inside double quotes it needs none. This mirrors the key-side rule from
  #317, which refuses instead because core/yaml never unquotes keys.

- **`gofastr pack` no longer drops `seed.count`, `seed.weights`, and entity
  `renames`** (#330): the same decoded-but-never-serialized class as #318,
  one level down — inside slice elements, where neither the example
  round-trip (no committed example uses them) nor the top-level coverage
  guards (a set `Seed` field already moves the output by emitting the `seed`
  key) could see the omission. The guard now probes every construct field
  (entities, fields, relations, indices, screens, body/children blocks,
  actions, transitions, nav, seed, endpoints, stubs); the same run caught
  stale key orders — `entityOrder` still listed the pre-grouping flat keys
  (`crud`, `mcp`, `soft_delete`, …) while missing
  `scope`/`pagination`/`exposure`/`search_fields`/`renames`, `fieldOrder`
  missed `no_query`, and `blockOrder` missed `filters`. Two fields are
  exempted with reasons in the test: entity-level `endpoints` (the
  authoring form lives in the top-level `endpoints` stubs; emitting the
  derived form would duplicate them) and index `expression` (not in the
  blueprint grammar).



## [0.76.0] - 2026-08-30

### Added

- **Widget authoring, host-theme consumption, and the agent-host contract**
  (#291): `AppConfig.HTML` was a raw string, so a widget author hand-wrote
  the whole document — and a one-character typo in the widget client's
  script URL produced a widget that rendered and silently never received
  anything. `mcp.WidgetDocument` assembles the document (doctype, head,
  root element, the client `<script src>`, the author's script) with the
  script URL taken from the same constant the server mounts the route at,
  so it cannot drift; `html/template` escapes the data fields
  (`Title`/`Lang`/`RootID`) for their contexts while `Body`/`Script` stay
  verbatim author content, and a `Script` carrying `</script` or `<!--`
  is rejected at build time instead of shipping a document the HTML
  parser silently truncates. A round-trip test reads the `ui://` resource
  back over `resources/read` and asserts the document's one script src is
  exactly `mcp.WidgetClientScriptURL`, then calls the linking tool.

  The widget client now consumes the host's theme signals (spec 2026-01-26
  `HostContext`): on `connect()` and on every
  `ui/notifications/host-context-changed` (partials merged, as the spec
  requires) it applies `theme` to the document root as
  `<html data-theme>` + `color-scheme` — the pair that makes the host's
  `light-dark()` variable values resolve — writes `styles.variables` to
  the root as inline custom properties, and injects `styles.css.fonts` as
  one replaced-never-stacked `<style>` element, before any registered
  handler runs. Widgets consume host theme and invent no palette; the
  builder emits zero CSS. `framework/docs/content/agent-host.md` is the
  contract doc (three settled decisions, `RoleAgent`, authoring
  end-to-end, the theme convention, and what GoFastr deliberately does
  not do), cross-referenced from agent-ready, scaling, mcp, and
  generative-ui.

- **Signed A2A agent cards** (#289): `AgentCardConfig.SigningKeys` signs
  `/.well-known/agent-card.json` per A2A v1.0 §8.4 — one JWS per key
  over the RFC 8785 canonical form of the card (with `signatures`
  excluded), `EdDSA` for Ed25519 and `ES256`/`ES384`/`ES512` for EC
  P-256/P-384/P-521, unsupported key types refused at mount — plus a
  public-keys-only JWK Set at `/.well-known/jwks.json` and an RFC 7638
  thumbprint `kid` default. The canonicalizer is a new stdlib-only
  `core/jcs` package, verified three ways: the RFC 8785 reference test
  suite, 5,000 vectors from the reference implementation's
  deterministic number generator, and a 227-value corpus diffed
  byte-for-byte against Node's own serialization; the served signatures
  verify under Node WebCrypto with zero shared code. Signing requires an
  explicit `BaseURL` and the host panics at mount without one: a signed
  card whose URLs derive from the request `Host` header would hand an
  attacker a validly-signed card pointing at their own endpoint.

- **Inbound Web Bot Auth verification (experimental)** (#290):
  `WithWebBotAuth` grew a `Verify` field — the option now owns both
  directions of the protocol. Publishing (the JWKS at
  `/.well-known/http-message-signatures-directory`) is unchanged and
  pinned byte-for-byte by a test when the new fields are unset.
  `Verify` installs RFC 9421 verification under the profile of
  `draft-meunier-webbotauth-httpsig-protocol-02` (18 August 2026 —
  pinned; the draft is moving and this half tracks it deliberately).
  Ed25519 only. Handlers read `framework.VerifiedAgent(ctx)` for the
  verified identity (resolved directory URL + key thumbprint), nil for
  the unverified majority.

  Default is observe mode — annotate and log, never block — so a
  verification bug cannot take an app's traffic down. `Require` refuses
  unverified traffic with 403, with two deliberate exceptions: the
  site's own key directory stays reachable unsigned, because a remote
  verifier fetches it to check the signatures on requests we send, and
  a request the verifier declined to examine (see the lookup budget
  below) gets 503 with `Retry-After` rather than 403 — a busy resolver
  is our backpressure, not a verdict on the sender's signature.

  At most four signatures per request are processed: each one can cost
  a DNS check and a directory fetch, and coalescing bounds nothing for
  a sender naming distinct hosts. The agent key-directory fetcher
  treats its URL as an SSRF primitive: `core/netguard` at parse,
  resolve, and dial time (the TOCTOU-closing hook), environment
  proxies disabled (a proxy would resolve and connect to the fetched
  host while the dial-time hook saw only the proxy), https only,
  redirects never followed, 256 KiB body cap, 5 s timeout, 32-key cap,
  bounded budgets on concurrent fetches and concurrent DNS lookups,
  separate bounded positive/negative caches with coalescing, and
  refresh-on-TTL rotation where a successful refetch replaces the key
  set and a failed one never evicts. A caller's own cancellation is
  never negative-cached, so a client that aborts cannot keep a
  legitimate agent refused for the TTL by repeating it.

  Conformance: RFC 9421's Ed25519 vector (B.2.6), the draft's
  E.2.1–E.2.3 vectors, a Node WebCrypto cross-check, and mutation
  proofs for every guard, all committed as testdata. The generated
  SDKs, app CLI, and webhook battery deliberately do not sign: baking
  draft churn into generated code would make every draft revision a
  breaking change downstream.

## [0.75.0] - 2026-08-29

### Added


- **The agent surface's middleware posture is pinned** (#291): a cookieless
  bearer request to `/mcp` traverses the whole default chain untouched — CSRF
  skips it by design, nothing demands a cookie, Origin or `Sec-Fetch-Site`,
  and nothing rewrites `Authorization` — while a request carrying neither
  credential is refused. That was already true and guarded by nothing: every
  prior wiring test built its app with `WithoutDefaultMiddleware()`, so the
  real chain had never met a bearer `/mcp` request. Now covered on both the
  serve and agent roles, against live listeners.

- **The widget client mounts itself** (#291): an app that registers an MCP App
  with `WithMCPApp` now serves the widget client at
  `mcp.WidgetClientScriptURL` automatically, rather than every author wiring
  the same route by hand. It mounts only when at least one app is registered,
  mirroring how `initialize` advertises `resources`/`prompts` only when
  something is registered, and it yields with a warning to a route the host
  already mounted rather than panicking on the conflict. `RoleAgent` forwards
  the path to the app router, because in a role-split deployment the agent
  listener is the origin a widget fetches its script from — not forwarding it
  would 404 every widget exactly when the agent process is the MCP endpoint.

- **MCP Apps widget client** (#291): `core/mcp` served the app side already
  (`RegisterApp`, `_meta.ui`), but the JS that runs inside the host's iframe
  and talks to the chat host did not exist, so every plugin author hand-rolled
  it. `WidgetClientJS()` / `WidgetClientHandler()` ship it the way
  pluginhost ships its frame client: the `ui/initialize` handshake followed by
  `ui/notifications/initialized`, the six widget-to-host requests
  (`tools/call`, `resources/read`, `ui/open-link`, `ui/request-display-mode`,
  `ui/message`, `ui/update-model-context`) and handler registration for the
  host-to-widget notifications.

  Every method-name string is pinned against non-comment JS, so a one-character
  typo — a widget that silently never fires — fails a test instead of shipping.
  Requests are correlated by id through a null-prototype map bounded at 64 in
  flight, rejecting `E_SATURATED` without posting rather than growing, with
  timeout, send-failure and teardown all rejecting rather than hanging.
  Messages are accepted only from the host window by `event.source`: the widget
  frame is opaque-origin, so an origin-string check is the wrong tool.

  GoFastr serves the widget side; it is not a chat host, and the host half is
  deliberately absent.

- **`RoleAgent` — an agent-only HTTP surface** (#291): `GOFASTR_ROLE=agent`
  (or `WithRole(RoleAgent)`) serves `/mcp` and health only, forwarding MCP to
  the app router so auth and owner scoping are identical to the serve role.
  Entity CRUD, OpenAPI, docs, admin and well-known discovery are not served,
  and workers do not start.

  The slice that mattered was not the role constant but the identity contract
  underneath it, which had no coverage at all: bearer tokens and session
  cookies both resolve a user today, and nothing guaranteed the same person
  arriving by token versus by cookie resolved to the same owner-scope
  principal. On an agent surface that is live — an agent would either miss its
  own user's rows or see another user's. The contract **holds**, and is now
  pinned on REST and `/mcp`: same user, same `GetID()`, same owner-stamped
  rows, with a second user's rows staying invisible. Injecting a divergent
  principal on the bearer path fails those tests, so they are guards rather
  than tautologies.

- **Combobox keyboard navigation skips filtered-out options** (#302):
  after typing a query, ArrowDown/ArrowUp/Home/End cycled through rows the
  static filter had hidden, so the `aria-activedescendant` highlight landed on
  an invisible option and Enter selected one the user had filtered away. The
  navigable-option selector now excludes `[hidden]`.

- **`ui.Sidebar` can express a server-owned collapse contract** (#298):
  `SidebarConfig.Collapse` is a tri-state mirroring how `CurrentPath` already
  works — the zero value keeps today's localStorage behaviour byte-identical,
  and `SidebarCollapseCollapsed`/`SidebarCollapseExpanded` mean the server
  decides. When the server owns the state the runtime neither reads nor writes
  localStorage, so a per-user setting restored from the database survives first
  paint on a device whose local value disagrees; SSR could not ship a collapsed
  sidebar at all before this.

  Also: `GroupMarkup` renders groups as `button[aria-expanded][aria-controls]`
  plus a `hidden` container for hosts pinned to that shape (the default stays
  `<details>`; the button dialect does not persist open state across navigation
  and needs JavaScript, both documented), `CollapseLabel`/`ExpandLabel` are
  configurable, and a `SidebarAutoHide` variant ships a 64px icon rail
  at >= md that reveals the full column on hover and `:focus-within`
  (keyboard), with no JavaScript. The collapse button now carries the
  expand label and `aria-expanded="false"` while collapsed — it
  previously hardcoded `aria-expanded="true"`, which was wrong the
  moment the rail collapsed.

- **`ClientModule.AssetServer(prefix, specs)`** (#300): builds a plugin's
  `AssetServer` from the module, reading `ClientModule.Assets` and
  threading `Manifest.CSP` through `WithCSP`. `Manifest.CSP` was the one
  manifest field a host could set correctly and have the frame ignore,
  because it is applied as a response header rather than carried on the
  manifest object to the mount: a plugin could declare the wasm tier, pass
  `Validate`, and still get a frame that refused to compile WebAssembly,
  reported only as a `CompileError` inside an opaque origin with
  `connect-src 'none'` and no way out. Building the server from the module
  makes the manifest the single source of truth, and passes less than the
  old call did (`fsys` comes from the module). `NewAssetServer` stays for
  assets that belong to no module.


### Fixed

- **`data-fui-ctx` now reaches SSR-inlined chrome.** A widget declared
  `Hidden().DeepLink(...)` *is* inlined when the request URL matches its deep
  link, and the runtime hydrated that node without ever reading the trigger's
  `ctx` — so a per-entity dialog opened from a ctx-carrying trigger rendered
  chrome for the wrong entity, or a form posting to a placeholder action. That
  is the shape `core-ui/ARCHITECTURE.md` prescribes for "edit/show entity
  detail" and the one the framework's own gallery ships, so it was the
  documented path that was broken, not an edge case. The runtime now drops a
  ctx-less inlined node when a trigger carries `ctx` and falls through to the
  `(name, ctx)`-keyed fetch; a ctx-free open still hydrates in place. Two
  comments in `framework/uihost/uihost.go` and a paragraph in `widgets.md`
  described the old, wrong behaviour and are corrected.

- **Cross-test browser-cache contamination in the site e2e suite** (#278): the
  suite runs on one Chrome profile, so tabs are fresh but the HTTP cache is
  shared, and split runtime modules are served `immutable` with a year-long
  max-age. Because `httptest` ports are released and rehanded out by the
  kernel, a later test could inherit a port for which the profile already held
  a cached 200 of `toasts.js?v=<hash>` — same port plus same content hash is
  the same cache key. The failure-injection server then never saw a request,
  the module loaded from cache, and the test that expects a fallback correctly
  found none. Each tab now clears the browser cache, restoring the per-test
  isolation the unique-port assumption already claimed.

- **An `AssetSpec` without a `ContentType` now serves one derived from the
  filename** (#303) instead of an empty `Content-Type`. `writeAsset` set
  the header unconditionally from the spec and set `nosniff` on the next
  line, so an omitted type served 200 with the right bytes and a document
  the browser was forbidden to parse or to recover by sniffing — silent in
  the server log, the console, and as a page error alike, leaving an empty
  frame that reads as broken plugin JS. Detection goes through
  `core/static.DetectFromName`, the repo's canonical detector: its table
  wins over `mime.TypeByExtension` so `.html`, `.js`, `.css` and `.wasm`
  resolve the same on every host, the stdlib covers only the long tail,
  and `application/octet-stream` is the floor. An explicit `ContentType`
  still wins, `nosniff` is unchanged, and `AddBytes` gets the same
  default.

- **`AssetServer.Register` rejects specs with no filesystem to read them
  from** instead of panicking the request goroutine later. `fs.ReadFile` on
  a nil `fs.FS` dereferences the nil interface, so a module that declared
  specs and shipped no assets took down whichever request first asked for
  the frame. `ClientModule.Assets` is optional — a plugin may serve its own
  assets — but such a plugin passes no specs to an `AssetServer` either, so
  a nil FS carrying specs is a wiring mistake, not a runtime condition, and
  it now fails at boot with a message naming the fix. A nil FS with no
  specs stays valid: that is the byte-backed server built from `AddBytes`
  alone.

## [0.74.0] - 2026-08-29

### Changed

- **BREAKING — `mcp.AppConfig.CSP` is a struct, not a string**: the MCP
  Apps spec defines `_meta.ui.csp` as an object
  (`connectDomains`/`resourceDomains`/`frameDomains`/`baseUriDomains`), and
  GoFastr emitted a bare string there, so a spec-compliant host could not
  parse the policy at all. `CSP` is now `mcp.AppCSP` with those four fields.
  Breaking loudly rather than behind a shim, because the old field produced
  JSON no conformant host could consume — there is no working caller to
  preserve. An app that sets no CSP emits the same `_meta` as before.

- **BREAKING — `kiln acp` speaks the real ACP session surface** (#287):
  `tools/list`, `tools/call`, `prompt` and `shutdown` are gone and
  `initialize` changed shape. None of those were methods in the published
  protocol; what kiln spoke was a bespoke subset. Move to `session/new`,
  `session/prompt` and `session/cancel`, with tool invocations arriving as
  agent-driven `session/update` `tool_call` frames. `kiln/agent/acp` is
  deleted with no deprecation shim — kiln is not a production surface, so
  a shim would carry cost for nobody. `kiln acp` attaches no model
  provider yet: prompts are journaled and refused with a pointer at
  `kiln mcp` and the panel, and `kiln/acp.WithProvider` is the embedder
  seam.

- **Worktree isolation honors an explicit `PORT` under
  `GOFASTR_ISOLATION_REWRITE=0`** (#268): the app's own listen address
  (`App.Start`) now respects that knob the way child-env rewriting
  already did, so an operator can keep DB/worktree isolation while
  serving on the port they assigned. Isolation still remaps by default
  (the collision-avoidance whole point); `App.Start` already warns when
  it remaps an explicitly-set address, and the worktree auto-activation
  + remap is now documented prominently in isolation.md instead of only
  in the source.

- **The last 23 legacy `framework/ui` files (24 components) join the
  ExtraAttrs sanitization contract** (#262, completing the migration
  PR #261 started): AnimatedCounter, Banner, Button, Carousel, Container,
  FilterToolbar, Gallery, Link, LinkButton, Markdown, Menu items,
  NotificationBell, NumberInput, PasswordInput, ProgressSteps,
  RangeSlider, SearchInput, Select, Slider, TextArea, Timeline,
  TableOfContents, Toolbar, and Workbench now route caller extras
  through `html.SafeExtraAttrs` (Button and Link through the carrier
  variant below). A caller-supplied ExtraAttrs key can
  no longer override the attributes a component derives from its
  config (a Slider's `min`/`max`/`step`/`value`, a FilterToolbar's
  sanitized form `action`, a TextArea's `maxlength`, a Workbench's
  CSP-safe `style`, …) or spoof `class`/`id`/`data-fui-*` — including
  case-variant keys (`"Type"`), which slipped past PasswordInput's
  lowercase-only re-assert, and the owned keys (placeholder, method,
  role, aria-label) that SearchInput's and FilterToolbar's hand-rolled
  guards never covered at all.
  Set owned values through the config fields instead. `ui.Form` stays
  raw by documented contract. `extraAttrsRawLegacy` in the contract
  test shrinks to that one entry.

- **`ui.Button` and `ui.Link` are documented wiring carriers**: they
  keep forwarding `data-fui-*` (via the new `html.SafeCarrierAttrs`,
  which still drops `class`/`id`/the component's owned keys/the
  `data-fui-comp` style marker) so the
  `interactive.Action.Attrs()`-into-ExtraAttrs pattern from
  interactive-patterns.md keeps working — the resource UI's
  delete/transition buttons, the admin battery, and uinoderender's
  href-fallback + `data-fui-rpc` ActionRef links depend on it.

### Added

- **MCP server-initiated notifications** (#287):
  `notifications/tools/list_changed`,
  `notifications/resources/list_changed`,
  `notifications/prompts/list_changed` and `notifications/resources/updated`,
  plus `resources/subscribe` / `resources/unsubscribe`. `initialize` now
  advertises `listChanged` and `subscribe` truthfully; both were hardcoded
  `false`. The SSE GET stream previously wrote one static `endpoint` event and
  returned, so there was no subscriber machinery at all — it now holds the
  connection and streams.

  Notifications are filtered **per subscriber, at delivery time**. A gated
  resource's `updated` notification carries its URI, so it reaches only
  streams whose caller passes the resource's own `WithResourceGate` — the
  same gate that refuses the read. That does not hide the resource from
  `resources/list`, whose metadata stays listed by design (the gate guards
  contents); it keeps update notices from callers who cannot read the
  result. Tools, prompts and resource templates are the other shape: those
  gates hide the item from its list method and page the post-gate set, and
  the `list_changed` a gated tool or resource template registration fires
  is withheld from callers the gate refuses. Payload-free `list_changed`
  still requires passing the server-wide gate, because a caller refused
  wholesale should not learn that something changed. Delivery-time
  evaluation means a session revoked mid-stream stops receiving
  immediately, and app gate code never runs on the publisher's goroutine.

  A subscriber that falls behind is dropped and its stream closed rather than
  blocking the publisher: `list_changed` is idempotent, so a reconnecting
  client re-lists and is correct again, whereas a blocked publisher would stall
  every other subscriber and the code that raised the notification.

  Two limits documented rather than solved: subscriptions are refcounted per
  URI rather than per stream, because this transport has no session id linking
  a POST to a GET stream (the per-subscriber gates remain the boundary), and a
  notification raised on one replica does not reach clients connected to
  another — the same class of limit as the per-process cursor signing key.

- **MCP prompts, pagination and resource templates** (#287): `core/mcp`
  spoke tools and resources only. It now serves `prompts/list` and
  `prompts/get` (registered with `RegisterPrompt` and the same option
  shape resources use, including `WithPromptGate`) and
  `resources/templates/list`, and every list method accepts an optional
  `cursor` and emits `nextCursor` until the walk is done. Capabilities are
  advertised only once something is registered.

  Pagination pages the **post-gate** listing. Paging the unfiltered set
  and filtering afterwards would leak the existence of gated items twice
  over, through short middle pages and through cursor arithmetic; the
  test walks interleaved public and gated items at page size 2 and
  asserts every non-final page is exactly full. Cursors are HMAC-SHA256
  over a method-bound payload keyed by a per-server random secret, so a
  client cannot mint an offset it was never handed, move a cursor between
  list methods, or alter one it holds, and the payload carries the resume
  offset and nothing else — no total, no page size — so it cannot be read
  as an oracle for how many items exist. A tampered cursor is a clean
  invalid-params refusal, never a silent reset to page 1. A set smaller
  than one page serves the byte-identical old wire shape, so existing
  clients are unaffected. All six new methods join the server-wide gate.

  Known limit: the cursor key is per process, so a load-balanced `/mcp`
  rejects a cursor minted by another replica.

- **`core/acp`** (#287): the Agent Client Protocol as a package beside
  `core/mcp`, so something other than kiln can speak it —
  `initialize` negotiation, `authenticate`, `session/new`, `session/load`,
  `session/prompt`, `session/cancel`, `session/update` streaming (message
  chunks, `tool_call`, `tool_call_update`, `plan`) and
  `session/request_permission`. Absences are declared rather than
  silently dropped: `promptCapabilities` emits explicit `false` for image,
  audio and embedded context, `mcpCapabilities` explicit `false` for http
  and sse, and a request using one anyway is refused with `-32602` naming
  the type. Client filesystem and terminal methods stay unimplemented
  because they are client-side and this agent never calls them.

- **`pluginhost.Manifest.HostRequirements`** (#294): a plugin can declare
  that it needs a host-page permission, as `permissions-policy:<feature>`
  tokens against a closed allowlist, and `CheckHostRequirements` turns
  what used to be a runtime console error into a boot-time warning. The
  default headers deny camera, microphone and geolocation, so a plugin
  built on the host-mediated shape — host captures, sandboxed frame
  decodes — failed in the *host page* at the moment a user clicked the
  control, and nothing in the manifest could say why. The check warns only
  when every directive naming the feature carries the empty allowlist
  `()`, the one unambiguous deny-everywhere shape; `(self)`, `*`, origin
  lists and contradictory duplicates stay silent, because a check that
  fires spuriously is one nobody reads. It never fails a boot.

- **`pluginhost.Manifest.CSP` — an opt-in wasm tier for the framed
  sandbox** (#255, last of the three framed-runtime PRs): the framed
  Content-Security-Policy had no `'wasm-unsafe-eval'` and no extension
  point, so WebAssembly could not instantiate in a plugin frame at all.
  The pdf plugin runs pdf.js worker-free on the main thread to dodge
  that, and a plugin built on a wasm engine had to become a trusted
  host-page plugin, giving up the isolation the frame exists for. A
  manifest may now opt in against a closed allowlist with exactly one
  member, `'wasm-unsafe-eval'`, which widens `script-src` and nothing
  else: the opaque origin, `sandbox allow-scripts` without
  `allow-same-origin`, and `connect-src 'none'` all stay, so a wasm
  engine still cannot fetch, reach host cookies or DOM, or exfiltrate.
  Matching is exact, byte-for-byte — unlike the HTML sandbox attribute
  the CSP header does not normalise source expressions, so one
  comparison rejects every smuggle shape, including a token carrying
  `;` that could otherwise splice a directive into the response header.
  Thread it with `NewAssetServer(...).WithCSP(mod.Manifest.CSP)`; a
  manifest without the tier produces a byte-identical header. Only
  single-threaded wasm builds work here — multi-threaded builds want
  `SharedArrayBuffer` and COOP/COEP cross-origin isolation, which fight
  the opaque origin.

- **`ui.Button.AriaLabel` / `html.Button.AriaLabel`** (#281): an
  accessible-name override for buttons that share a visible `Label` but
  must be announced distinctly — a table of "Revoke" buttons, a
  dashboard's repeated actions. Button owns `aria-label` (an
  `ExtraAttrs` one is dropped), so before this there was no supported
  way to set it and repeated buttons announced identically; the admin
  RBAC revoke buttons and the live-dashboard demo now use it.

- **`pluginhost.MountConfig.Fallback`** (#253, second of the three
  framed-runtime PRs): a `render.HTML` slot for server-rendered
  pre-hydration content inside the plugin mount marker. The broker
  shows it while the frame loads, hides it — never removes it — when
  the frame reports `ready`, and swaps back to it on `bootError`, so a
  plugin with a Go-side renderer (the chart plugin's SSR SVG) degrades
  to its static output instead of an empty box, and works with
  JavaScript off. The fallback is host-trusted HTML built by the
  plugin's `Mount()`; plugins without one keep the frame visible while
  loading, unchanged.

- **Pluginhost bidirectional request channel** (#252, first of the
  three framed-runtime PRs): frame → host requests are now
  platform-owned, mirroring the host's existing `request()`. GoFastr
  ships the frame-side client for the first time —
  `frame/frameclient.js` (`window.__gofastrPluginFrame` with
  `ready`/`sendEvent`/`sendRequest`/`onEvent`/`onRequest`), served via
  `pluginhost.RegisterFrameClientRoute` with the framed CORP
  relaxation, or bundled via `pluginhost.FrameClientJS()`. Host
  adapters answer with `api.onRequest(method, handler)` or a static
  `registration.onRequest` fallback. One contract in both directions:
  every request is answered (`E_NO_HANDLER` / `E_HANDLER`, never
  silence), the in-flight map is bounded at 64 (`E_SATURATED`),
  invalid timeouts fall back to 5s, and teardown rejects outstanding
  requests with `E_TEARDOWN` instead of leaking hung promises. A
  non-cloneable payload (`DataCloneError`) on the request path cleans
  up its pending entry and rejects `E_SEND` on both sides instead of
  leaking toward the bound, and a non-cloneable handler *result* answers
  a cloneable `E_HANDLER` rather than hanging the caller. Plugins
  (richtext, datagrid) each hand-rolled their own correlation before
  this.

- **`ui.SiteHeaderConfig.PersistentActions`** (#256): a slot for the
  one journey-critical control (sign-in link, primary CTA) that stays
  in the bar at every viewport width instead of collapsing into the
  mobile drawer with the rest of `Actions`. It is never copied into
  the drawer, so no duplicate control exists at any width. Meridian's
  guest marketing header now keeps Sign in visible at 390px.

- **Porting guide** (#245 item 1, decided: escape hatch, not a
  supported use case): a new docs page, `porting.md`, shows how to move
  an app with a foreign DOM contract onto GoFastr — html/template
  screens rendered as `render.HTML`, the compiled design system served
  via `static.Mount` — and states plainly that markup and stylesheets
  brought this way are outside the one-styling-surface contract: the
  app owns the drift.

- **`ui.MenuItem.Confirm`**: pre-flight confirmation for RPC menu
  items, emitted as `data-fui-confirm` alongside the item's
  `data-fui-rpc` wiring. Previously the Danger field's doc pointed at
  an attribute there was no way to set once item extras were
  sanitized.

### Fixed

- **`data-fui-confirm` fires on plain form submits** (#279): the
  attribute was only read in `_dispatchRPC`, which needs `data-fui-rpc`
  on the node, and a plain POST form leaves the submit bridge at the
  enctype check. So the gate never ran: the admin process-modules screen
  revoked a capability and disabled a module on the first click, and so
  did any app that put the attribute on a plain form. The gate now runs
  before every branch of the submit bridge, covering native,
  `data-fui-spa`, and `data-fui-rpc` forms alike, and the widget-scoped
  listener gets the same treatment. A submit button's message takes
  precedence over the form's, since one form can carry several submit
  buttons of different destructive weight. The core gzip budget moves
  14700 → 14784: the gate cannot be carved into a demand module, because
  a native submit navigates away before one could load.

- **Generated API docs describe routes that exist** (#266, sibling of
  the v0.73.0 banner fix): `/openapi.json` and `/api/llm.md` documented
  auto-CRUD endpoints for entities whose routes registration never
  mounted (no DB attached, or in llm.md's case even `Exposure.CRUD=
  false`). Both generators now take the app's route predicate: CRUD-less
  entities keep their declared custom endpoints (unlinked in the llm.md
  index — the per-entity page rides the CRUD mount), and entities with
  no routes at all are omitted. **BREAKING for direct callers**:
  `openapi.EntityOpenAPI` and `crud.RegistryLLMMD`/`RegistryLLMMDHandler`
  gained a `crudMounted func(*entity.Entity) bool` parameter — pass nil
  for the previous Exposure-only behavior (the `framework.EntityOpenAPI`
  re-export is unchanged and passes nil).

- **`battery/auth` treats emails as case-insensitive end-to-end**
  (#270): `Owner@Example.com` and `owner@example.com` were two
  different accounts — login failed across casings, OAuth created
  duplicate accounts on a case-mismatched provider email, and the
  rate limiter lowercased while the lookup three lines later did not.
  Every flow (login, registration, magic links, OAuth matching,
  password reset, limiter keys) now canonicalizes via the new exported
  `auth.CanonicalEmail` at its ingestion point, so custom `UserStore`
  implementations receive canonical input too. **Stores with existing
  mixed-case rows need the one-time migration in auth.md** (collision
  check first, then `LOWER(email)` rewrite + expression unique index).

- **`ui.SiteFooter` link tap targets reach the 24px AA floor** (#257):
  inline 13px/1.6 anchors gave a ~17px hit area that padding could not
  extend; footer links are now `inline-flex` with `min-block-size:
  24px` (the design system's documented dense-cluster AA relaxation),
  pinned by a rendered 390px measurement.
- **Plugin frames get the full theme palette** (#271): the broker
  discovered token names by walking `document.styleSheets`, which
  raced stylesheet parsing (a sheet still loading contributed no
  names) and skipped `@media`/`@supports`-nested declarations — either
  way a PARTIAL palette bridged, giving dark text on the frame's light
  fallbacks. The broker now reads the theme's canonical vocabulary
  (`style.TokenNames()`) from computed style unconditionally; a Go
  test pins the JS list against the theme so a new token cannot
  silently go unbridged.

- **Stateless value screen components render again** (#259):
  registering a component by value (the documented stateless form) died
  at render time with a generic "di: target must be a non-nil pointer".
  DI is now skipped for value components; a value struct that *asks*
  for injection via `inject` tags fails with the type and the
  register-a-pointer remedy named.
- **`http.NewResponseController` deadlines reach the connection through
  the logging wrappers** (#260): `middleware.Logging`'s and
  `battery/log`'s response writers gained `Unwrap`, matching the
  cors/metrics/tracing wrappers, so streaming handlers behind an access
  logger can set per-write deadlines instead of getting a silent
  `ErrNotSupported` (a half-open client then pinned the goroutine).
- **Silent clobbers now scream** (#258, #268): replacing a router's
  NotFound handler warns (a UI host dispatches every screen through it,
  so the replacement disabled all pages; `Router.WrapNotFound` is the
  compose path, `uihost.WithNotFoundScreen` the supported custom 404),
  and worktree isolation remapping an explicitly-assigned listen
  address warns with both addresses and the kill switches instead of
  banner-printing the remapped port as if it were the configured one.

## [0.73.0] - 2026-08-28

### Fixed

- **`ui.SearchInput` with `Action` lost its styling** (#239): the
  stylesheet keyed every rule on the `data-fui-comp` marker as an
  ancestor, but `WrapHTML` injects the marker into the outermost tag —
  the `<form>` once `Action` is set — so the box shell landed on the
  form and the label went unstyled (a ~180px clipped input inside a
  bordered full-width form). Rules now key on the `.ui-search-input`
  class, which the label carries in both variants; a browser layout
  measurement reproducing the issue's repro pins the regression.
- **Startup banner advertised URLs that 404** (#244): the banner
  printed an API URL for every registered entity, including
  `Exposure.CRUD=false` entities and apps with no DB, whose routes are
  never registered. It now uses the same predicate as route
  registration (shared `entityCRUDEnabled`) and prints a summary line
  for entities registered without API routes.
- **Deterministic output where map iteration order leaked into bytes**:
  island action attributes and injected attrs (`core-ui/interactive`),
  non-canonical widget slot DOM order, `uihost` action-JS concatenation,
  menu item extra attributes, SMTP and logged email header order, the
  in-memory search battery's field blob, and kiln's world snapshot for
  the agent prompt all iterate sorted keys now. Same render, same
  bytes — screen caching, gzip dedup, hydration diffs, and LLM prompt
  caching stop churning on Go's randomized map order. Found by the new
  repo analyzers (`internal/analyzers`, run via `make analyze`, the
  pre-commit hook, and CI's vet step).

### Added

- **WebMCP bridge (experimental)** (#267): `framework/experimental/webmcp`
  exposes server-declared tools to in-browser agents through the Chrome
  WebMCP origin-trial API (the page's `modelContext`). Declare tools
  (name, description, JSON Schema, same-origin endpoint) and `Mount`
  serves a bridge script that registers them; each `execute()` proxies
  to its endpoint with the visitor's session cookies and the app's
  double-submit CSRF token, so an in-browser agent acts strictly as the
  signed-in user. Separate from the server MCP surface by design
  (session auth vs token auth). No-op on browsers without the API;
  automation enables it with `--enable-blink-features=WebMCP`.
  Reference: `framework/docs/content/webmcp.md`.

- **`gofastr generate cli --from-openapi <file|url>`** (#240): generates
  the terminal client from an OpenAPI 3 document instead of the entity
  set, for apps with hand-written APIs. One subcommand per
  `operationId` (missing/duplicate ids fail, no auto-naming), typed
  flags from parameters and JSON object bodies, `--file` for binary
  bodies, raw streaming of non-JSON responses, `servers[0].url` as the
  default URL, bearer/apiKey security wired into the existing
  `--token`/`login` machinery. The generated tree is self-contained
  (stdlib-only `internal/client`), and `custom.go` works unchanged.
  The importable `--extend` half of #240 is deferred until a second
  consumer outgrows `custom.go`.

- **Per-route request timeouts** (#246): `Router.SetRouteTimeout(method,
  pattern, d)` overrides `AppConfig.RequestTimeout` for one route, and
  `group.SetTimeout(d)` for every route under a group — route beats
  group, nearest group wins, `router.NoTimeout` exempts entirely. The
  resolution is stamped on the request before the middleware chain runs,
  so apps that never configure an override pay nothing. When the
  deadline fires, the 504 now logs a structured `request timeout` line
  naming the method, path, matched route pattern, and budget.

- **`ExtraAttrs` on every `framework/ui` component Config** (#251): the
  94 component configs that lacked the field (Lightbox included) now
  forward extra attributes (`data-*` test hooks, analytics markers,
  ARIA overrides) to the component's root element, with
  component-owned keys dropped case-insensitively — `class`/`id` (use
  `Class`/`ID`), `data-fui-*`, and behavior-critical attributes like
  `href` and form-control `type`/`name` — via the new
  `html.SafeExtraAttrs` helper. Charts forward to the shared zero-data
  placeholder too. Two source-level gates
  (`framework/ui/extraattrs_contract_test.go`) fail the suite if a
  component Config ships without the field or if new code forwards
  extras unsanitized. The 24 components whose pass-through predates
  this contract still forward raw and are enumerated on the gate's
  legacy allow-list (#262 tracks unifying them).

### Changed

- **BREAKING: `ToggleConfig.ExtraAttrs` (Checkbox / Radio / Switch) now
  follows the sanitized contract**: extras land on the root `<label>`
  instead of being copied raw onto the `<input>`, and `class`/`id`/
  `for`/`data-fui-*` keys are dropped. The previous raw copy was
  documented for `data-fui-*` RPC wiring but had no known callers; if
  you relied on it, move RPC wiring to the enclosing form or widget.

## [0.72.0] - 2026-08-27

### Added

- **Agent-readiness wave for the is-agentic class of scanners** (the
  industry successor to the isitagentready checks the framework already
  passes 11/11). Everything maps to a standard, not a scanner quirk:
  - `uihost.WithOrganization` embeds schema.org Organization JSON-LD
    (contactPoint + PostalAddress) in every full page head.
  - `AgentReadyConfig.WhenToUse` and `.CLI` render "## When to use" and
    "## CLI" sections in `/llms.txt`; the index now also auto-links the
    configured OpenAPI endpoint and MCP endpoint.
  - `/.well-known/mcp.json` is served whenever the MCP server is
    mounted, naming the `/mcp` endpoint and its streamable-http
    transport in both manifest conventions.
  - 404s content-negotiate: `application/problem+json` (RFC 9457) under
    a JSON Accept — and any miss under the API namespace answers
    problem+json regardless of Accept (`core/router.WrapNotFound` is
    the new seam) — plus a markdown recovery body under `text/markdown`
    when content negotiation is enabled. Neither machine arm reflects
    the request path.
  - `TestAgentReady_IsAgentic` pins all twelve reproducible checks the
    way the old scorecard pins the eleven.

### Fixed

- **Accept-negotiated responses carry `Vary: Accept`.** The markdown
  content negotiation served two representations of one URL with no
  Vary member, so a shared cache could hand the markdown variant to a
  browser (or the HTML variant to an agent). Both page variants and
  every 404 arm now emit it.

### Fixed

- **framework: a Shutdown during a pre-listen phase is a graceful nil.**
  The v0.71.2 startup-race fix covered a Shutdown landing after the
  phases but before the server; a Shutdown landing DURING a phase still
  surfaced as a Start error, because cancelling the lifecycle context
  made the running phase fail with `context canceled` (seen on main CI
  as `run seeds: … record ledger: context canceled`; a SIGTERM
  mid-migration would report the same way). Start's abort funnel now
  returns nil when the phase error is the app's own shutdown
  cancellation, pinned by a deterministic mid-seed regression test.

## [0.71.2] - 2026-08-26

### Fixed

- **framework: a Shutdown during startup stops Start.** `App.Shutdown`
  racing `App.Start`'s pre-listen phases (migrations, seeds, plugin
  init) found no server to stop, drained the batteries, and returned —
  and Start then adopted the listener and served forever with nobody
  left to stop it. Start now checks the cancelled lifecycle context
  under the adoption lock, re-drains, and returns nil like any graceful
  shutdown. This was the intermittent 30s race-job hang in
  `TestAppStart_WiresRunSeeds`; the window is now pinned by a
  deterministic regression test.

- **framework/ui: Card footers pin to the card's bottom edge.** A
  config-only card (Heading/Description/Footer, no body children) has
  no flex-stretch element inside, so in a stretch context — a `ui.Grid`
  row, an align-stretch flex parent — the footer floated mid-card with
  dead space beneath it and sibling cards' footer rows misaligned. The
  footer now carries `margin-top: auto`, and the linked variant's
  `.ui-card__inner` wrapper grows to fill the stretched anchor so the
  pin reaches the card edge there too (CodeRabbit caught the linked
  sibling). A chromium-tagged layout test measures all three shapes'
  pinned edges in a real Grid.

## [0.71.1] - 2026-08-25

### Fixed

- **battery/relay: trailing-slash tails are forwarded, not rejected.**
  The tail validator treated any empty path segment as the `//`
  smuggling shape, which 400'd the trailing-slash paths vendor SDKs
  post to (posthog-js sends `/i/v0/e/` and `/e/`). One final empty
  segment is now allowed (mid-path empties still refuse) and the
  validated trailing slash survives the upstream join, so upstreams
  that distinguish `/e` from `/e/` see the path verbatim.

## [0.71.0] - 2026-08-25

### Added

- **uihost: host-page scripts registered before serving.**
  `RegisterExternalScript` adds a same-origin external `<script src>` to
  every full-page render, emitted just before `</body>` after runtime.js.
  It exists for plugins and batteries wiring in `Init`, after
  construction-time options froze, and refuses registration once a page
  has been served so a script cannot ship on some pages and not others.
  `ScriptHandler` serves the bytes with the framework's versioned-script
  policy (strong ETag, immutable on a matching `?v=`), and `ScriptURL`
  produces that hash-versioned URL.
- **battery/relay: first-party relay for third-party services.** A
  declarative reverse proxy: one mount path, a fixed route table of
  prefix → upstream, per-route method allow-lists and body caps. Vendor
  scripts and beacons become same-origin, so the strict default CSP
  needs no script-src/connect-src exceptions and domain-based ad
  blocking no longer severs a page from its analytics. Not an open proxy
  by construction: upstreams are fixed at `New`, credentials are
  stripped both directions, inbound `X-Forwarded-*` is never trusted,
  and upstream redirects are refused rather than leaked.
- **Analytics recipes** (`framework/docs/content/analytics-recipes.md`):
  the first-party pattern for PostHog and Statsig end to end — relay
  route tables, a host-authored bootstrap served via
  `ScriptHandler`/`ScriptURL` and registered with
  `RegisterExternalScript`, pageviews on the runtime's `gofastr:navigate`
  event, a same-origin identity endpoint over `handler.GetUser`, and a
  `featureflag.Store` adapter that surfaces the vendor's boolean gates
  server-side.

## [0.70.0] - 2026-08-24

### Added

- **BREAKING: Go 1.27.** The toolchain directive names `go 1.27.0`, so this
  is the minimum toolchain that can build the module. Request ids and
  entity keys use the standard library `uuid` package instead of hand-rolled
  RFC 4122 bytes, six `LastIndex` slicing sites become `strings.CutLast`, and
  the runtime's `goroutineleak` profile is surfaced three ways:
  `goroutineLeaks` in `/.debug/stats`, `/.debug/goroutineleak` for the stacks
  (auth-gated), and an `app_goroutine_leaks` MCP introspection tool. In-memory
  timing tests run in `testing/synctest` bubbles, which takes `core/stream`
  from 35.7s to 13.3s; tests touching a database, Redis, or a real socket stay
  on the real clock. The `go fix` modernizers are applied across the tree,
  with the `embedlit` analyzer excluded because it corrupts source (see below).

### Known issues

- **`go fix`'s `embedlit` analyzer corrupts source in Go 1.27.0.** Rewriting a
  composite literal with an embedded struct field, it deleted the field name
  and its closing brace and left unparseable Go. One file in 571, which makes
  its other edits untrustworthy by the same token. The sweep here runs with
  `-embedlit=false`, and every changed file is checked to parse afterwards,
  which is how it was found.

## [0.69.0] - 2026-08-23

### Added

- **`gofastr verify` catches a theme token that does not exist
  (`GOFASTR1806`).** An invalid `var()` is not a CSS error. It resolves to
  nothing, the browser drops the declaration, and nothing reports it: not the
  build, not the console, not the linter. One reporter wrote `--radius-lg`
  where the theme emits `--radii-lg`, and every rounded corner on the site
  rendered square for days. The typed theme already checked the definition,
  since a `style.Radius` is compiler-checked, but a reference in a stylesheet
  is just a string and nothing connected the two. `contracts.Pass` now
  discovers `.css` files alongside Go source and hands them out through
  `StyleFiles()`; `Files()`, `AppFiles()`, and `TestFiles()` stay Go-only, so
  no analyzer that assumes Go source ever sees one. The rule reports a
  `var(--name)` whose name the theme does not emit and names the nearest token
  when one is within an edit distance of two. It stays quiet for a reference
  carrying a fallback, because `var(--x, 8px)` degrades instead of dropping
  the declaration; for a custom property declared in any scanned stylesheet;
  and for the `--ui-*` and `--fui-*` component knobs. A `--name:` counts as a
  declaration only inside a rule block and outside parentheses, so the
  condition of a feature query (`@supports (--brand: red)`, which asks whether
  the browser can parse that declaration rather than making one) no longer
  marks the token known; `@property --name` registrations do count. The
  `var(` and `@property` keywords match case-insensitively, as CSS does, while
  the token name stays case-sensitive, as CSS also does: `VAR(--radii-lg)`
  resolves, and `var(--mixed)` against a declared `--Mixed` does not. `style.TokenNames()` is
  the manifest it checks against, built from the same reflection walk that
  emits the CSS, so the two cannot drift apart. The scan is string-aware:
  `content: "var(--x)"` is content rather than a reference, and a quoted `/*`
  does not open a comment that swallows the rest of the file along with the
  real typos in it. A stylesheet can waive it with
  `/* gofastr:allow(GOFASTR1806) reason */`, which meant teaching the
  suppression scanner to read `.css` files at all: it walked Go source only,
  leaving the one rule that fires on stylesheets with no escape hatch in the
  file it fires in. The catalog is now 51 rules.

- **`Layout.WithStickyHeader()` pins the header wrapper the layout emits.** A
  header component given `position: sticky; top: 0` stuck for exactly its own
  height and then scrolled away. `app.Layout` renders the header component
  inside a `<header role="banner">` of its own, and a sticky element only
  travels inside its parent's box, so the component was sticky inside a box
  65px tall. It looked deliberate, which is why it survived so long. The fix
  belongs in the layout: the app-side workaround is a project CSS rule against
  a wrapper the app does not own. `WithStickyHeader()` adds a
  `layout--sticky-header` modifier and `LayoutBaseCSS` sticks the wrapper on
  the theme's `--z-sticky` layer, with a background from
  `--ui-layout-header-bg` so page content does not scroll visibly through it.
  The wrapper and its banner role stay. The sticky shell is also a flex column
  that fills the viewport rather than exceeding it: `.layout-body` carries
  `min-height: 100vh`, so a header above it made a short page scroll by exactly
  the header's height with nothing in the overflow, which under a pinned header
  reads as broken. `layouts.md` now writes down what each layout slot is wrapped
  in, which is the fact whose absence cost the time.

- **`gofastr:beforenavigate`, a cancelable event the SPA router fires before
  it takes a click.** A userland click handler that checked
  `e.defaultPrevented` and stood down, which is the polite thing for a
  userland handler to do, never ran: the router had already called
  `preventDefault` on its way past. Winning meant a capture-phase listener
  plus `stopPropagation`, racing the router rather than cooperating with it.
  The router now dispatches `gofastr:beforenavigate` on the anchor (bubbling,
  cancelable) after every fall-through check, so it fires only for clicks the
  router would actually handle. `detail` carries `href`, the resolved `path`,
  the `hash`, and the `anchor`. A listener that cancels it claims the click:
  the router still suppresses the browser default, so a cancelled click never
  becomes a hard page load, and then does nothing else. No `pushState`, no
  partial fetch, no intercept overlay.

  The same guard now matches the `target` keywords the way HTML does,
  ASCII-case-insensitively: `target="_SELF"` names the current browsing
  context exactly as `_self` does, and comparing the attribute raw sent it
  down the skip path, turning a soft navigation into a full page load. The
  core runtime's level-1 budget moved 128 bytes (14336 to 14464) to carry the
  five bytes that costs, and the reason sits beside the constant: the usual
  answer of carving a feature into a demand module is the one thing
  unavailable for a guard that IS the click path.

- **`data-fui-activelink-skip` opts a nav link out of active-route
  highlighting.** The `activelink` module neither sets nor clears
  `aria-current` or `.active` on a link carrying it, at load or after
  navigation. The escape hatch for a link whose current-state belongs to
  something else: a hand-set attribute, app code, a signal binding. Both
  attribute tables now state what `activelink` writes (`aria-current="page"`,
  the ARIA-correct value for a page link, so an `[aria-current="true"]`
  selector will not match it), what it removes, and which links it leaves
  alone.

- **A theme with a partial dark palette says which tokens it left out.**
  `Theme.Colors` paints the light scheme and `Theme.DarkColors` re-declares
  the same custom properties under a dark preference, so on a page rendering
  dark, editing `Colors` changes nothing on screen. Both maps are valid Go,
  both compile, and nothing said which one was being read.
  `style.DarkPaletteGaps` returns the color tokens a non-empty `DarkColors`
  has no usable dark value for, missing keys and keys present with an empty
  value alike, and the UI host logs them once at boot. A missing key leaves
  the light value painting in dark mode, which is usually a contrast bug. An
  empty value is worse: an empty custom property is valid CSS, so
  `--color-surface: ;` overrides the light declaration and every
  `var(--color-surface)` substitutes to nothing, dropping the consuming
  declaration at computed-value time. The token paints transparent rather
  than light. An empty `DarkColors` is a light-only theme by design and stays
  silent. The framework theme now
  declares dark values for `code-surface`, `code-text`, and `code-border`
  matching its light ones, which was always the intent and existed only as an
  omission.

### Fixed

- **`gofastr dev` served project JavaScript and CSS that the browser then
  ignored.** Editing a project `.js` file and hard-reloading kept running the
  old code. `fetch('/nav.js', {cache:'reload'})` returned the new bytes while
  the executing script was the old one, and only restarting `gofastr dev`
  cleared it. Framework assets are content-versioned
  (`/__gofastr/runtime.js?v=…`), but project assets serve at plain URLs with a
  bare `Last-Modified`, which invites heuristic caching. Under `gofastr dev`
  they now go out with `Cache-Control: no-store`, from both the static
  directory and the embedded static FS. Production is untouched: with dev mode
  off, no header is set. The cost was out of proportion to the fix, because
  you do not doubt the cache, you doubt your own change and go looking in the
  wrong file.

- **`activelink` cleared an `aria-current` the scrollspy module had just
  set.** The module walks every `nav a` and removes `aria-current` from any
  link whose href is not the current path, which an in-page anchor never is. A
  scrollspy rail is a `<nav>` of `#section` links, so every navigation wiped
  the section indicator scrollspy had written. Links inside a
  `[data-fui-scrollspy]` wrapper are now left alone, the same hands-off rule
  href-less links already had. The `data-fui-scrollspy-target` rows in both
  attribute tables were wrong while this went unnoticed: it is a selector on
  the wrapper naming additional target elements, not a marker on a link.

- **GOFASTR1801 reported a Go short variable declaration as a stylesheet.**
  `fill := "var(--color-surface)"` failed the build. `fill` and `stroke` are
  CSS property names and legal Go identifiers, so the `:` of `:=` satisfied
  the property colon, `= "` filled the value gap, and the token reference
  matched the value shape. A CSS declaration never carries `=` directly after
  its colon, so a match containing `:=` is now rejected; the hyphenated
  properties need no such guard, since a hyphen cannot occur in a Go
  identifier. The diagnostic also names the fragment that matched, because the
  reporter read "a CSS rule is defined outside the design system" as being
  about the value and went looking at what they had assigned.

## [0.68.0] - 2026-08-22

### Added

- **Row-level read scoping.** `Exposure.ReadScope` narrows WHICH rows a caller
  may read. `Exposure.Access.Read` only decided WHETHER an entity was readable,
  so an entity was readable in full or not at all. That left the most ordinary
  content posture there is with no way to declare it: what an author wants is
  "anonymous visitors see published rows, signed-in editors see drafts", and
  before this there was no declaration for it. Three shipped example blueprints
  served their own draft and unapproved rows to anonymous callers, each with a
  `KNOWN EXPOSURE` comment saying so. A read scope carries predicates on the
  entity's own columns plus the permission that lifts them; a blank
  `unrestricted` means any signed-in caller reads everything, which is a weak
  posture and is documented as one. Predicates AND together and there is no OR
  form yet. Fields are checked at registration against the same rules defaults
  are, and a predicate on a `Hidden` column is refused because it would leak
  that column through the row set. One builder feeds every read: list data and
  count, get, cursor, stream, the in-process API, typed queries, the upsert
  read-back, and the include and eager loaders at every depth. A row outside
  the scope answers 404 rather than 403, so the caller does not learn it
  exists. Writes are deliberately not filtered: a caller with write-but-not-read
  can still update or delete a row they cannot see, and `RETURNING` on an
  upsert is not filterable without changing write semantics. Declared in a
  blueprint as `read_scope:`, which survives the pack round-trip; every level
  of it goes through the unknown-key check, because a typo in a security
  posture must fail the build rather than be ignored.

- **`gofastr migrate repair` rebuilds a table carrying a stale owner-column
  foreign key.** Releases before v0.67 emitted `FOREIGN KEY (owner_col)
  REFERENCES target(id)` for an entity declaring one column as both
  `Scope.OwnerField` and a relation. The framework stamps that column from the
  session identity, which lives in `auth_users`, so the key referenced a table
  where no matching row will ever exist and every create violated it. SQLite
  did not enforce keys, so it stayed invisible; Postgres always rejected it.
  v0.67 turned enforcement on and stopped emitting the clause, but neither
  helps a database that already has one, and SQLite has no `DROP CONSTRAINT`.
  The command reports by default and rewrites under `--apply`, naming every
  table before it touches one. The replacement DDL is the original minus the
  offending clause rather than a fresh `CREATE TABLE` built from the entity
  declaration, so columns, defaults, and constraints the declaration no longer
  mentions survive; indices and triggers are replayed. Other foreign keys keep
  enforcing, and a composite key that merely includes the owner column is left
  alone. `AutoMigrate` warns at boot naming the table and the command, because
  the failure otherwise surfaces as a bare constraint error on every create
  with nothing pointing at the cause.

- **The race detector covers the `framework` root package.** It holds `app.go`,
  `health.go`, and the process-module supervisor, the goroutine-heaviest code
  in the repo, and it was the one package the gate excluded. The exclusion was
  never about a data race: two suites whose deadlines do not absorb race
  instrumentation lived there, and they moved to `framework/uie2e` (chromedp)
  and `framework/processmoduletest` (the supervisor scenarios). The root
  package runs 22s under `-race`, down from 48s before the split.

### Fixed

- **A nested filter refused an owner filtering their own rows.**
  `?rel.field=` compiles to an `EXISTS` clause that counts rows without
  selecting them, so it could not narrow them by selecting, and the previous
  fix refused the shape outright for every owner-scoped or multi-tenant
  target. That closed the count oracle and took the ordinary case with it. The
  subquery now carries the caller's owner and tenant predicates, built by the
  same code the include and eager paths use, so the three surfaces cannot
  drift into three different answers. A caller holding a cross-scope grant
  gets no predicate on that axis, and the axes stay independent.

  **BREAKING for in-process callers.** `ListAll` and `CountAll` now apply
  those predicates to a `ListOptions.NestedFilters` spec on every call, not
  only over HTTP. The narrowing was previously gated on a context marker that
  no host could set: the setter is unexported and crud's three call sites hand
  it to the include loader, which never reaches `ListAll`, so the safe branch
  was unreachable and the default was the unsafe one. A host handler
  forwarding a caller-influenced relation into `ListAll` mid-request had a
  count oracle over rows the target's posture hides. Server-side code that
  means "across every owner" now says so with `owner.AllowCrossOwner` or
  `tenant.AllowCrossTenant`, the escape the in-process surface already had.
  The posture check — may this caller read the target at all — stays HTTP-only,
  because it is the baseline session gate and in-process code has no session;
  applying it there refuses every background nested filter and protects nothing.

- **A list filter narrowed the total but not the rows.** `filter.ApplyToQuery`
  reached the count query and not the data query, so `?field=value` returned
  the whole table under a correct-looking total. Nothing asserted that a filter
  narrows the ROWS, so the entire crud suite stayed green; an admin battery
  test one package over was what caught it. The two queries are now pinned
  against each other.

- **`repolint` stopped reading a file at a `/*` that was not a comment.** A
  `/*` inside a line comment or a string literal opened a block comment that
  never closed, so every later line went unscanned and the rule reported clean.
  A lint that stops reading reports the same "no findings" as a lint that read
  everything. Comments are recognised in one lexical pass now, tracking line
  comments, block comments, and all four string forms, with raw-string and
  block-comment state carried across the newline.

- **`make mutate` mis-parsed a pattern matching several packages.** `go list`
  prints one record per package and the parser read every line after the first
  as a file name, so `./framework/...` turned the second package's directory
  into a file and the run died with an error naming neither the wildcard nor
  the rule it broke. A multi-package pattern is refused with the directories it
  matched.

- **Three gates measured the developer's own environment.** The `gofastr
  migrate` dotenv-precedence tests asserted file precedence without clearing
  the process environment they consult first, so a developer with an exported
  `DATABASE_URL` ran them against a different database than CI did.


## [0.67.0] - 2026-08-19

### Added

- **`make mutate` finds guards no test distinguishes.** It breaks each
  conditional in a package one at a time, once so it can never fire and once so
  it always fires, and reports the ones the suite does not notice. A survivor
  is usually a test whose fixture trips several refusal conditions at once, so
  removing any one of them changes nothing; seven such tests were found by hand
  in a single review cycle, and this reproduces those findings mechanically.
  The mutation annotates the original condition (`if (cond) && false`) rather
  than replacing it, so every identifier stays referenced and the mutant
  compiles whenever the original did. The unmutated suite runs first, because
  an already-red package reports every mutant as caught; a mutant that fails to
  compile is reported as BROKEN, never as caught; and a mutation that does not
  change the file or reach disk is a hard error, not a verdict. Because it
  writes into real source files it takes a per-package lock, bounds each run
  with `-timeout`, and restores every file on interrupt. One full test run per
  mutant, so it takes a package argument rather than the repo.

- **The `gofastr init` scaffold now serves MCP.** A fresh app wires
  `framework.WithMCP()` and sets `Exposure.MCP` on the sample entity, so
  `/mcp` answers `tools/list` with the entity's five CRUD tools on first boot,
  and an unauthenticated `tools/call` returns the same 401 the REST route does.
  The boot banner names the endpoint and its tool count. MCP is the one mounted
  surface a user cannot discover by guessing a URL, and it was the only one of
  the advertised surfaces the banner left out.
- **`framework.DefaultDotEnvPaths()`** exposes the dotenv files `NewApp` loads,
  in precedence order, so code that must read an environment variable before
  `NewApp` can load exactly the same set. Loading a shorter list first pins the
  lower-precedence file's value, because `dotenv.Apply` never overwrites an
  existing variable.
- **`CrudHandler.CanReadRecordScoped(ctx, id)`** answers the read posture for
  ONE record. It matters when a resource-aware `Decider` allows the listing and
  denies a row: the collection-level predicate would let a detail screen render
  a record the read-one route refuses.
- **`CrudHandler.CanReadScoped(ctx)`** answers an entity's whole read posture as
  a boolean with no HTTP response: owner scoping, tenant scoping, the baseline
  session requirement, and RBAC. It is what any surface reading the same
  rows outside the CRUD routes should call. See
  [access control](framework/docs/content/access-control.md).
- **CI runs the race detector** over the packages that coordinate goroutines
  (queue, outbox, fanout, cron, event, hook, ratelimit, lifecycle, slowquery,
  cache, stream, uihost). `-race` was previously a local `RACE=1` opt-in and
  appeared in no gate. The `framework` root package is deliberately excluded:
  its chromedp and process-supervisor suites flake under race instrumentation
  for timing reasons, so the goroutine-heaviest code in the repo still has no
  race gate until those suites are split out. The workflow comment records why.
- **The generated-app gate exercises more than the homepage.** Every example
  blueprint is now driven past `GET /`: for each entity it compares the REST
  list route's authorization posture against the same entity's MCP tool, and
  requires that a screen for an entity whose REST list is refused renders the
  refusal rather than rows, checked positively, so a leak through any
  renderer fails it, not only one shaped like a data table.
- **Three repo lint rules.** `cmd-binary-not-ignored` fails when a `cmd/<name>`
  has no root-level `.gitignore` entry. `go build ./cmd/<name>` from the root
  drops the binary at `/<name>`, and the hand-written list of names had already
  rotted twice, most recently letting this change set's own 3.4 MB `/mutate`
  sit untracked. `front-door-missing` keeps the README linking the
  deployed docs site; `example-dead-origin` fails an example that advertises a
  hostname which is neither reserved for documentation nor actually served.
- **`gofastr validate` reports a colliding `app.module`.** A module path inside
  the framework's own module produces code that cannot build outside this
  repository. Nothing surfaced it before. `generate` even printed that same
  colliding path in its "Next steps" as the remedy.

### Fixed

- **BREAKING: foreign keys were never enforced on SQLite.** SQLite ignores
  `FOREIGN KEY` constraints unless `foreign_keys` is enabled per connection,
  and it is off by default: in the driver GoFastr ships, in
  `mattn/go-sqlite3`, and in SQLite itself. `AutoMigrate` emits a
  `FOREIGN KEY` clause for every declared relation, so an app read as though
  referential integrity held while a create that set `author_id` to an id
  naming no row inserted cleanly and left a permanent dangling reference.
  PostgreSQL enforced the same constraints all along, so identical
  application code got different guarantees per database. Every DSN opened
  through the `sqlite3` driver name now defaults to
  `_pragma=foreign_keys(1)`.
  **Upgrade:** a write that references a row which does not exist now fails
  instead of succeeding, and so does deleting a row that other rows still
  reference. `AutoMigrate` declares no `ON DELETE` action and
  `entity.Relation` cannot express one, so children must be deleted before
  their parent. Any app already holding dangling references will see errors on
  writes that touch them. Setting `_pragma=foreign_keys(0)` in the DSN restores
  the old behavior while the data is cleaned up; `_foreign_keys=0` and `_fk=0`
  work too, as does any spelling SQLite's PRAGMA grammar accepts
  (`foreign_keys(0)`, `foreign_keys = 0`, `foreign_keys=0`, with any whitespace
  or percent-encoding). A DSN that names the pragma without assigning it, a
  bare `_pragma=foreign_keys`, reads rather than sets, and still receives
  the default.
  **One upgrade case is worth checking before you flip this on.** If an entity
  declares the same column as both `Scope.OwnerField` and a relation, older
  `AutoMigrate` wrote a `FOREIGN KEY` for it, and the framework stamps that
  column from the session identity, which lives in the auth battery's table
  rather than in the related entity. The constraint was one the framework
  violated on every create, and SQLite tolerated it only because nothing was
  checking. This release stops emitting it, but it cannot remove one already
  written into a table: SQLite has no `DROP CONSTRAINT` and `AutoMigrate` has
  no table-rebuild path. Such a database needs `_pragma=foreign_keys(0)` until
  the table is rebuilt without the key. `evals/upgrade-fixtures` carries a real
  v0.53.0 app in exactly this shape and proves that upgrade path.
  This does not close the *permission* half of the same gap: nothing yet checks
  that the caller may read the row a relation column points at. See
  [security](framework/docs/content/security.md).
- **BREAKING: the in-house SQLite engine is removed.** `gofastr/sqlite` held a
  second, from-scratch SQLite that six `*_pure_sqlite_test.go` suites ran
  against as a cross check, on the theory that validating the framework's SQL
  against one engine lets an assumption baked into both look like correctness.
  The theory does not hold here, because modernc.org/sqlite is not a second
  reading of the SQL spec. It is the SQLite C source translated to Go. Whenever
  the two disagreed the answer was always that the local engine was wrong, and
  the changelog records no case of the cross check finding a bug in the
  framework's SQL against twelve entries fixing the engine itself. It was
  16,000 lines of engine and 21,000 lines of tests that only ever generated
  work. The suites that used it now open `sqlite3`, which is a stronger test
  than they had. `gofastr/sqlite/stdlib` is untouched and is what every app
  imports; only the `gofastr-sqlite` driver name and the `gofastr/sqlite`
  package are gone. **Upgrade:** if you opened `gofastr-sqlite` directly, open
  `sqlite3` instead. Nothing outside this repository's own tests did. The
  `sqlite-engine` doc topic is replaced by `sqlite-driver`, which documents
  what `sqlite/stdlib` actually does: the three DSN parameters it adds to every
  connection (`foreign_keys(1)`, `_time_format=sqlite`,
  `busy_timeout(5000)`), why each one is there, and why an in-memory database
  needs `SetMaxOpenConns(1)`. Those defaults were previously described only in
  code comments.
- **BREAKING: server-rendered screens bypassed entity read permissions.**
  `framework/ui/resource`'s `List`, `Table`, `Detail`, and pre-filled edit
  `Form` read rows directly, so they never entered the route middleware that
  enforces an entity's posture. A generated app whose `users` entity declared
  `read: users:read` answered 403 on `GET /api/users` and 200, with every
  row's email in the HTML, on the `GET /users` screen the same blueprint
  generated. The same gap served every row of a default-posture entity, whose
  JSON route answers 401. All four now check the read posture, `CanReadScoped` for
  `List`/`Table`, `CanReadRecordScoped` for `Detail` and the edit `Form`, and
  render an access notice instead of the rows. **An app whose screens rendered entity
  rows without a session will now show that notice.** To render for a
  sessionless visitor the entity has to be readable by one: `Public`, or
  `Access` with a blank `read:`. `Scope.OwnerField` does not help here. A
  caller with no owner in context still fails the check, so the notice stays
  and only owners see rows.
- **`?rel.field=` filtered the wrong table when an entity's name and table
  differ.** The `EXISTS` clause named the relation's registry KEY as its table,
  while every check the filter performs is made against the RESOLVED target:
  declared fields, hidden columns, soft delete, the owner/tenant refusal. An
  entity declaring `Table` different from its name (or a versioned
  registration) therefore validated one table and queried another: silently
  wrong rows where a same-named table existed, a 500 where it did not. The
  subquery now reads the resolved target's table, as the eager-load path
  already did.
- **A cross-owner include returned almost nothing instead of everything.**
  `applyRelatedOwnerScope` unconditionally pinned the owner column to the
  request's owner, with no branch for the cross-owner postures that
  `ApplyOwnerScope` honours: `owner.AllowCrossOwner` and the entity's
  `CrossOwnerRead` permission. A caller holding either saw every row on the
  routes and only their own through `?include=`. Same defect as the tenant one
  below, in the sibling helper.
- **A cross-tenant include returned nothing instead of everything.**
  `applyRelatedTenantScope` unconditionally pinned `tenant_id` to the request's
  tenant, with no branch for the cross-tenant posture that `RequireTenant` and
  `ApplyTenantScope` both honour. An admin reading across tenants got an empty
  relation rather than the rows they are entitled to. That is fail-closed, but
  contradicts the documented contract.
- **An include's authorization answer depended on whether the parent table had
  rows.** The posture check sat below an early return for an empty parent, so
  the same request answered 200 while the table was empty and 403 once a row
  existed, a table-emptiness oracle, and inconsistent with the nested-filter
  check, which refuses before it queries anything. The check now runs first.
- **BREAKING: `?include=` and `?rel.field=` reached past an entity's read gate.** An
  eager-load is a read of the RELATED entity, and the CRUD route applied every
  other guard that hangs off the include target, the hidden-column scrub,
  owner scope, tenant scope, soft delete, but never its `Exposure.Access`. So
  `GET /api/posts?include=author` returned whole rows of an entity whose own
  route answers 403, `?include=comments.author` dumped the table in one
  request, and the same held through `?fields=` and cursor pagination. Nested filters were the quieter half: `?author.email=…` did not
  return the row but changed the result count with the guess, making it an
  oracle over a column the entity refuses to serve. Includes now answer 403
  naming the entity, at every depth. Nested filters answer 403 for an
  unreadable target, and are also refused outright when the target is
  owner-scoped or multi-tenant: the `EXISTS` clause counts rows without
  selecting them, so it cannot narrow them to the caller, and a signed-in owner
  or a wrong-tenant caller could otherwise enumerate everyone else's rows one
  guess at a time, except for a caller who may already read every row of that
  target (`owner.AllowCrossOwner`, `CrossOwnerRead`, `tenant.AllowCrossTenant`),
  for whom the count reveals nothing new. To filter by a scoped entity's field,
  query that entity's own list route and filter the parent by the ids it
  returns. The subquery now also hides soft-deleted rows, which every other read
  surface already hid. Trashed values were enumerable the same way. An *include* targeting an entity you can read is
  unaffected. A nested *filter* additionally requires the target to be neither
  owner-scoped nor multi-tenant: an ordinary owner filtering their own rows
  gets 403 and must use the target's own list route, while a caller holding a
  cross-owner or cross-tenant grant is exempt.
  **Upgrade:** a request that previously returned 200 now returns 403 when it
  includes or filters across an entity the caller may not read, most often a
  default-posture entity reached from a `Public` one. Declare the related
  entity as readable by that caller: `Public`, or a blank `read:` beside real
  write permissions. `Scope.OwnerField` does NOT restore the old answer for an
  anonymous caller. The nested-filter gate still refuses, and an include
  returns 200 with zero related rows because the owner scope matches nothing.
- **The same screens served RELATED entities' rows.** Gating a screen on its
  own entity was not enough. Relation columns resolve the related entity's rows
  to display labels, reverse-relation sections list them outright, and
  dashboard `StatValue`/`groupCounts` aggregate over them. None checked the
  related entity's posture, so a public screen with a relation to a gated
  entity served that entity's display values, on the full page and the island
  fragment alike. `groupCounts` was the sharpest: it returns the grouped
  column's distinct values, so grouping a gated entity by `email` published the
  addresses. Each related read is now gated on the related entity: a relation
  the caller may not read renders muted, never the raw foreign key, which
  would be useless to a reader and disclose an internal id. A
  reverse-relation section is omitted rather than announced (a notice on a
  public page tells every visitor which entities exist), and an aggregate
  reports no data.
- **Generated apps showed "Sign out" to anonymous visitors.** The app shell's
  sidebar footer was built once at registration, so it could not see the
  session. It now resolves per request, and offers "Sign in" only when a screen
  actually hosts the login form. Six of the seven shipped blueprints never
  route `/login`.
- **Four example blueprints declare their public content read-open.**
  `examples/{portfolio,project-manager,lms}` moved their content entities
  (posts, projects, tags, testimonials, courses, modules, lessons, boards,
  columns, labels) from the default session-required posture to a blank `read:`
  beside real write permissions, and `examples/blog` tightened its posts and
  comments from `public: true` to the same shape. Their public screens
  previously rendered only because server-rendered screens bypassed the read
  gate; with that closed, each entity had to say what it actually is.
  Anonymous REST reads on those entities are now open by declaration, and every
  write stays gated.
- **The `examples/ecommerce` storefront reads open and gates every write.** Its
  catalog screens are public and previously rendered only because
  server-rendered screens bypassed the read gate; with that closed, the
  entities had to declare a posture. They now use a blank `read:` with named
  write permissions, the same shape as `examples/blog`, on both the blueprint
  and its runnable Go twin. `public: true` would have opened anonymous
  `DELETE` on the catalog and on user-submitted reviews, the exposure
  `gofastr generate` warns about on every run, which is not a tradeoff a
  storefront example should be teaching in a release whose subject is closing
  read holes. The shape the example actually wants, anonymous read with any
  signed-in user writing, still cannot be expressed: a blank `read:` alone
  leaves `AccessControl` empty (so `Declared()` is false and the session
  requirement still applies), and naming a permission does not help because a
  generated app grants permissions only to the admin role. Admin-gated writes
  are the closest expressible posture; the blueprint comment records the gap
  rather than papering over it.
- **Documented that a blueprint's `nav: role:` is visibility, not access
  control.** It hides the sidebar link; it does not gate the route. A blueprint
  screen cannot declare a role of its own, so a plain generated screen at a
  role-gated href answers 200 to anyone who types the URL, and every screen
  path appears in the route manifest embedded in each page. The docs previously
  said "the route stays protected regardless", which is only true when the
  destination has its own gate (entity `access:`, or the admin battery).
- **The docs site advertised a hostname with no DNS record.** `og:url`, the
  sitemap, `robots.txt`'s `Sitemap:` line, every per-page canonical, and the
  agent-ready discovery URLs pointed at a domain that resolves to nothing.
  Both the site and the Meridian example now derive their public origin from a
  single build-time variable.
- **`DATABASE_URL` in the scaffolded `.env` was ignored.** The scaffold read it
  before `.env` was loaded, so `PORT` applied and the database silently did
  not. The app opened a different database than the file named.
- **Three more places loaded a shorter dotenv list than `NewApp` does.**
  `dotenv.Apply` never overwrites a variable that is already set, so anything
  reading a shorter list first pins the lower-precedence file's value and
  `NewApp`'s higher-precedence `.env.<APP_ENV>` can no longer win. Blueprint-
  generated `main.go` and the two generated test files loaded
  `.env.local, .env`; `gofastr migrate` resolved `DATABASE_URL` from `.env`
  alone, ignoring `.env.local`, the *highest*-precedence file and the
  documented place to point a checkout at a local database, so the CLI
  migrated one database while the app it was migrating for opened another.
  All four now load `framework.DefaultDotEnvPaths()`.
- **`gofastr pack` silently lost the sidebar nav of every auth-enabled app.**
  It reconstructs nav by reading `sidebarConfig`'s AST and required the
  function to be a single `return ui.SidebarConfig{…}`. Once that builder
  resolved its auth control per request it became `cfg := ui.SidebarConfig{…}`
  … `return cfg`, the reader's type assertion failed, and pack reported no nav,
  indistinguishable from an app that declares none. It now follows a
  returned identifier back to the literal assigned to it.
- **Meridian's sidebar showed "Sign out" to everyone.** The app shell built
  its sidebar footer once at registration with a hardcoded sign-out control,
  the same bug fixed in the generator; meridian is hand-maintained, so it did
  not inherit the fix. Its footer now resolves per request, and the role-gated
  "Admin" item is filtered through the context-aware path rather than rendered
  for every visitor.


## [0.66.0] - 2026-08-17

### Fixed

- **Mixed-case `Content-Type` defeated the multipart body cap.**
  `isMultipart` matched the `multipart/form-data` prefix case-sensitively
  while the content-type gate parsed per RFC 9110, so
  `Multipart/Form-Data` requests passed the gate but took the JSON path,
  and its 1 MiB cap. Both checks now parse the media type the same way.
- **`pagination.OffsetForPage` panicked on a zero page size.** The
  overflow guard divided by `limit`; `limit == 0` is now clamped to
  offset 0 alongside `page < 2`. No in-repo HTTP path could reach it, but
  the function is exported.
- **A comma bomb in `?field_in=` allocated the full list before the cap
  check.** `filter.SplitINValuesBounded` counts entries first and never
  materializes more than cap+1, while the over-cap 400 still reports the
  exact entry count; both the top-level and nested (`?rel.f_in=`) paths
  use it.
- **BREAKING: a stale queue worker could delete another worker's job.**
  `Queue.Ack` and `Queue.Nack` now take a `Job` instead of a job ID
  string, and `RedisQueue` verifies `Job.ClaimToken` before acting. The
  old signature carried no claim identity, so this sequence lost a job
  outright: worker A claims job J, A stalls past its lease, worker B
  re-claims J, A wakes and Acks. A's Ack deleted the processing entry
  that belonged to B, and when B then crashed the job was on no list, in
  no hash, and had never been dead-lettered. A mismatched token is now a
  no-op counted by `StaleClaimCount()`. Update call sites to pass the
  `Job` returned by `Dequeue`.
- **A worker crash on a job's final attempt stranded it forever.**
  `DBQueue` reclaimed expired leases only while `attempts <
  max_attempts`, but `Dequeue` had already incremented `attempts`, so a
  crash on the last permitted attempt left the row `claimed` with
  nothing able to see it, including `Replay`. An expired lease on the
  final attempt is now dead-lettered and logged.
- **`DBQueue.Ack` retires only a live claim.** The delete had no status
  predicate, so the stale worker whose crash caused a dead-lettering
  could wake, `Ack`, and erase the very row that made the failure visible
  to `Stats`, `ListJobs("failed")`, and `Replay`. It now matches
  `status='claimed'`. The route from `failed` to gone is `Replay`, claim,
  `Ack`, the same path an operator already used. This also aligns the
  three backends: `MemoryQueue` and `RedisQueue` already no-op an `Ack`
  for a job they do not have claimed.
- **Multipart uploads over 1 MiB always failed.** The CRUD write
  handlers wrapped every request body in a 1 MiB `MaxBytesReader` before
  deciding whether it was multipart, so `ParseMultipartForm` read
  through a cap 32× smaller than the 32 MiB it was asked for. JSON
  bodies still cap at `MaxJSONBodyBytes`; multipart bodies now cap at
  the new `crud.MaxMultipartBodyBytes` (64 MiB) and a request over it
  gets a 413 instead of a 400.
- **Cursor pagination re-served rows whose sort key held invisible
  characters.** `EncodeCursor` kept the raw keyset value while
  `DecodeCursor` stripped zero-width and bidi codepoints, so paging
  resumed before the row it had just emitted. Values now decode
  verbatim; field names, which reach SQL as identifiers, are still
  scrubbed.
- **Repeating a `?field_in=` key dropped every occurrence but the
  first.** `?tag_in=a,b&tag_in=c` filtered on `a,b` alone and said
  nothing. All occurrences now union, and the 1000-entry cap counts the
  union. The same cap now applies to relation-scoped lists
  (`?author.name_in=`), which had no cap at all.
- **`?page=` near `MaxInt64` wrapped to a negative offset.** The guard
  in `framework/pagination` existed but nothing called it. It is now
  wired into the buffered list, the streaming list, and the admin table.
- **Two users uploading the same filename overwrote each other.** The
  standalone `upload.Handler` keyed objects on the sanitized client
  filename with no unique component, and `LocalStorage.Save` opens
  `O_TRUNC`. Keys now come from `upload.UniqueFilename`, matching what
  the auto-CRUD path already did. Read `key` from the response rather
  than rebuilding it from `originalName`.
- **The blog blueprint generated an app that would not boot.**
  `examples/blog/gofastr.yml` seeded `post_id: 1` against UUID string
  primary keys; the generated app compiled and then died at startup with
  `seed comments: validation failed`. `gofastr generate` now rejects a
  seed value whose type cannot satisfy its target field and names the
  `@entity.field=value` form to use instead. The blueprint uses that
  form.
- **A failing seed said only "validation failed".** The generated seed
  hook discarded `crud.ValidationError.Fields()`, where the per-field
  messages live. It now reports the entity, the row, and each field's
  message.
- **Auth-gated blueprint screens panicked under strict mode.** The
  generator chained `WithTitle` but not `WithDescription` when mounting
  a screen behind a policy, so every screen with a declared description
  tripped `uihost strict mode: screen "…": no description`. Meridian's
  generated app could not start.
- **The stability tier gate checked one package instead of 219.**
  `TestEveryPackageIsClassified` ran `go list ./...` from its own
  package directory, so it saw only `stability` itself and an entire
  unclassified top-level tree would have shipped green. The enumeration
  runs from the module root, and `TestGateSeesWholeModule` fails if it
  ever narrows again.
- **Every contract diagnostic linked to a domain that does not resolve.**
  `contracts.docBaseURL` pointed at `gofastr.dev`, which has no A
  record, so each `HelpURI` was dead. It points at the published docs
  site.
- **Eight documentation claims contradicted the code.** `multi-tenant.md`
  taught reading the tenant from a request header. `TenantMiddleware`
  ignores any client header precisely because trusting one lets a caller
  impersonate a tenant, so the page described a vulnerability the code
  refuses to have. Also corrected: the webhook secret codec default,
  the idempotency middleware's position, `EnableMCP`'s tool count,
  anonymous subjects at partial rollout, batch rollback's `data`
  scrubbing, the `TrustTrusted` tier name, and the PWA cache-name
  format.
- **Documented Go examples that could not compile.** Enabling the
  compile gate on 83 more blocks surfaced ten of them, including
  `new(0)` for a `*time.Duration`, `Post` given an `http.HandlerFunc`,
  and `q.Nack(ctx, job.ID)`.

### Added

- **Every shipped blueprint is now booted, not just compiled.**
  `TestExampleBlueprintsBoot` generates each `examples/*/gofastr.yml`,
  compiles it, starts the binary, and fails if it exits or never
  serves. The ladder stopped at compilation before, which is why a seed
  that only fails at runtime survived. It caught the meridian strict-mode
  panic above on its first run.
- **`gofastr generate` reports every schema error at once.** Fixing a
  blueprint was a serial guess-and-recompile loop because validation
  stopped at the first bad key. Unknown top-level keys also get a
  location hint: `auth:` at the root suggests `app.auth:`.
- **The doc-example compile gate covers 85 blocks, up from 2.** Blocks
  opt in with a `gofastr:compile` directive, using the same mechanism as
  before.

## [0.65.0] - 2026-08-16

### Added

- **The release gate now checks SECURITY.md.** `scripts/release-gate.sh`
  refuses to publish a tag unless SECURITY.md names the released minor as
  the supported line (v0.64.0 shipped while the policy still said
  `0.63.x`; that class of drift now aborts the release instead).
- **CI cross-compiles for Windows and macOS.** The blocking job now runs
  `GOOS=windows` / `GOOS=darwin` compile smokes, so the windows-tagged
  runtime files can no longer rot uncompiled on the ubuntu-only matrix.
- **The layering rules are now a test.** `framework/layering_test.go`
  mechanically enforces the ARCHITECTURE.md invariants: no framework
  subpackage depends on the root facade (with the one sanctioned
  `pluginhost` → `battery/auth` carve-out pinned), `uihost` never links
  `framework/ui`, and `core-ui` never imports `framework`.
- Direct tests and a 98.0 coverage floor for `framework/datexport`, the
  registry behind `ExportData` / `ImportData` / `EraseUserData`,
  previously covered only from the consumer side.
- **`battery/log` grew a JSON sink.** `log.JSONSink` writes one JSON
  object per line to stdout (or any writer), the container pattern where
  the platform ships the stream. It reuses the fanout's slog encoding
  byte-for-byte and emits each entry in a single `Write`, so a collector
  never sees a torn line.
- **Backups have a doc page.** `backups.md` covers SQLite
  `.backup`/`VACUUM INTO`, `pg_dump` and point-in-time recovery, a
  restore drill, and why `ExportData` is not a backup. The deploy
  checklist gained the missing backup line.
- **CI now runs `make build`'s own checks.** csp-check, embed-check, and
  repo-lint run in the blocking job; they were previously local-only.
- **`historical upgrade fixtures` is a blocking check.** The job joined
  `scripts/release-required-checks.txt`, so a `gofastr upgrade`
  regression now blocks the release instead of shipping.
- **126 new coverage floors.** Every package the blocking CI job runs
  that measures ≥70% coverage now has a floor 1.5 points under its
  measured value; previously 14 packages were floored.

### Fixed

- **Docs told the truth about `.env` loading.** `deploy.md` claimed
  dotfiles load "in development"; `NewApp` loads them in every
  environment unless `GOFASTR_DOTENV=off`, and `.env.<APP_ENV>` joins the
  load order whenever `APP_ENV` is set. The corrected wording is pinned
  by the docs truth sweep.
- `query-dsl.md` pointed composite cursors at `EntityConfig.CursorFields`;
  the real path is `EntityConfig.Pagination.CursorFields`. Also pinned.
- **The docs site stopped calling Meridian regenerable.** The homepage and
  `/examples` cards said Meridian "is generated from one gofastr.yml"
  (the `/examples` card added "zero hand-written app code" and a
  `gofastr generate` run command its own doc.go forbids). Both now say
  what `examples/meridian/doc.go` says: seeded from one blueprint,
  hand-evolved since.
- `framework/ARCHITECTURE.md` caught up with the tree: the real
  subpackage count, map entries for the eight packages that had none
  (contracts, datexport, embed, fanout, gallery, outbox, ratelimit,
  semcov), the current intra-layer import edges, and why-it-stays rows
  for the root files added since the last refresh.
- README's documentation list now links the auth reference, access
  control, the runtime contract, and the browsable docs index. The
  most-asked topics were previously unreachable from the front door.
- **`OwnerField` without a declared field no longer loses the owner.**
  `entity.Define` now injects the owner column (hidden, read-only) when
  the declaration omits it, symmetric with the `tenant_id` injection and
  matching what the blueprint generator always did. Before, AutoMigrate
  never created the column: an authenticated create returned 201 while
  silently persisting a row no owner scope could ever reclaim, and the
  first scoped read failed with "no such column". The README's
  owner-scoping example now works as printed.
- **`MCP: true` without `WithMCP()` warns at boot.** Dev auto-mounts
  `/mcp`, so the miss only surfaced as a production 404. `Start` now
  logs the unreachable entity tools and names the fix, the same way the
  `WithMCPApp` warning does.
- **`WithPublicOpenAPI()` exposes `/api/llm.md`, as the banner already
  claimed.** The route stayed session-gated while the startup banner and
  capability map said otherwise. It now serves the full entity index
  under the same opt-in as the public spec; without the option it still
  401s, and per-entity `llm.md` routes keep their own scope gate.
- **The harness MCP server stopped advertising tools it cannot
  dispatch.** `harness.create_session` / `attach_session` /
  `detach_session` appeared in `tools/list` but every call returned
  "unknown tool"; they are de-listed, and a parity test now sweeps every
  advertised tool through dispatch.
- **Bool filters accept `true`/`false` on every read path.**
  `?published=true` silently matched nothing on SQLite while
  `?published=1` worked. Bool-column values now bind as real booleans on
  both dialects across the whole filter surface: flat filters, `?where=`
  predicate trees, nested relation filters (`?author.active=true`),
  scoped includes (`?include=posts(published=true)`), and the admin/
  resource bool facet. The last one is the Yes/No filter every
  blueprint-generated app ships.
- **A fresh scaffold explains its 401s.** The generated entity carries a
  comment naming the two ways out of secure-by-default (`Public: true`
  or `battery/auth`), and `gofastr init`'s next steps plus the generated
  AGENTS.md say the same. The unblock was previously README-only
  knowledge.
- **`gofastr migrate status` explains inline migrations.** On apps the
  CLI itself scaffolds (which register migrations in Go and apply them at
  boot), the missing-`migrations/` error now describes both systems and
  what to do, instead of a bare "directory not found".
- **One HTML escaper.** Seven hand-rolled escapers, two shipping the
  no-apostrophe shape documented as a past attribute-breakout XSS, now
  delegate to `core/render.Escape`, with per-package tests pinning that
  apostrophes are escaped. No live vulnerability existed; the class is
  gone.
- **README and the stability policy agree about `core-ui`.** README said
  core-ui "may break between commits" while `stability.md` put it under
  the one-minor deprecation window; README now states the policy applies,
  with the window simply exercised most often there.
- `harness-architecture.md`'s MCP tool table matches the shipped server
  and explains why session-lifecycle verbs have no tools.

### Changed

- **One backoff implementation.** New `core/backoff` (`Exponential`,
  `At`) replaces four copies across `framework/outbox`, `battery/queue`,
  `battery/webhook`, and `battery/log`. The log sink's uncapped inline
  doubling converged to the capped canonical curve.
- **One casing table in the CLI.** `cmd/gofastr`'s five converters merged
  into one file with correct acronym handling: `HTTPServer` snake-cases
  as `http_server` (generate previously produced `httpserver`, pack
  `h_t_t_p_server`). No golden output changed.

## [0.64.0] - 2026-08-16

### Changed

- **BREAKING: `/openapi.json` path keys now carry the API prefix.** Under
  `WithAPIPrefix("/api")` the document keys its operations `/api/posts` with
  `servers: [{url: "/"}]`, where it previously keyed them `/posts` with
  `servers: [{url: "/api"}]`. Both compose to the same URL, so Swagger UI and
  every servers-aware SDK generator are unaffected; a consumer that
  concatenated `servers[0].url` onto each path key must drop the
  concatenation.

  The old shape was legal OpenAPI and deliberately chosen. It was also the one
  shape that misleads a reader who takes `paths` at face value, and that reader
  is this framework's stated audience: the 2026-07-26 backend eval reproduced
  the confusion twice, in its agent *and* in its deterministic grader, which
  both concluded "the document does not describe the live `/api/tickets` path"
  , and ranked fixing it the highest-value change available. Custom
  `Endpoints` are documented at their mounted path too, via the same
  `EntityEndpointRoutePath` the router uses, so the absolute-path escape hatch
  behaves identically in both. Pinned by
  `TestAPIPrefix_OpenAPIPathsMatchLiveRoutes` and
  `TestAPIPrefix_EveryDocumentedPathIsRoutable`, which requests every
  documented path/method pair and fails on a 404.

- **BREAKING: `IdempotencyStore.Finish` takes the request fingerprint.** The
  signature is now
  `Finish(ctx, key, fingerprint string, resp *IdempotentResponse) error`, and
  the write is bound to the claim that produced it. Custom stores must accept
  the fingerprint and refuse to write a row that no longer carries it. The
  unbound signature was rejected rather than kept behind an optional interface
  because a store that ignores the fingerprint can serve one principal's
  response to another, and that must not be the quiet default.
- **A brokered call can never lift owner scoping.** A process module's reverse
  entity call is now marked at the boundary and `CrossOwnerRead` is refused for
  it unconditionally, instead of being predicted from the policy captured when
  the delegation was minted. The re-dispatch runs the full middleware chain and
  re-resolves authority, so the old check could pass judgement on a context the
  query never used. This matches what the broker already documented: a
  delegated user who legitimately holds `CrossOwnerRead` in their own session
  cannot exercise it through a module. It is still a behavior change for a host
  that relied on the carve-out silently not applying.

### Added

- **`gofastr docs backend-capability-map`: one page from job to primitive to
  proof.** The 2026-07-26 backend eval measured GoFastr at 313,579 cold-start
  tokens against Gin's 72,172 (4.35×) for 48% fewer lines of application code:
  the compression is real, and finding the primitive is what costs. Its sixth
  next-move was "one short capability map and exact verification commands
  before deep topical docs". `ui-capability-map.md` already did that for the
  UI lane; the backend lane, where the tokens actually went, had nothing.

  One row per job (scope rows to a user, add auth, run background work, prove
  the API works), naming the symbols to compose, a command that verifies it,
  and the one topic to open next. Generated `AGENTS.md` now points at both
  maps above everything else, so an agent lands on a row before it opens a
  reference.

  Three tests keep it honest, because a lookup table that lies costs more than
  no lookup table: every `pkg.Symbol` it names must be exported by a real
  package (it caught two wrong references while being written), every doc link
  must resolve to an embedded topic, and every `gofastr <sub>` must be in the
  CLI's dispatch table. The verification commands were run against a live
  scaffold rather than assumed. That is how the page came to state that
  `/openapi.json` and `/api/llm.md` answer 401 by default, that `/metrics`
  404s until `WithMetrics()`, and that a granular option placed before
  `WithConfig` is silently zeroed by it.

### Security

An audit of the packages that had no `*_security_test.go` coverage. The 194
existing files pin what earlier passes cleared; this one worked the
never-looked list instead. Every fix below has a test that was confirmed
failing against the unfixed code first.

- **SSRF: an IPv4-translation address reached internal space.**
  `netguard.IsInternal` normalized only the IPv4-mapped form
  (`::ffff:a.b.c.d`, via `net.IP.To4`), so every other encoding that resolves
  to an IPv4 destination read as public. `64:ff9b::a9fe:a9fe` is cloud
  instance metadata behind NAT64, and it passed both guards that use this
  predicate: webhook subscriber registration and the harness's webfetch, at
  registration *and* at dial time. A static AAAA record is enough, with no
  rebinding, and on an IPv6-only subnet with NAT64 (the documented AWS and
  GCP pattern) the translator forwards to `169.254.169.254`. NAT64
  (`64:ff9b::/96` and `64:ff9b:1::/48`), 6to4, and IPv4-compatible addresses
  are now classified by the IPv4 they carry. Public embeddings stay external,
  since `64:ff9b::/96` is the sanctioned egress path for IPv6-only workloads.

- **A panicking fanout subscriber killed every replica.**
  `SubscriberQueue` ran the callback bare on a framework-owned goroutine.
  No caller frame and no middleware can recover a panic there, so one
  poison payload took down every process subscribed to the topic. Subscribers
  are an extension point, and the framework already recovers around those in
  `hook.runHookSafely`; the same rule now applies here. Proven by a test that
  forks a child and observes real process death.

- **Rate-limit keys were attacker-chosen and unbounded.** `maxKeys` capped how
  many keys the in-memory limiter tracked, never how large one could be. The
  per-account login limiter keys on the submitted email, deliberately, so that
  account existence cannot be probed. That left the 1 MiB body cap as the only
  bound, and one unauthenticated POST could park ~1 MiB for a full block
  duration. Keys past 256 bytes now fold to a digest, and an address longer
  than RFC 5321 allows is rejected before it becomes a key.

- **Key-flood eviction lifted the attacker's own lockout.** The limiter's
  low-water shed sorted by ascending `blockedUntil`, which is creation order
  once every block shares one duration, so the oldest block, the one predating
  the flood, went first. Burn the login budget, spray keys, walk free. Eviction
  now sheds unblocked entries first, then the newest blocks.

- **`/\evil.example` passed the URL guard.** Browsers normalize `\` to `/` at
  the authority boundary, so `\\host`, `\/host`, and `/\host` all resolve to
  `//host`. Only the `//` spelling was checked. `/\host` matters most: it
  begins with `/`, so it matched the relative-reference check one line below
  the `//` guard and reads as same-origin. Eight sinks delegate to this
  predicate.

- **A `.env` typo put a secret in the logs.** `dotenv`'s missing-`=` error
  formatted the whole raw line, so `API_TOKEN sk-live-…` reached stderr and
  everything behind it. Hosts wrap `LoadAndApply` in `log.Fatal`. The line
  number stays; the line does not.

- **Kiln: four holes on the agent-facing surface.** `escAttr` did not escape
  `'` while the panel emits single-quoted attributes, so a model-chosen
  `plan_id` planted a live event handler on the operator's Approve button,
  the control gating destructive world edits. `/kiln/status` returned
  `Auth.JWTSecret` and `Admin.SeedPassword` unredacted under `?fields=world`
  and `?fields=app`, while `serveWorld` twenty lines away redacted both.
  `set_theme` values reached `/kiln/theme.css` unvalidated, giving an agent
  arbitrary CSS on every page. And `X-Forwarded-Host` chose a string
  substituted into the fallback page, on a server that binds loopback and is
  never proxied.

- **The admin rendered stored media URLs without a scheme check.** Image `src`
  and File `href` came straight from the column value, the last two dynamic
  URL sinks in the repo not routed through `urlsafe`. Writes are validated, so
  this is defense in depth, but seeds, migrations, and hook-written rows never
  meet that check. Worth noting that `download` does not defuse a `javascript:`
  href; browsers ignore it for non-http(s) schemes.

- **Setup ran a wizard step while the completion probe was erroring.** An error
  means unknown, not "not done", and that guard is the only thing between a
  second caller and a step that usually creates the admin account. It now
  refuses.

- **`gofastr init` scaffolded `.env` world-readable.** It carries
  `DATABASE_URL` and is the documented home of `GOFASTR_SECRET`. Now `0600`.

- **`softdelete.SoftDelete` gained the unscoped-operation warning** its
  siblings `Restore` and `HardDelete` already carried. Same shape:
  `UPDATE … WHERE id = $1` with no tenant, owner, or access filter.

### Fixed

- **An option placed before `WithConfig` is no longer discarded in silence.**
  `WithConfig` replaces the whole `AppConfig`. That is deliberate, since a merge
  could not tell an explicit zero from an unset field, but the discard was
  invisible: `gofastr init` scaffolded `WithConfig` last, so pasting
  `framework.WithPublicOpenAPI()` at the natural spot next to `WithDB` did
  nothing, with no error. The scaffold now emits `WithConfig` first (any
  option pasted below it takes effect), and `NewApp` logs a warning naming
  each field an earlier option set that `WithConfig` zeroed and no later
  option restored. Replace semantics are unchanged.

- **Go floor raised to 1.26.6 for six standard-library advisories.** `go.mod`
  declared `go 1.26.5`, and govulncheck found six advisories reachable from
  this tree that are all fixed in the *toolchain*, not in any dependency:
  GO-2026-6089 (`net/http` skips `ReadHeaderTimeout` on the unencrypted HTTP/2
  check, reached from `framework.App.Start`), GO-2026-5972 (`encoding/asn1`
  unbounded recursion, reached from `uihost.UIHost.ServeHTTP`), GO-2026-5026
  (`x/net/idna` accepts ASCII-only Punycode labels, reached from every
  outbound `http.Client` call including `battery/auth`'s OAuth2 provider), and
  three more. CI resolves its toolchain from `go-version-file: go.mod`, so the
  directive is the pin. `CONTRIBUTING.md` and `deploy.md` were updated in the
  same commit. The last time they drifted from `go.mod` it took a release to
  notice.

  Three temp modules that `replace` gofastr with the checkout hardcoded the old
  directive, which Go rejects (a module may not declare a `go` version below a
  dependency's). Two now read the repo's own go.mod, and the upgrade-fixture
  harness rewrites the directive the same way it already rewrote the `replace`,
  so a committed fixture stays an honest snapshot of the app its version
  generated and a future bump touches neither.

- **`battery/print/chromepdf` went stale on a root dependency bump.** The
  nested module reaches its dependencies through a local `replace` to the repo
  root, so raising `golang.org/x/image` in the root left its own go.mod/go.sum
  behind and `go build` refused with a bare "updates to go.mod needed". Its
  go.sum was also still carrying the entire testcontainers/Docker closure this
  release removed from the root graph, 84 lines of it. Re-tidied.

- **A process module's migrations failed on every deploy after the first.**
  `provisionModuleSchemaRole` creates the module's restricted Postgres role
  inside `DO $$ … CREATE ROLE … PASSWORD '<new>' … EXCEPTION WHEN
  duplicate_object THEN null; END $$`. That is idempotent for the role's
  *existence* and a silent no-op for its *password*: roles are cluster-scoped,
  so on the second deploy the role already exists, the `CREATE` raises
  `duplicate_object`, the handler swallows it, and the role keeps its original
  secret. The following `ALTER ROLE` re-asserted every privilege flag but not
  the password. The coordinator's very next step is to authenticate as that
  role with the freshly generated one, so it got `password authentication
  failed for user "module_<name>_role" (28P01)` and the migration never ran.

  The `ALTER ROLE` now re-asserts `LOGIN PASSWORD` alongside the flags. The
  DDL is split into `moduleSchemaRoleStmts` so the invariant is pinned by an
  ordinary unit test rather than one that needs a live server. The defect
  survived precisely because it was only reachable with a *pre-existing* role,
  and CI handed every test process a throwaway Postgres container where no
  role ever pre-existed. Sharing one server is what exposed it.

- **The in-house SQLite engine is documented.** `sqlite/` holds a
  from-scratch SQLite that nothing outside this repo should ever import:
  pager, B-tree, parser, file format, ~7k lines plus as many again in tests. It
  had no doc topic, no ROADMAP entry, and no README mention, so the only way
  to learn what it was for was to read it. `gofastr docs sqlite-engine` now
  states the split plainly: applications get modernc.org/sqlite as `sqlite3`
  via `sqlite/stdlib`; the in-house engine is registered as `gofastr-sqlite`
  and exists so ~20 suites can validate the framework's generated SQL against
  an implementation that shares no code with the one it was developed on.
  `sqlite/BENCHMARKS.md` is marked as the 2025-05-15 snapshot it is, and
  `sqlite/driver_name_test.go` fails if the engine ever claims the `sqlite3`
  name. The day it does, the whole suite starts validating the wrong engine
  while every app still ships modernc.

- **Every app built on GoFastr inherited the Docker client stack.**
  `testcontainers-go` was a direct require in the root `go.mod`, used only to
  spawn `postgres:16-alpine` for this repo's own tests, and Go hands a
  module's requirements to everything that imports it. A hello-world scaffold
  therefore resolved 95 modules and 178 `go.sum` lines, pulling `go-winio`,
  `go-ansiterm`, `plan9stats`, `perfstat`, `purego`, `wmi`, `go-ole` and
  `pprof` to run `go mod tidy`. It is now **57 modules and 100 `go.sum`
  lines**, with zero Docker-stack entries.

  Every Postgres suite already preferred `TEST_POSTGRES_DSN`; the container
  branch behind it was removed rather than hidden, because `go mod tidy` walks
  every build configuration and a build tag would have kept the require. CI
  supplies the DSN from a `pgvector/pgvector:pg16` service, and
  `docker-compose.yml` gained a matching `postgres` service: `make
  postgres-up` starts it, `make test-pg` starts it and runs against it. One
  image covers the plain-SQL suites and `battery/semantic`'s pgvector store
  alike. The fail-closed `PGTEST_REQUIRED` canary now also guards this wiring:
  a service that fails to come up fails the job instead of skipping quietly.
  `cmd/repolint` gained a `test-only-dep-in-consumer-graph` rule so a test-only
  dependency cannot return to the root `go.mod` unnoticed.

  One consequence for anyone running the Postgres suites locally: all test
  processes now share a single server instead of getting an ephemeral
  container each, so per-test schema names have to be unique across processes.
  `testdb.NewSchemaName` now embeds the pid (`internal/pgtest` always did), and
  the one test that created a fixed database-scoped schema drops it on
  cleanup. Without those, `go test -p 2` raced itself with `schema … already
  exists`.

- **`gofastr generate --from=` outside a Go module recommended a command that
  could not run.** It reported plain success and led its next steps with
  `go mod tidy`, which fails with a raw `go.mod file not found in current
  directory or any parent` before anything else happens. The generated code
  imports itself by module path, so nothing it wrote could build yet. The
  adjacent case (a `go.mod` declaring a *different* module) already failed with
  the exact remedy; the absent case now warns and prints the runnable
  `go mod init <app.module>` ahead of `go mod tidy`. Still a warning, not a
  refusal: generating into a directory about to become a module is legitimate.

- **Five of seven shipped blueprints generated an app that did not compile.**
  `blog`, `lms`, `portfolio`, `project-manager`, and `real-estate` each emitted
  `app.go` using `context.Context` without importing `context`, and the four
  with a home screen also emitted `screen_home.go` calling
  `resource.PublicIsland()` without importing `framework/ui/resource`. Two
  independent import-set bugs, both the same shape: a condition that re-derives
  what the emitter already decided. `rbac` was computed *below* the import
  block, so the `"context"` condition could not see it; and the screen walker
  set `needs.resource` only for blocks declaring filters or transitions, while
  `blueprintEntityListConfigExpr` attaches an island policy to *every*
  entity_list, `resource.PublicIsland()` on an ungated screen. The walker now
  asks `blueprintIslandPolicyExpr`, the emitter's own helper, instead of
  guessing alongside it.

  These survived because only two blueprints ever had their emitted Go
  compiled: `examples/meridian` (a per-example build gate) and
  `examples/ecommerce` (byte-parity against a committed `app/` that CI builds).
  ecommerce declares no home screen and no `access:` role policy, so it is the
  one example that reaches neither broken path. Every other blueprint was
  covered solely by a test asserting its YAML parses.
  `TestExampleBlueprintsGenerateAndCompile` now generates *and* builds *and*
  vets every `examples/*/gofastr.yml`, so a generator path is gated the moment
  any blueprint uses it.

- **An idempotent request could be answered with another caller's response.**
  When a handler outran the in-flight TTL and a second request re-claimed the
  same `Idempotency-Key`, the first handler's `Finish` wrote its response into
  the second's row while leaving that row's fingerprint in place, so the
  retry passed the fingerprint check and was served a body it never asked for.
  With `Principal` set to a tenant id, as the configuration documents, that
  crosses users. Proven by execution before it was fixed. The expired-claim
  re-claim also deleted by key alone, so two racing re-claims could destroy a
  fresh claim and run the handler twice; it now deletes only a still-expired
  row.
- **A process module could read entities outside its approved grant.** Every
  key of a module's filter object was forwarded into the CRUD list query, so
  `include=` eager-loaded a second entity the grant never named, with no row
  scoping at all when that entity declares no owner and no tenant, and
  `trashed=true` surfaced soft-deleted rows. Filter keys are now allow-listed
  against the resolved entity's declared, non-hidden, queryable fields, which
  is what the code claimed to do. Delegation handles are also bound to the
  module they were issued to, and handle minting fails loudly on an entropy
  error instead of silently producing a constant.
- **`/api/llm.md` listed every entity to any signed-in caller.** The index was
  rendered once at startup and gated on a session, while the per-entity
  document it links to runs the full scope chain. The schema is the
  disclosure, not the rows. It is now rendered per request and filtered to the
  entities that caller can list.
- **`SearchInput` and `FilterToolbar` put their `Action` into a form without
  the URL guard** every sibling form applies, so a `javascript:` action
  survived to submit. `FilterToolbar` only appeared safe because an unrelated
  button panicked first, which `HideReset` skipped.
- **Panic and timeout logs carried raw request paths.** `URL.Path` is
  percent-decoded, so a `%0d%0a` in the request forged log lines through the
  recovery and timeout sinks, which never scrubbed; the access-log sink
  scrubbed but never bounded, so an oversized path was logged whole. Both
  halves now hold at all three.
- **An unknown filter parameter could pin a CPU.** The "did you mean" suggestion
  ran an edit-distance pass over an unbounded, unauthenticated query-parameter
  name; the suggestion is now skipped past a sane length and the plain error is
  returned.
- **One panicking metrics collector no longer drops the whole scrape**: each
  runs isolated, matching how the hook runner already treats third-party
  callbacks.
- **Magic-link tokens are erased with the user.** `EraseUserData` gained a
  declarative identity seam: a battery can key an eraser on a resolved
  identity, such as the user's email, instead of the user id. Magic-link
  tokens are keyed by email and so survived an erasure: a link minted before
  it and redeemed after created a fresh account for the erased address. Shipped
  as a documented limitation in v0.63.0; it is closed now. 2FA secrets and
  OAuth links are erased too.
- **`SegmentedControl` posts the selected value.** `RPCPath` attached the RPC
  attribute and fired the request with an empty body, so the handler fell
  through to its default and confidently rendered the wrong selection. Any form
  control dispatching an RPC now serializes its enclosing form; an explicit
  body still wins, and a control outside a form is unchanged. The same gap in
  the tool-call dispatcher is fixed with it.

## [0.63.0] - 2026-08-05

### Added

- **HTTP server timeouts are configurable.** `AppConfig.HTTPServerTimeouts`
  (or `framework.WithHTTPServerTimeouts`) sets `ReadHeaderTimeout`,
  `ReadTimeout`, `WriteTimeout`, and `IdleTimeout` on the embedded
  `http.Server`. Fields are `*time.Duration`: nil keeps the defaults
  (10s/60s/60s/120s), an explicit 0 disables that deadline, matching
  `net/http`. A request that needs longer than the old hardcoded 60s cap now
  has a knob. SSE is unaffected. Streams already survive the write deadline
  via `EventSource` reconnect. The resolved values appear in the `app_config`
  MCP tool. See `gofastr docs deploy`.
- **A poll can stop.** A server response carrying `X-Gofastr-Poll-Stop: 1`
  ends that region's `data-fui-poll` cadence after the body is applied.
  Widgets declare it with `Builder.PollTerminal(func() bool)`. A job-status
  widget that reaches `completed` stops hitting the server. No new DOM
  attributes; a swapped-in fragment without the poll marker (or with
  `"off"`/`"0"`) also never re-wires. See `gofastr docs reactivity`.
- **Subsystem metrics at `/metrics`.** The metrics endpoint now reports DB
  pool stats (`sql.DBStats`), queue depth and dead-letter counts per lane,
  outbox pending/dead-letter per consumer, webhook delivery/failure counts,
  and a slow-query counter. Each family appears when its subsystem is
  wired. Batteries register through `App.Metrics().RegisterCollector`. The
  panic-recovery middleware routes through a new `log.ErrorReporter` seam
  (slog default; an HTTP JSON sink ships for Slack-style collectors), and
  `gofastr docs observability` has the copy-paste OTLP exporter wiring.
- **Secrets rotate without a mass logout.** `GOFASTR_SECRET` verification
  accepts previous keys from `GOFASTR_SECRET_PREVIOUS` (or
  `framework.WithSecretRotation(current, previous...)`); the auth battery
  accepts `AuthConfig.JWTPreviousSecrets`. Embedded surfaces rotate too:
  outstanding handshake nonces and frame grants verify against the previous
  keys. New tokens always sign with the current secret; an explicit
  `WithSecret` or previous-less `WithSecretRotation` closes the window even
  when `GOFASTR_SECRET_PREVIOUS` is still in the environment; production
  mode still refuses a previous-only config. The drain procedure is in
  `gofastr docs deploy`.
- **`App.EraseUserData`: the erasure half of GDPR.** Hard-deletes a user's
  rows across every `OwnerField`-scoped entity and registered battery tables
  (auth users, sessions, 2FA secrets, and OAuth links), anonymizes the actor
  on audit rows instead of deleting them, and returns a per-table report.
  Entity tables are deleted children-first so foreign keys without
  `ON DELETE CASCADE` do not roll the erasure back; a blank user id is
  refused rather than matching every unowned row. Idempotent, with a dry-run
  mode. Batteries declare erasure the same way they declare export. Magic-link
  tokens are keyed by email rather than user id and are not reached. See the
  note in `gofastr docs data-export`.
- **`framework/ratelimit`: the auth limiter, extracted for any route.**
  The sliding-window limiter that guarded login/register/password-reset is
  now a package with `Middleware()` (per-IP) and `MiddlewareByKey(fn)`;
  `battery/auth` keeps its behavior through a thin wrapper. Limits are
  per-replica (process memory) unless a shared `Store` is wired. New doc
  topic: `gofastr docs rate-limit`.
- **`gofastr verify`: `entities/crud-without-auth`.** Warns when an entity
  exposes CRUD routes while the app wires no auth and the entity is not
  `Public`, the state where every advertised route answers 401. The
  catalog is now 50 rules.
- **Historical upgrades are proven in CI.** Two generated apps pinned to
  v0.38.0 and v0.53.0 (with committed pre-upgrade databases) are upgraded to
  the current tree, built, booted, and exercised over HTTP: migrations over
  existing rows, login, owner isolation, OpenAPI, and one island RPC. The
  shipped `gofastr upgrade` detector must flag the pre-upgrade source, and a
  negative test proves a skipped migration step fails the build.
  `evals/upgrade-fixtures/`, non-blocking CI job.

### Changed

- **Dialect detection fails closed.** When the migration engine cannot
  determine whether the database is PostgreSQL or SQLite after retries,
  `AutoMigrate` (and `MigrateEntity`, `DiffSchema`, `RunSeeds`, the
  `WithSeed` hook path, and the process-module store and migration
  coordinator) now return an error naming the probe failure instead of
  assuming SQLite. A guess there skipped the cross-replica advisory lock
  and every Postgres-only routine. `migrate.DetectDialectStrict` is the
  exported fail-closed probe for coordination decisions; `DetectDialect`
  keeps its signature for callers that only pick DDL types. The probe
  classifier ignores quoted identifiers, so a Postgres error naming a role
  like `"syntax error"` is no longer read as evidence of SQLite.
  Connection-class failures carry a "check the database connection /
  DATABASE_URL" hint.
- **The release gate is a tested script with an exact manifest.**
  `scripts/release-gate.sh` (stubbed and tested against nine failure
  fixtures) replaces the inline workflow poll. Every check in
  `scripts/release-required-checks.txt`, all ten blocking CI checks, must
  complete green on the tagged SHA, the tag must equal the current main
  head (an unmerged or stale commit cannot publish), and a pre-existing
  release for the tag fails the gate. A repo test pins the manifest to
  ci.yml's job names, so a CI rename breaks the build instead of the gate.
  The supported flow is merge → tag push; the workflow is the publisher.
  The tag name is shape-validated once and passed through the environment,
  never spliced into a step's shell source. Git accepts command-substitution
  syntax in tag names. Check runs fold newest-first across all pages, so a red
  re-run of an already-green check blocks the release; tag and main head are
  re-verified immediately before publishing.
- **Release tags no longer re-run CI.** A release tag points at a merge
  commit on main, so the release gate consults the blocking check runs
  that commit's main-push CI run already produced; the tag-triggered
  duplicate of the identical pipeline is gone. CI itself got faster: the Go
  build/test cache persists per commit, so packages whose sources and
  dependencies are unchanged skip re-execution, and each browser e2e suite
  runs on its own runner. The browser-e2e wall clock drops from the
  ~28-minute serial sum to the slowest suite (~13 minutes).
- **`battery/print/chromepdf` is a nested module.** Same import path, own
  `go.mod`/`go.sum`, excluded from the root module's `./...`. The root Go
  floor stays 1.26. `chromedp` is also a direct dependency of the CLI's
  accessibility audit and `framework/testkit/axetest`, and deploy.md
  wrongly attributed the floor to the print battery.
- **Coverage floors extend to the UI and auth plane.** `framework/ui`,
  `framework/uihost`, `framework/access`, and `battery/auth` are gated in
  `scripts/coverage-floors.sh`; previously only the data plane was.

### Fixed

- **`gofastr dev --addr` reaches the app again.** The dev runner passed
  `--addr` as argv, but generated scaffolds read only the `PORT` env var,
  so the child bound the committed `.env` default while the banner printed
  the requested address. The runner now injects `PORT=<addr>` into the
  child environment (a stale parent-shell `PORT` is dropped).
- **The startup banner says which routes need auth.** A fresh scaffold
  advertised `/posts`, `/openapi.json`, `/api/docs/`, and `/api/llm.md`,
  all of which answer 401 to an anonymous request. Non-`Public` entity
  routes and the non-public API surface are now annotated
  `(requires auth …)`.
- **Generated seeding no longer re-runs on a failed count.** The blueprint
  seed hook treated a `CountAll` error as "not seeded" and inserted again.
  A transient read failure could duplicate seed rows. A count error now
  aborts startup naming the entity. Regenerate to pick it up.
- **`app-cli.md` build path matches the generated directory** (`cmd/<binary>/`),
  and deploy.md's Go-version note names the real source of the 1.26 floor.

## [0.62.0] - 2026-08-05

### Changed

- **Generated apps fail closed at boot.** `gofastr init`'s scaffold used to
  log `Migration warning` on a failed `migrator.Up` and start the server
  anyway; it now exits, so a deploy whose committed migration did not apply
  fails the rollout instead of surfacing as later request errors. Blueprint
  seeding is fail-fast for the same reason: a seed row that fails (or an
  entity with no handler) aborts startup naming the entity, where it used to
  be logged and skipped, and never retried, since the idempotency check
  marks a partially-seeded entity as done. Regenerate to pick both up.

### Fixed

- **A release tag now requires green CI on the exact tagged commit.** CI runs
  on `v*` tag pushes, and the release workflow refuses to publish until every
  blocking check on the tag SHA has completed successfully. It previously
  checked only that a changelog section existed, which is how v0.61.0
  published while its build·vet·test check was red.
- **The Postgres CI canary cannot skip in required mode.** The URL-shape
  guards in `pgtest.DB` / `FreshDatabaseDSN` / `UnusedDSN` ran after the
  fail-closed choke point and still skipped, so a non-URL `TEST_POSTGRES_DSN`,
  the documented override, silently turned the canary green.
- **The backend-adoption eval runs again.** The Gin and stdlib baseline lanes
  required `gofastr/sqlite/stdlib@v1.14.44`, mattn's version carried onto a
  path that is not a module during the driver swap, so lane setup failed
  before any trial. They now pin `modernc.org/sqlite`, and a test fails when
  any baseline requirement stops resolving.
- **Required markers sit on the label line.** `ui.Select`, `ui.RadioGroup`,
  and `ui.CheckboxGroup` joined the `*` as a sibling of their
  `<label>`/`<legend>` inside a grid container, so it rendered as its own row
  under the label text, unlike `ui.FormField`, which keeps it inline. The
  marker now renders inside the label/legend, and the select and toggle
  stylesheets carry its red color and spacing themselves instead of relying
  on `ui-form-field`'s scoped rule happening to be on the page. (#188)
- **The sidebar shell's header has banner chrome.** A `WithSidebar` +
  `WithHeader` layout rendered its banner with no height, background, or
  bottom border: `ui.SiteHeader` is `block-size: 100%`, which resolves
  against a parent `LayoutBaseCSS` never sized. The shell header now takes
  its height from the `--ui-layout-header-height` token (default 56px), and
  the `.layout-body` underneath subtracts that height so pages stop
  scrolling by exactly the header's size. (#187)

- **Flat blueprint booleans that gate access are strict.** The entity-level
  `soft_delete:` and `multi_tenant:` keys and `app.auth.enabled` now demand a
  real `true`/`false`, exactly like their grouped `scope.*` twins. A YAML-1.1
  truthy such as `multi_tenant: yes` decoded to *false*, silently dropping
  tenant scoping, and `enabled: yes` left the whole app public with no
  login. `gofastr validate` and `generate` both reject the spelling with an
  error naming the key.
- **Detail screens must put `{id}` in their route.** The blueprint validator
  rejects an `entity_detail` (or edit-mode `entity_form`) screen whose route
  declares no parameter: such screens rendered an empty record and every
  list-view "View" link pointed at a route that matched nothing. Six bundled
  example blueprints had exactly this bug; their detail routes now live under
  the entity's list path (`/products/{id}`, `/posts/{id}`, …) and the
  regenerated ecommerce flagship serves them with an e2e guard that follows a
  real View link.
- **Directory-mode blueprints keep their seeds.** `gofastr generate
  --from=<dir>` merged every section except `seed:`. Multi-file blueprints
  silently lost all seed data.
- **Generated apps no longer mount a dead delete-confirmation widget.**
  Soft-delete entities emitted a `ui.ConfirmAction` nothing referenced, aimed
  at a nonexistent RPC path; the resource engine's own delete flow is the one
  that works, and now it's the only one emitted.
- **`gofastr init --db=` validates its value.** An unknown driver (say
  `--db=mysql`) used to silently scaffold a SQLite project that only looked
  configured; it now errors listing the accepted values: `sqlite`,
  `postgres`, aliases `sqlite3`/`postgresql`.
- **Example seeds no longer skip rows silently.** The ecommerce reviews
  (missing required product reference), lms courses (missing instructor),
  and project-manager tasks/comments (whole board→column chains absent)
  seeded nothing on boot; all are wired with `@entity.field=value`
  references, and the flagship's e2e now asserts seeded reviews and that
  every category has products.
- **SSE streams no longer die at the request timeout.** The Timeout
  middleware returned to net/http at the deadline even when the response
  was already streaming, finalizing the response under the live handler,
  so every SSE connection older than the 30s default died with a recovered
  heartbeat panic and a silent client reconnect, violating the documented
  "streams outlive the request timeout" contract. Once a handler flushes
  or hijacks, the deadline now stops terminating the response (the request
  context still expires, so handlers that honor it still unwind); hung
  non-streaming handlers keep their 504 at the deadline.
- **Navigated-away pages no longer hoard SSE connections.** The SSE client
  module never closed its EventSource on navigation, and Chrome keeps
  bfcached pages' sockets open, so hard-navigating six SSE-bearing pages
  exhausted the tab's per-host connection budget and the seventh page never
  loaded. The transport now closes on `pagehide` and reconnects on a
  bfcache restore (`pageshow.persisted`). This had been masked by the
  request-timeout bug above, which was killing every stream at 30s.
- **The harness ws control channel could lose an entire turn's events.** The
  event pump subscribed to the bus in its own goroutine after the read loop
  was already accepting commands, so a turn dispatched before the
  subscription registered broadcast to nobody, the intermittent
  `TestWSHandshakeAndCommand` CI failure. Subscription now happens before the
  read loop starts.
- **Meridian's auth footers match the generator again** (#185): the
  hand-owned login/signup screens now render the URL-checked `ui.Link`
  footer the generator emits, verified byte-identical and screenshotted in
  both themes.
- **`gofastr pack` reads the v0.61 auth footer.** The reverse reader only
  understood the old raw-anchor footer, so packing a current app dropped
  `register_href`/`login_href` from auth screens and broke the round-trip.
- **The website taught an entity shape that no longer compiles.** The Get
  Started and framework-hub pages showed the pre-v0.54 flat `EntityConfig`
  fields (`CRUD:`, `Public:`, `OwnerField:` at top level) and a scaffold
  layout `gofastr init` never writes; the samples are fixed and every Go
  code block on both pages is now extracted and compile-gated in CI.
- **Docs corrected against code**: init/blueprint scaffold layouts in four
  docs, install pins unified on `@latest`, the CLI doc now covers `gofastr
  build`'s contracts gate and the real `--db` values, the README's
  cursor-paging field path (`Pagination.CursorField`), the overview's
  embed-vs-semantic-search mixup, and the embedded-docs count floor is exact
  so a deleted doc fails the build.

## [0.61.0] - 2026-08-04

### Added

- **Versioned migrations from a host binary.**
  `migrate.GenerateMigrationFile(plan, name, opts)` diffs a migration
  Plan against the committed snapshot and writes the next numbered
  migration, updating the snapshot. `App.MigrationPlan()` returns the
  Plan `Start` auto-migrates from, the entity registry plus every
  registered routine and view, so an app whose schema lives in owned
  Go (the blueprint deleted, as the architecture intends) can emit
  reviewable migrations instead of choosing between boot-time
  auto-migrate and hand-written SQL. `gofastr migrate generate` now
  calls the same entrypoint, so the two paths cannot drift in file
  naming, directive layout, or snapshot format. New doc section:
  `gofastr docs migrations` → "Generating from a host binary".
- **`App.SetSetup`** wires a first-run setup runner after construction.
  `setup.HealthStep` needs the `*App` to call `RunReadinessChecks`, so
  the runner cannot exist before `NewApp` returns and `WithSetup` could
  not receive it; the documented workaround was to re-apply the option
  as a function.
- **Support tiers for every package.** `stability` classifies each
  package Stable, Provisional, Experimental, Internal, or Excluded, and
  `TestEveryPackageIsClassified` fails when a package has none, so a new
  top-level tree cannot ship unclassified. Nothing is Stable before
  v1.0.0: the supported surface is Provisional, meaning documented and
  covered by the deprecation window, not frozen. `docs/public-api.md`
  says what each tier promises.
- **Column renames in blueprints.** An entity's `renames:` key (old
  column → new column) reaches `EntityConfig.Renames`, so the schema
  diff emits `RENAME COLUMN` instead of a data-losing drop plus add.
  Renames were Go-declaration-only, which left the blueprint, the only
  input the standalone migration generator reads, unable to express the
  difference. Both sides are checked as column identifiers, the new name
  must be a declared field, and the old name must not be.
- **Embedded doc examples are compiled.** `TestDocExamplesCompile`
  builds Go snippets marked with a `gofastr:compile` directive against
  the real framework, so an example cannot rot into something that does
  not build. The existing denylist could not catch a scope or signature
  error, which is how `first-run.md` came to use a variable one line
  before declaring it.

### Security

- **A blueprint relation name is now checked as an identifier.** It was
  the last name in the blueprint IR without that guard, while the entity
  emitter puts it in struct-field position and inside a backtick
  `json:"…"` tag, a raw literal with no escape mechanism, and the
  typed-column emitter puts it in const-identifier position. Every
  crafted value we tried produced Go that does not parse rather than Go
  that runs, so this is a corrupt-output bug we are treating as a code
  injection because the emitter is fed agent-transcribed text. A
  relation whose name collides with a field's is refused too; it emitted
  a duplicate struct field. As a backstop for the whole class, blueprint
  generation now parses every `.go` file it emits and refuses to write
  one that does not.
- **The generated SDK's module path and header are validated.** A
  newline in `--module` appended `replace` directives to the shipped
  SDK's `go.mod`, which runs arbitrary code in every repo that builds
  it. The header is sanitized at its one choke point, closing a comment
  escape into `client.js`, served as `application/javascript`, so
  same-origin script execution.
- **Codegen command extensions get an allowlisted environment.** An
  extension is a binary named by whichever `gofastr.codegen.yml` is in
  the project, and it inherited the developer's whole environment
  including `GOFASTR_SECRET` (enough to forge sessions for the deployed
  app), `DATABASE_URL`, and cloud credentials. It now receives only what
  a build tool needs to run; project values belong under the extension's
  `config:` key, which already arrives on stdin.
- **Extension output is scrubbed, bounded, and time-limited.** stderr is
  replayed to the operator's terminal, so escape sequences are stripped
  from it and from diagnostic messages (CWE-150; an OSC 52 sequence can
  write the system clipboard, planting a command the developer later
  pastes). stdout and stderr are capped at 16 MiB, and an extension
  holding a pipe open can no longer hang the generate indefinitely.
  Cancellation was previously unreachable because the command was
  started with a background context.
- An extension's `severity: error` diagnostic was collected and read by
  nobody, so an extension could not reject its input. It now fails the
  run.
- **Blueprint theme values are validated against the CSS grammar.**
  `app.theme` and `app.theme.dark` values were emitted as direct struct
  assignments, which bypass every setter, so a value carrying `;` or `}`
  closed the `:root` block and appended rules to the stylesheet the UI
  host and the admin battery serve as `text/css`. That is CSS injection,
  `url()` exfiltration and attribute-selector reads, and becomes XSS
  if any surface ever inlines that sheet in a `<style>` element.
  `theming.md` already named this exact anti-pattern under "Common
  mistakes". `style.ValidateColorValue` is now exported so the generator
  calls the one existing grammar instead of carrying a copy.
- **The generated auth footer link no longer accepts any scheme.** The
  `login_form` / `signup_form` footer was a hand-rolled `<a href>` inside
  `render.Raw`, which escaped the value into the attribute but never
  checked the scheme, so `register_href: "javascript:…"` put a live
  `javascript:` anchor on the login and signup screens. It emits
  `ui.Link` now and is refused at validate time, sharing `urlsafe.Anchor`
  with the renderer.
- `gofastr pack` no longer invents list items. `needsQuote` tested for a
  comma only as a scalar's first byte, so an interior comma was emitted
  bare into a flow list, where it separates items, and an enum value
  like `open,closed` round-tripped into two values.
- A typo in a screen's `layout:` silently rendered the screen inside the
  authenticated app shell instead of failing; anything that was not
  `marketing` mapped to the app layout.
- **BREAKING (validation): six blueprint booleans that gate access now
  reject anything but `true`/`false`.** `core/yaml` implements YAML 1.2,
  where `yes`, `on`, `y`, and `1` are strings rather than booleans, so
  `access: {auth: yes}` read as **false** and registered the screen with
  no policy at all, publicly reachable, with no error to say so.
  `scope.multi_tenant: yes` dropped tenant scoping identically, and a
  field's `hidden`, `no_query`, or `read_only` failed open the same way.
  The six now error; keys where false is the inert direction (`public`,
  `mcp`, `crud`, `enabled`) keep the lax reading. A blueprint using one of
  the rejected spellings was already not getting the protection it asked
  for, so fix the value rather than reverting: no in-tree blueprint,
  example, or doc used one.
- The generated JavaScript client emitted entity names into unquoted
  object-key and statement positions (`this.<name> = new Resource(…)`),
  and those names are not identifier-constrained on the way in. It now
  emits `this[<name>]` with quoted keys. Consumer-visible behavior is
  unchanged. `client.d.ts` keeps unquoted names deliberately: type
  declarations are never executed.

### Fixed

- A declared `Index.Expression` was dropped by the entity emitter, so an
  expression index came out with no key at all. A `Unique` index on an
  expression silently did not constrain anything.
- `migrate.GenerateMigrationFile` validates a group name before writing
  anything, and fails loudly rather than silently dropping the group if
  the `-- +migrate Up` anchor it stamps against is absent. The CLI
  validated `--group`; the library path did not.
- The first-run guide's setup example did not compile. It passed `app`
  to `setup.HealthStep` a line before declaring it.
- The cron guide said the scheduler had no distributed coordination and
  every replica fired every job, while `Scheduler.WithLeaderElection`
  and `NewPostgresAdvisoryLease` shipped for exactly that.
- The README's "No reflection" claim was false as written; reflection
  runs in CRUD paths. It now states the accurate claim: no reflection
  discovers your entities.
- `SECURITY.md` named `0.59.x` as the supported release after v0.60.0
  shipped.
- `core-ui/ARCHITECTURE.md` said an island owns server-side state "in
  memory or DB", contradicting the stateless interactive layer it
  describes twenty lines earlier. State lives in the DB; any replica
  serves any request.
- `core-ui/ARCHITECTURE.md`'s package map still listed `theme.css` and
  `styles.css` as served routes; `app.css` replaced both and the old
  paths return 410.
- `perf-results.md` presented v0.26.0 measurements as current; it now
  says which release and date they describe.

## [0.60.0] - 2026-08-04

### Added

- **Layout chains.** A screen's layouts now resolve to an explicit
  chain (outermost → innermost) derived from the default layout,
  `ScreenGroup` nesting, and per-screen overrides. Every layer renders
  with `data-fui-layout-key` (its identity) and `data-fui-layout-slot`
  (its content cell), the route manifest carries the chain as the
  `layouts` array, and navigation swaps at the deepest layer the
  current page shares with the destination. Sibling pages keep their
  section's sidebar DOM (scroll, open disclosures included); pages
  sharing only the root keep the site chrome and re-render the rest;
  disjoint chains replace the shell with a full fetch, still without a
  hard reload. New doc: `gofastr docs layouts`.
- **Subtree partials.** Partial navigations send `X-Gofastr-From`; the
  server renders only the layout layers the two routes do not share
  and names the swap boundary in `X-Gofastr-Swap`. Shared chrome is
  never re-sent. Old runtimes (no `From` header) get the same bare
  partial as before, so a deploy mid-session degrades instead of
  breaking; a boundary the DOM doesn't have recovers with a full-page
  load. `App.RenderPartialFromResult` is the Go entry point.
- **Route prefetch.** `app.Preload(app.PreloadHover|PreloadVisible|
  PreloadEager)` on a registration lets the client fetch the route's
  partial before the click: on link hover/focus, when a link scrolls
  into view, or at idle. Prefetched entries live in a 4-entry,
  30-second cache the router checks before fetching; `invalidate()`
  selectors evict them; requests carry `X-Gofastr-Prefetch: 1` and
  skip session minting; routes that would open as overlays are never
  prefetched. Ships as the `preload` demand module. Apps that never
  declare a mode load none of it.
- **Scroll restoration.** History entries carry an id, positions are
  captured per entry (and persisted to `sessionStorage`), and Back,
  Forward, and reload land where the user left instead of at the top.
- **State-aware history.** A Back/Forward whose only URL change is
  in-page state, a widget deep-link or pane parameter, replays that
  state with zero fetches instead of re-rendering the screen, which
  used to discard the mounted widget (the v0.44.0 known issue; Forward
  across a deep link now works). Search/filter/page params still
  refetch. All runtime history writes go through the new
  `__gofastr._pushURL` choke point.
- **Versioned-asset policy.** Every text asset under `/__gofastr/*`
  carries a strong ETag with 304 revalidation and is served
  `immutable` exactly when its `?v=` matches the current content hash:
  `app.css` (now composed and fingerprinted once per process instead
  of re-concatenated per request), `runtime.js`, `color-scheme.js`,
  `manifest.js`, and the per-screen action scripts.
- `widget.RuntimeHash`, `widget.RuntimeModuleManifestJSON`, and
  `style.ContributedCount` are exported for hosts that address these
  assets themselves.

### Changed

- **BREAKING**: `app.RouteEntry.Layout` (a single, innermost layout
  name) is now `Layouts` (the layer-key chain) plus a `Preload` mode;
  the client route manifest's `layout` field is now `layouts` and the
  dead `cssChunk` field is gone. Anything reading the old fields must
  switch to the chain.
- **BREAKING**: `data-fui-layout` is emitted on every layout layer
  (was: outermost only) and is emit-only; the runtime's swap decisions
  read `data-fui-layout-key`/`-slot`. `data-fui-screen-group` remains
  emitted but no longer drives swap decisions.
- **BREAKING**: static export (`Router.RenderRaw`) now composes the
  same layout chain as live SSR, including the default layout, and no
  longer double-wraps `<main>`. Exported HTML for grouped screens
  changes shape accordingly.
- **BREAKING**: a `SubGroup` inheriting its parent's layout renders
  that layout once (the level keeps an addressable marker) instead of
  emitting a duplicate nested shell.
- **BREAKING**: live pages externalize the component catalog and
  runtime-module manifest into one content-addressed
  `/__gofastr/manifest.js` (loaded before `runtime.js`) instead of
  inlining ~3 KB gz of JSON into every page head. Static export and
  theme-variant pages keep the inline blocks. Code that scraped the
  inline `#gofastr-catalog` / `#gofastr-runtime-modules` elements on
  live pages should read `window.__gofastr_catalog` /
  `window.__gofastr_runtime_modules`.
- **BREAKING**: live pages stop referencing the whole-app
  `/__gofastr/actions.js` concat. SSR emits one content-addressed
  script per compiled action registry present on the page, and the
  `actionloader` demand module fetches the rest on navigation.
  `/__gofastr/widget/{id}.js` is now session-gated like the concat
  (it was previously ungated) and served private+immutable. The
  concat endpoint remains for static export and embeds.
- **BREAKING**: `style.Contribute` fragments are frozen into
  `app.css` at first render; a later contribution logs a warning and
  does not ship. Contribute at package init or before `Mount`.
- **BREAKING**: modules that write in-page state to the URL must use
  `__gofastr._pushURL`; a raw `history.pushState` around the router
  leaves `currentPath` stale and breaks scroll restore and the
  stateful-popstate diff. `history.state` now carries `{__fui: <id>}`.
- `app.ComposeLayouts` is internalized; layout composition goes
  through the chain renderer.
- Active-link highlighting (`aria-current`, `data-fui-match-prefix`)
  moved from the core runtime to the idle-loaded `activelink` module.
  SSR renders the initial state, so only the first client-side nav's
  highlight can lag by a frame. Core `runtime.js` is 12,313 B gz@6 /
  14,220 B gz@1 against the 12,800 / 14,336 budget lines.
- The live service worker precaches the `?v=<hash>` spellings of the
  shell assets alongside the bare paths; a static export's worker
  stays bare-path-only to match its query-free files.

### Fixed

- Client-side navigation to a dynamic route (`/items/:id`) in a
  different layout silently swapped the new screen into the old shell.
  The manifest lookup was literal-path only and missed patterns.
- Leaving a layout for a layout-less page kept the old chrome around
  the new content.
- SPA navigation dropped the `<article>` wrapper on article screens,
  so Reader Mode support lasted only until the first client-side
  visit.
- Runtime-module preload hints were `rel="modulepreload"` for scripts
  the loader injects as classic scripts. The preloaded response was
  never reused, so every hinted module downloaded twice. Hints are now
  `rel="preload" as="script"`.
- A hydration request racing DOM insertion marked the component id
  hydrated before checking the element existed, permanently blocking
  that id's behavior script.
- Group sibling navigation never moved focus into the swapped content;
  every layer swap now focuses the destination cell (nested cells
  carry `tabindex="-1"`).

## [0.59.0] - 2026-08-03

### Added

- **`gofastr verify`: contracts and semantic analysis.** A single
  pipeline that answers "is this still an idiomatic GoFastr application",
  where `go build` answers "does it compile" and `go vet` answers "is
  anything obviously wrong". It discovers the route table, entity
  declarations, permission strings, and rendering surface, then reports
  where they no longer hold, with the reason and the fix attached to
  every finding. See `gofastr docs contracts`.

  49 rules at launch spanning twelve capabilities: `routing`, `permissions`,
  `security`, `data`, `entities`, `architecture`, `rendering`,
  `accessibility`, `performance`, `testing`, `ai`, `meta`. Each is also a
  filter: `gofastr verify routing security` runs two of them.

  Rules are data, not error strings: every one carries a stable ID
  (`GOFASTR1002`), the consequence, the remedy, and a bad/good example
  pair, validated at init. `gofastr verify --list` prints the catalog;
  `--explain <rule>` prints one in full, and a mistyped ID gets
  closest-match suggestions rather than silence.

  Two of the routing rules catch failures that are otherwise silent.
  `GOFASTR1002` finds Express-style `:id` in a ServeMux pattern, which
  registers cleanly and 404s every real request. `GOFASTR1005` finds a
  lowercase method, which registers cleanly and 405s every real request.
  Neither produces a boot error or a log line today.

- **`GOFASTR1903`: auth configured but never mounted.** `auth.New(...)`
  builds a manager; without `auth.SessionMiddleware`, `auth.RequireAuth`,
  or `auth.BFF` in the chain, no request ever carries a user, so every
  signed-in caller gets 401 identically to a real intruder. The app looks
  configured and the login form works. This is not hypothetical: it
  shipped from the blueprint generator, which enabled the battery and
  never mounted the middleware. In a module with several binaries each is
  checked separately, by import reachability: one app's mount says
  nothing about another app's manager, which its binary never links.

- **Strict by default, relaxed only in writing.** Every rule is enforced
  at its declared severity; there is no opt-in. The two ways to say no
  both leave a trace: `//gofastr:allow(RULE) reason` waives one instance
  (a directive with no reason, an unknown rule, or one that stops matching
  anything is itself a finding), and `gofastr.contracts.yml`, or a
  `contracts:` block in `gofastr.yml`, relaxes a rule, a capability, or a
  path. Every relaxation is printed in the report footer, so a run that
  passes because half of it was switched off says so.

- **Semantic coverage** (`framework/semcov`). Line coverage says a
  statement ran; it cannot say a request ever reached a route through the
  real router, middleware chain, and auth check. `framework.TestHarness`
  now records six dimensions a suite genuinely exercised into
  `.gofastr/semantic-coverage.json`: routes (by registered pattern, not
  request path), permissions, roles held, entity CRUD operations,
  lifecycle hook firings, and published event types. The `testing`
  rules diff that against the discovered surface. Automatic for suites already using the harness;
  `framework.RecordSemanticCoverage(t, app)` for anything else;
  `GOFASTR_NO_SEMANTIC_COVERAGE` opts out. A missing manifest reports at
  info level and does not fail. A manifest that exists but misses a
  route does.

  Two distinctions carry the weight. A permission **denial** counts as
  coverage: a test asserting a rejection proves the boundary at least as
  well as one asserting a grant, and the failure worth catching is a
  check never reached at all. A hook firing is recorded only when
  something is *registered* for that lifecycle point: `ExecuteHooks`
  runs on every CRUD operation regardless, so crediting the call would
  hand every entity full hook coverage on its first request.

  `GOFASTR_SEMANTIC_COVERAGE=1` turns recording on for a *serving*
  process, so an integration test that builds the binary and drives it
  over real HTTP still counts. That shape never touches the test harness,
  and every route it exercised was previously invisible. The e2e test
  `gofastr generate` emits sets it.

  Roles (`GOFASTR1109`) invert the usual worry: a role granting too little
  surfaces as a broken feature someone reports, while one granting too
  much surfaces as nothing at all until it is used.

  Hooks (`GOFASTR1107`) and event subscribers (`GOFASTR1108`) are the two
  surfaces with no callers to follow: nothing invokes them by name, so a
  rename on the emitting side leaves the subscriber compiling, the suite
  green, and the notification never sent.

- **`access.SetObserver`, `hook.SetObserver`, `event.SetObserver`**:
  evaluation, firing, and emission callbacks, the same inversion
  `router.SetServeHook` uses. Test tooling installs them;
  `framework/access`, `framework/hook`, and `framework/event` stay
  dependency-free leaves and production pays one atomic load per check.

- **Baselines: the adoption ratchet.** `gofastr verify --baseline-write`
  records every current finding into `.gofastr-contracts-baseline.json`;
  subsequent runs pass on that debt and fail on anything added. Without
  it, strict-by-default and an existing codebase pull against each other:
  hundreds of findings at once, and the realistic response is to turn the
  tool off or downgrade every rule to warn.

  Counts are keyed by (rule, file) rather than by line, so a reformat does
  not invalidate the baseline: moving a finding within a file keeps it
  accepted, adding one more does not. When findings are fixed the report
  says which entries are now over-accepting, because that slack is exactly
  where a new finding could hide; re-recording is what makes it a ratchet
  rather than a mute button.

  Only *gating* findings are recorded: anything below the run's fail-on
  severity is skipped, because an entry for a finding that cannot fail the
  run would absorb it on every later run and silence a signal the project
  deliberately kept visible.

  This repository now carries one (65 findings) and its CI gate runs
  `--strict`, so warnings gate too. Before, the gate could only fail on
  errors and a change adding fifty unguarded mutations would have passed.

  Its config downgrades the semantic-coverage rules to `info`. Those
  record *which tests ran*, which differs by environment, CI excludes
  some chromedp packages and runs Postgres suites a laptop skips, so
  gating a shared CI on a recorded baseline of them is flaky by
  construction. Verified rather than assumed: halving the manifest's
  recorded routes turned `verify --strict` from exit 0 into exit 1 before
  the downgrade, and leaves it at 0 after.

- **`--changed`: verify only what this change touched.** `gofastr verify
  --changed` reports findings in uncommitted files (including untracked
  ones); `--changed=main` reports everything since the branch forked, from
  the fork point rather than the tip. The pre-commit, dev-loop, and
  PR-review question is the same narrower one, and a whole-tree report
  answers a different question.

  The analysis still runs over the whole tree, because the route table, entity
  list, and coverage manifest are only meaningful whole, so a duplicate
  route introduced by editing one file is still found. Only reporting
  narrows, and the run states how many findings it withheld.

  The repository's `.githooks/pre-commit` uses it, so a commit that adds a
  contract finding fails locally before CI sees it, with a report scoped
  to the files that commit touches.

- **`gofastr dev` reports contract findings after each reload**, scoped to
  what changed, the practical approximation of the RFC's inline-diagnostics
  goal. It runs *behind* the restart, never before it: a second added to
  every save is the difference between a loop people use and one they turn
  off. The output is one line per finding with the rule ID, capped and
  deduplicated, so the reasoning stays in `verify --explain` rather than
  burying the loop. Fixing the last finding prints `contracts: clean` once.

- **Machine-readable output.** `--json` emits every diagnostic with its
  whole rule attached, so an agent handed one finding can act on it
  without a second call. `--sarif <file>` writes SARIF 2.1.0 for GitHub
  code scanning and IDE inline diagnostics, declaring the analysed root in
  `originalUriBaseIds`: artifact URIs are relative to that root, which is
  not necessarily the repository root, and without the declaration a
  consumer assumes repo-root and maps every annotation onto a path that
  does not exist, silently. `--fix` applies the
  mechanical fixes and re-verifies. Fixes are gofmt-ed after application,
  so an edit only has to be syntactically correct rather than reproduce
  the surrounding indentation. That is what makes inserting fields into
  a multi-line composite literal safe. A file's original line endings
  survive: gofmt emits LF, so CRLF is restored afterwards. Without that,
  a one-line fix in a Windows working tree rewrote every line in the file. `GOFASTR1005` (uppercase the
  method), `GOFASTR1404` (add the missing cookie attributes), and
  `GOFASTR0002` (delete a stale suppression) ship fixes. Every edit
  records the text it expects to replace and is refused if the file
  changed since analysis or the result would no longer parse; an aborted
  pass names the files it already rewrote. `--fix` also says when
  nothing could be fixed, and why. The report admits its blind spots:
  `unparsed` counts files the parser rejected, so "no findings there"
  never reads as "clean".

- **Deliberate non-goals**, documented in `gofastr docs contracts` so they
  do not read as oversights: middleware-execution coverage (it would need
  a permanent per-request wrapper for a rule that cannot realistically
  fire, since `app.Use` middleware runs on every request by definition)
  and rules over `framework/experimental/apiversions` (pinning an
  experimental API's shape is the opposite of what experimental is for).

- **MCP contract tools.** `WithMCPIntrospection()` now also serves
  `contracts_list`, `contracts_explain`, and `contracts_capabilities`, so
  an agent connected to a live app can read what the framework expects
  before writing code rather than after a build rejects it.

- **MCP contract tools, dev loop.** Under `gofastr dev`, `contracts_verify`
  runs the analyzers over the app's source and returns structured
  findings, and `contracts_fix` applies one rule's autofixes and reports
  the files it changed, so an agent can verify, explain, and fix without
  shelling out. Both touch local source files, so neither is registered
  outside the dev loop: a production `/mcp` does not gate them, it does
  not have them.

- **A `go vet` stage in every mode**, with its outcome in every report
  (`vet.ran` / `vet.passed` / `vet.skipped`). A failing vet fails the
  run: an empty diagnostic list on a tree that will not compile means
  "could not look", not "nothing found".

- **`check.ScanInlineScriptsIn`** (`core-ui/check`): a per-file entry
  point for the inline-script linter, so callers that already hold a
  parsed AST (the contracts pass, on every dev-loop save) can lint
  without re-reading and re-parsing the tree.

- **Custom rules.** `contracts.RegisterRules` and `contracts.Register`
  accept project-defined rules and analyzers alongside the built-in
  catalog, and they ride the whole pipeline: config severity and `off`,
  exemptions, `//gofastr:allow` with a mandatory reason, the baseline
  ratchet, text/JSON/SARIF output, and the MCP catalog tools. Custom IDs
  use their own uppercase prefix (`ACME101`); the `GOFASTR` namespace and
  its per-capability number blocks stay reserved for the built-in
  catalog, so a suppression written today cannot collide with a future
  release. A worked example, a project gate command with its own rule,
  is in `gofastr docs contracts` under "Your own rules".
### Fixed

- The `examples/ecommerce` flagship test no longer regenerates the committed
  example into the working tree. It ran the generator with `--force` in the
  repo directory, so a suite run rewrote tracked `app/` files, dirtying the
  tree before a commit, and silently reverting if anyone ran
  `git checkout -- examples/`. It now emits into a gitignored scratch package
  inside the module, the same arrangement `examples/meridian` already used.
  The assertion the overwrite was standing in for is now explicit:
  `TestCommittedAppMatchesGeneratorOutput` diffs generator output against the
  committed `app/` and names the stale file, so drift is a failing test rather
  than a dirty tree nobody reads. (#176)

## [0.58.0] - 2026-08-03

### Fixed

- **A Go map or slice `Default` on a `schema.JSON` field emitted invalid DDL**
  (`framework/migrate`). `SQLDefault` had no arm for the shapes such a default
  naturally takes, so a map fell to the `fmt.Sprintf("%v")` fallback and
  rendered `DEFAULT 'map[a:1]'`, which Postgres rejects on a `JSONB` column,
  failing `AutoMigrate` at boot. SQLite's column is `TEXT`, so the same
  declaration stored the literal text and looked fine: the identical dialect
  split as #174. The value was already correct in the two other places it is
  used: `schema.validateJSON` accepts maps and slices, and
  `crud.marshalJSONColumn` marshals them on the insert path. Only the DDL
  rendering was wrong. It now marshals to JSON *before* quoting, which keeps
  the deliberate literal-escaping intact rather than replacing it. (#178)

## [0.57.0] - 2026-08-03

### Breaking

- **BREAKING: a malformed field `Default` now fails registration.** The change
  surfaces an existing bug rather than creating one: the declaration was
  already broken, it just failed per-request instead of at boot. But an app
  that starts today can stop starting, so treat it as breaking. `App.Entity`
  panics as it already does for other declaration errors (relation without
  `To`, duplicate fields, wire-key collisions); `TryEntity` returns the error.
  Fix the declaration the message names.

### Fixed

- **Field `Default` values are validated at registration** (`framework/entity`).
  A `Default` is the value `crud.doCreate` substitutes for a field the request
  body omitted, and it reached the driver through the same column as a
  client-sent value but through none of the same checks: `ValidateAll` ran
  over the body and the `Default` was applied afterwards. A caller who *sent*
  the bad value got a 400 naming the field; a caller who *omitted* it got a
  500 with nothing actionable in it. `Entity.Validate` now runs
  `schema.Validate` over every `Default`, so a malformed one fails the
  declaration at boot with the field named. The clearest case was
  `{Type: schema.JSON, Default: "draft"}`: 500 against Postgres `JSONB`, and
  stored unchanged in SQLite's `TEXT`, the same declaration broken on one
  dialect and silently fine on the other. Auto-generated fields are exempt
  (their `Default` is never an insert value; it survives only as the column's
  DDL `DEFAULT`). Two Go spellings a JSON body cannot produce are normalized
  before validating rather than refused: a `schema.Decimal` default written as
  a Go number (what `gofastr generate` emits for `default: 0`) and a
  `Timestamp`/`Date` default written as a `time.Time`. (#174)

## [0.56.0] - 2026-08-02

Windows support, and the SQLite driver swap it required. `mattn/go-sqlite3`
needs cgo, which is why `CGO_ENABLED=0` builds did not work; the replacement
is pure Go. `GOOS=windows`, `GOOS=linux` and `GOOS=darwin` all build with
`CGO_ENABLED=0`.

Swapping the driver moved several behaviours that only surfaced under the new
one. Sessions were the worst: modernc binds `time.Time` in Go's `String()`
format, which no parser in the repo accepts, so every stored session read back
as the zero time and looked expired. The browser E2E suites had stopped running
on macOS at the same time and reported `PASS` while skipping, which is why it
went unnoticed. Both are fixed here.

This release also carries the migration work (`SERIAL`, type-change detection,
column renames), cron leader election, argon2id hashing, and Reader Mode.

### Breaking

- **BREAKING: the `sqlite3` driver is `modernc.org/sqlite`.**
  `github.com/DonaldMurillo/gofastr/sqlite/stdlib` registers it, and
  generated apps import it. `mattn/go-sqlite3` is out of `go.mod`. A host
  that still imports `mattn/go-sqlite3` keeps cgo and wins the `sqlite3`
  name, because whichever package registers it first is the one that
  stays. Drop the import.
  Timestamps bind with `_time_format=sqlite`, which is byte-identical to
  what mattn wrote, so existing database files read back unchanged and no
  migration is needed. `busy_timeout` defaults to mattn's 5000ms rather
  than modernc's 0, so a second writer waits instead of failing
  immediately with `SQLITE_BUSY`. Set either explicitly in your DSN to
  override.
- **BREAKING: `sql.Open("sqlite", …)` no longer reaches the in-repo
  engine.** `github.com/DonaldMurillo/gofastr/sqlite` registers itself as
  `gofastr-sqlite`; the `sqlite` name belongs to modernc.
- **BREAKING: boolean columns serialize as `true`/`false` in CRUD JSON.**
  On mattn they came back as `0`/`1`. A client testing `row.active === 1`
  has to change to `row.active === true`. Apps that ran on the in-repo
  engine already saw `true`/`false` and are unaffected.
- **BREAKING: new storage keys must be portable to Windows.**
  `LocalStorage.Save` rejects keys whose path components contain `:`, end
  in a space or dot, or name a reserved device (`CON`, `NUL`, `LPT1`, …),
  so a store written on Unix can be served from Windows. `Get`, `Exists`
  and `Delete` still accept them, so objects written by earlier releases
  stay readable and removable. Path-traversal rules are unchanged and
  still apply to every operation.
- **BREAKING: repeated markdown headings get distinct ids.** A second
  `## Setup` renders as `id="setup-2"` instead of a duplicate
  `id="setup"`. Anchor links to the later heading change.

### Added

- Windows file ACLs for storage, logs, uploads and kiln freeze snapshots,
  restricting each to the owning account. Unix keeps the `0o600`/`0o700`
  modes it already used.
- `data-fui-disclosure-persist` on a `<details data-fui-disclosure>` keeps
  it open across in-shell SPA navigation. Shell controls such as a sidebar
  want this; an ordinary dropdown does not, and still closes.
- Lightbox prev/next clicks and arrow keys that land while `lightbox.js`
  is still downloading are replayed once it arrives, instead of being
  dropped.
- `internal/browserpath` resolves Chrome, Chromium or Edge for the
  chromedp suites on all three platforms, including macOS `.app` bundles.
  `GOFASTR_BROWSER_PATH` overrides it.

- AutoIncrement integer primary keys render `SERIAL` on Postgres (was plain
  `INTEGER`, which has no sequence); the column is omitted from INSERT so the
  Postgres sequence / SQLite `INTEGER PRIMARY KEY` rowid alias assigns it.
  `auto_generate: increment` now works on both dialects.
- The schema diff detects column type changes and surfaces them as destructive
  changes (refused by default), with dialect-aware normalization so Postgres
  `information_schema` names don't false-positive. Previously silently ignored.
- `EntityConfig.Renames` (`old`→`new`) emits a non-destructive `RENAME COLUMN`
  instead of a data-losing drop+add. Go-declared opt-in (blueprint YAML to come).
- `Scheduler.WithLeaderElection` + a `LeaderElection` interface make cron safe
  across replicas (per-tick mutual exclusion); ships a `PostgresAdvisoryLease`
  backed by `pg_try_advisory_lock`.
- `argon2id` password hashing via `Argon2Hasher`; `CheckPassword` auto-detects
  the algorithm so a user table migrates gradually. Bcrypt stays the default.
- `gofastr audit lint` flags the `unscoped-pii` rule in Go-declared
  `app.Entity(...)` too, not just in `gofastr.yml` blueprints.
- New doc→owner parity gate catches `data-fui-*` attributes documented with no
  emitter/reader; `TagInput`'s `data-fui-tag-input-id` now has a runtime owner
  (focus returns to the field on chip remove).
- Browser Reader Mode: `app.AsArticle()` (a screen registration option) and
  the optional `ScreenArticle` interface mark a page's content as an article
  so Safari Reader / Firefox Reader View offer their built-in reader view.
  The framework wraps the content in `<article>`, emits `Article` JSON-LD,
  and sets `og:type=article`; the headline and description are derived from
  the screen's `ScreenTitle` / `ScreenDescription`, so a normal screen
  becomes reader-ready with no article-specific data. Implement
  `ScreenArticle` to add a byline, date, or cover image.

### Fixed
- SSE streams no longer die at the request timeout (issue #159). A live
  subscriber was cut at `middleware.Timeout`'s 30s deadline, and the naive
  fix (clearing the connection's read/write deadlines, reverted in #158)
  stranded streams instead, exhausting the browser's per-origin connection
  pool. The stream loop now ignores the request context's
  `DeadlineExceeded` (the timeout firing on a still-connected client) while
  unwinding on a real `context.Canceled` (client disconnect); a heartbeat
  (`island.WithSSEHeartbeat`, default 15s) keeps live streams writing, and a
  bounded stream lifetime (`island.WithSSEStreamBound`, default 5m) reclaims a
  stranded stream even when its heartbeat writes keep succeeding into the
  kernel buffer. Read/write deadlines are no longer cleared, so net/http's
  close-notify (and thus prompt disconnect detection) stays intact.

- `ui.SiteHeaderLink.MatchPrefix` activates on canonical hrefs with no
  trailing slash: `/docs` now lights up on `/docs` and `/docs/getting-started`
  (and still not on `/docs-old`, since matching is on segment boundaries).
  The runtime previously prefix-matched only hrefs ending in `/`, which made
  the attribute inert for apps that register `/docs` rather than `/docs/`.
  Applies to the initial render and to client-side navigation, desktop and
  mobile. (#171)
- A `schema.JSON` field is writable through the generated CRUD routes. The
  handler bound the decoded Go value straight to the driver, so every create
  or update that populated one failed with a 500 (`unsupported type
  map[string]interface {}, a map`), leaving the field unreachable through any
  supported interface. Values are now marshalled on write and parsed on read,
  so a JSON document round-trips as a document on create, update, get, list,
  cursor pages, `?stream=true`, and `?include=` rows. A string still means
  JSON text and is stored verbatim; text that is not JSON reads back
  unchanged. Note the read change is visible to existing clients: a JSON
  column that used to arrive as a string now arrives parsed. (#170)
- `/openapi.json` no longer advertises CRUD paths for entities with
  `Exposure.CRUD: false`. The router honoured the opt-out and the spec did
  not, so the spec documented a management API the server answered 404 for,
  worst exactly where the opt-out was deliberate, and generated SDKs shipped
  methods that could not work. Only the generated surface goes: the entity's
  schema component stays, and so do hand-written `Endpoints` (the router
  mounts those whether or not auto-CRUD is on). An unset `CRUD` still means
  enabled. (#169)
- Boot `AutoMigrate` no longer applies `RENAME COLUMN` (additive-only); a stale
  `Renames` hint can no longer rename the wrong in-use column at startup.
- `Scheduler.runTick` no longer blocks the run loop on in-flight jobs, so
  `StopContext`'s bounded shutdown works again for jobs that ignore their context.
- `UpsertOne` no longer injects `id=0` for an AutoIncrement PK, which clobbered
  rows on repeated upsert.
- Removed three stale `data-fui-*` doc rows (`inline-edit`, `password-toggle`,
  `repeater`) that documented attributes nothing emits or reads.
- `gofastr audit lint`'s `isRenameChange` no longer mistakes an
  `ADD COLUMN ... DEFAULT 'rename column'` for a rename (it matched the
  operation token space-delimited now), which had silently dropped a legit
  ADD COLUMN at boot.
- A rename combined with a type change now emits both (the type-change loop
  resolves a renamed column's live type under its old name); previously the
  type change was silently lost.
- A column declared with `RawType` (Postgres domains, arrays, custom types)
  no longer false-positives as a type change on every diff.
- A panic inside a cron `RunOnce` (e.g. a panicking gate) now still releases
  the leader-lease instead of leaking it (the release is deferred).
- The Meridian canary's Customers list-island now states its policy
  explicitly (`WithIslandPolicy(authPolicy("/login", ""))`), matching the
  contract `gofastr generate` emits on every island, instead of leaning on
  `TableHandler`'s nil-policy sign-in fallback, which silently stops gating
  the rows the moment the screen gains a role. (#160)

### Security

- `Argon2Hasher.Verify` rejects resource-exhaustion parameters parsed from a
  stored hash (memory up to ~4 TiB, unbounded time) before invoking
  `argon2.IDKey`. A malicious or corrupted password-hash row could otherwise
  allocate gigabytes or peg a CPU on every verify (per-login DoS). Legitimate
  hashes sit far below the caps.

## [0.55.0] - 2026-07-31

Five independent analyses of the whole framework, three in-repo passes
plus two external models with none given the others' findings, turned up
bugs the v0.54 sweep did not reach. This release fixes all of them, each
with a test that failed first. A three-model review of the resulting
branch then found four more, including a regression this wave itself had
introduced into the timeout middleware; those are fixed here too.

A second six-reviewer round (Claude, GLM and Sol, each over half the
diff) then found nine more, including a data leak this branch had itself
introduced: the generated list-island endpoint served rows without the
screen's own gate. Three reviewers independently hit entity-registration
atomicity and the static ETag cache. Those are fixed here as well.

### Breaking

- **BREAKING: `openapi.SwaggerUIHandler` is now `openapi.DocsHandler`.**
  It takes a third argument, `public bool`, which gates the landing page
  and its nested spec route together. `framework` passes `PublicOpenAPI`,
  so `WithPublicOpenAPI()` now opens `/api/docs/` as well as
  `/openapi.json`. Previously the page was auth-gated unconditionally,
  which left a published spec with no browsable page. The startup banner
  says "API docs" instead of "Swagger UI"; the handler serves a static
  landing page and has not embedded Swagger UI since the CDN reference
  was removed.
- **BREAKING: `island.Manager` presence hooks are set through methods.**
  The public `OnPresenceChange` and `AuthorizeTopic` fields are gone; use
  `SetOnPresenceChange` and `SetAuthorizeTopic`. Apps assigned the fields
  without the lock the hooks were read under. `ConnectSession` now
  returns `(ch, cancel, error)` so it can refuse a connection over the
  new stream caps.
- **BREAKING: dead widget-builder API removed.** `BootstrapMode`,
  `Definition.Bootstrap`, `Definition.BootstrapPath`, `Builder.Bootstrap`,
  `Builder.RPCWithSignal`, and `RPCEndpoint.ResponseSignal` are deleted.
  `Mount` never applied the documented bootstrap default and
  `ResponseSignal` was written but never read; signal routing goes
  through the `data-fui-rpc-signal` attribute.
- **BREAKING: production refuses the in-memory session store.** With
  `DevMode: false` and no `SessionStore`, `Init` now returns an error
  instead of logging a warning. Sessions in process memory do not survive
  a restart and never resolve on a second replica. Set
  `AllowInMemoryStores: true` to acknowledge a single-node deployment, or
  wire `EntitySessionStore`. The in-memory 2FA store already failed this
  way; both stores now behave the same.
- **BREAKING: `battery/semantic` routes require a bearer token.** The
  handler accepted any non-empty `Authorization` header, so
  `Authorization: x` granted index write, query, and delete. Configure
  `semantic.WithAuthToken(token)` (compared in constant time) or
  `WithInsecureDisabledAuth()` for local development. With neither, every
  route returns 401 rather than mounting open.
- **BREAKING: `gofastr init` writes `gofastr.isolation.yml`.** The
  isolation config used the blueprint's `gofastr.yml` filename, so
  following the CLI's own example sequence failed with `unknown key
  "version"`. `framework/isolation` reads the new name first and still
  falls back to `gofastr.yml`.
- **BREAKING: an over-length `?field_in=` list is a 400.** Above 1000
  entries the parser silently dropped the remainder, narrowing the
  predicate without telling the caller. It now errors, matching the
  include-scoped IN cap.
- **BREAKING: `AutoTimestamp` writes microsecond precision.** Values were
  whole seconds, so rows written in the same second shared a
  `created_at` and stalled single-field cursor pagination on the tie. The
  format is fixed-width (`.000000`) because SQLite compares these as
  strings and a zero-stripped fraction sorts wrongly.
- **BREAKING: out-of-range integers fail to bind.** `core/handler`
  parsed every integer at 64 bits and let `SetInt` truncate, so
  `?small=300` bound 44 to an `int8` with a nil error. Binding now uses
  the destination's bit width and returns a range error.
- **BREAKING: island endpoints enforce the entity's read permission.**
  `resource.Config.TableHandler` checked only that someone was signed in,
  so a list's sort/paginate endpoint served rows that `GET /api/<entity>`
  refused with 403. It now also applies the entity's declared read
  permission (via the new `crud.CrudHandler.CanRead`) and the screen's own
  policy (the new `Config.IslandPolicy`). A role-gated screen MUST set
  `IslandPolicy`; a public one declares `resource.PublicIsland()`, because
  with no policy the handler still requires sign-in.
- **BREAKING: generated island endpoints moved to
  `/api/tables/<screen>/<entity>`.** They were one per entity, mounted on
  the bare registry entry, so two screens showing the same entity shared
  one endpoint, and a sort click on a filtered list came back unfiltered.
  Each screen now gets its own endpoint serving that screen's refined
  config behind that screen's policy.
- **BREAKING: entity registration rejects route collisions it used to
  half-commit.** `TryEntity` pre-flighted a guessed pair of paths; it now
  asks `crud` for the full set it will mount (the new
  `crud.CrudRoutePatterns`) and compares wildcard SHAPE, so a screen at
  `/posts/{slug}` and an endpoint shadowing `/posts/_batch` are both
  caught during validation. Declarations that previously panicked
  mid-commit, leaving the entity in the registry and its CRUD routes
  mounted, now return an error having registered nothing.
- **BREAKING: `RedisClient.RPop` must report an empty list as
  `queue.ErrRedisEmpty`.** `Dequeue` used to treat any `RPop` error as an
  empty queue, which masked a backend outage as an idle worker. It now
  surfaces every error except that sentinel. `RedisClient` is a public
  interface hosts implement, so an adapter returning its driver's own
  nil-sentinel (go-redis's `redis.Nil`) now errors on every idle poll
  rather than reporting `ErrNoJob`. Map it with `errors.Is` in the wrapper.
- **BREAKING: production re-checks the session store after plugin init.**
  A plugin's `Init` could call `SetSessionStore(NewMemorySessionStore())`
  after the only fail-closed check, so `Init` returned nil with every
  session in RAM. The gate now runs again once plugins are initialised.

### Fixed

- **`DisableRequestTimeout` also drops the server read/write timeouts.**
  Setting it removed the middleware deadline but left the server's fixed
  60s limits in place, so a request that legitimately runs longer, such as an
  upload over a minute, was still cut off.
- **`TryEntity` rejects a declaration without publishing part of it.**
  Entity validation, the CRUD/MCP contract, route collisions (including
  custom endpoint paths), and MCP tool names are all checked before the
  registry, router, or MCP server is touched. A rejected declaration used
  to leave its registry entry and routes behind, which made the corrected
  retry fail under the same name.
- **`MemorySessionStore` no longer races on two-factor state.** `Get` and
  `Create` returned the stored `*Session` while the marker methods
  mutated it under the store lock; both now return a copy taken under the
  lock. `go test -race` reported three races on authentication state.
- **The Redis queue no longer loses jobs.** A failed processing-hash
  write after `RPOP` dropped the job entirely; it is now pushed back.
  Attempts are counted at claim (matching the DB backend), so a worker
  that crashes before `Nack` cannot redeliver a poison message forever. A
  backend error during dequeue is returned instead of being reported as
  an empty queue.
- **`core/static` serves whole files.** Files over 32MB were sent
  truncated under a matching `Content-Length` and ETag. On a filesystem
  whose handles do not implement `io.Seeker`, the body was empty after
  the ETag hash consumed the file; the handler now reopens. Read errors
  other than "not found" return 500 instead of 404, and the content
  digest is cached per file rather than recomputed per request.
- **`$N` inside a SQL string literal is no longer rewritten.** The
  placeholder renumberer was quote-blind, so `label = '$5 off'` became
  `'$1 off'` and shifted the real placeholders. Renumbering stays
  positional, which is what `entity.Condition` composition depends on.
- **MCP SSE frames carry their payload intact.** The writer collapsed
  newlines and rewrote any `event:`/`data:` substring in the body,
  corrupting JSON-RPC content. It now emits one `data:` line per payload
  line, so a spec-conforming client reassembles the exact bytes.
- **The pure-Go SQLite driver honors its DSN and shares one engine per
  pool.** `sql.Open("sqlite", "file.db")` returned a fresh in-memory
  engine and discarded every write. Each pooled connection then got its
  own engine over the same file, with separate page caches and no
  locking: 25 of 50 concurrent inserts survived. A corrupt schema page
  now surfaces as an open error instead of reading as an empty database.
- **`gofastr migrate` with no subcommand runs `up`** instead of panicking
  on a slice bounds error.
- **The `.ui.go` sandbox lint runs in `gofastr build` and `gofastr dev`.**
  The goroutine, channel, and import rules were implemented and tested
  but had no caller outside their own tests, so a `.ui.go` file with
  `go func()` or `import "os"` passed the build.
- **Concurrent SSE streams are capped** at 16 per session and 4096 per
  replica (`island.WithStreamCaps`). An over-cap connect gets a 429 with
  `Retry-After`; existing streams are never dropped to make room. Any
  same-origin caller could previously hold unbounded streams, each with a
  goroutine and a 64-slot channel.
- **Per-theme component CSS is evicted** when a theme variant is
  released, and `MemoryBurnStore` prunes expired nonces. Both grew for
  the life of the process.
- **Failed writes are reported.** Admin RBAC and process-module audit
  rows, webhook delivery-state updates, idempotency `Finish`, and
  password-reset email sends discarded their errors while the surrounding
  docs promised the write happened. Each now logs.
- **Client runtime:** a slow page load can no longer swap its content
  over a newer navigation; `data-fui-confirm` is honored inside widgets
  (a delete in a drawer fired without confirming); an SSE island swap
  loads the CSS for components it introduces; repeated form fields keep
  every value instead of the last; computed and animate teardown runs on
  island swaps, not only on navigation; two lightboxes on one page no
  longer share state.
- **Redis `Nack` no longer loses the job.** It deleted the processing
  entry before pushing to the retry or dead-letter list, so a failed push
  left the job in no list and invisible to `Reclaim`. It now writes the
  destination first; a failure between the two re-delivers rather than
  drops, which is what an at-least-once queue promises.
- **`sql.DB.Close` releases the sqlite engine's file.** `Pager.Close`
  dropped the page cache without closing the backing `*os.File`, so a
  program that opened and closed databases in a loop exhausted its
  descriptor limit while every handle looked closed.
- **Static files:** the content-digest cache is per handler, not
  process-wide. `embed.FS` reports the zero modtime for every file, so
  two handlers over different filesystems serving a same-name, same-size
  file shared an ETag and answered `304` for content the client had never
  seen. A path that is not valid UTF-8 (`/%ff`) is now a 404 rather than a
  500. `fs.ValidPath` rejects it as `fs.ErrInvalid`, which any client
  could use to drive a 5xx.
- **SQL placeholders inside string literals are left alone.** The
  renumberer understood `'…'` but not `$$…$$`, `$tag$…$tag$`, or the
  backslash escapes `E'…'` allows, so a `$5` inside a literal was
  rewritten and every real placeholder after it shifted.
- **The semantic bearer check is constant-time in the token's length.**
  `subtle.ConstantTimeCompare` returns immediately when lengths differ,
  so comparing raw tokens still revealed when a candidate was the right
  length. Both sides are hashed to 32 bytes first.
- **A form field named after an `Object.prototype` member posts its
  value.** The multi-value merge read `obj[k]` on a plain object, where
  `constructor` and `toString` resolve up the prototype chain and are
  never `undefined`, so a single such field posted `[null, value]`.
- **SPA navigation A→B→A leaves the URL and the content agreeing.** The
  in-flight dedup returned before the epoch bump, so re-requesting an
  earlier path never became the current intent and the page showed B at
  the URL `/a`.

### Changed

- **The RPC engine ships as a demand-loaded module.** `frag/rpc.js` is
  gone; the network machinery (dispatch, CSRF, form encoding, abort
  controllers, response-header effects, the kiln tool POST) now lives in
  `src/rpc.js`, and the core bundle fell from 14,338 to 12,368 gzip bytes
  , from 2 bytes over the 14,336-byte initial congestion window to nearly
  2 KB under it. A page carrying `[data-fui-rpc]` or `[data-kiln-tool]`
  starts fetching the module at boot, fire-and-forget, so it is normally
  resident before the first click; core keeps one delegated bridge that
  calls `preventDefault()` synchronously and then awaits the module, so a
  click landing mid-download is still dispatched rather than falling
  through to a native submit. A module that never arrives (a host serving
  `runtime.js` but not `/__gofastr/runtime/<name>.js`, or a network blip)
  warns rather than swallowing the action, and a non-JSON intercepted form
  falls back to a native submit. Prevented-then-never-dispatched would
  lose the submission silently, which could not happen while RPC was
  compiled into core. Static exports keep `rpc-stub` and never request the
  module. Client-only signal mutations
  (`data-fui-signal-set`/`-inc`/`-toggle`) moved to the signals fragment
  and work with no module load at all; the same-origin guards moved to
  the kernel, which also deletes the copy `rpc-stub` was carrying. No
  public Go API changed.
- Generated blueprint apps sort and paginate list screens through the
  resource island endpoint rather than a full page navigation, and no
  longer emit `.gofastr-entity-*` or `.detail-*` markup. The dead
  client-JS list and detail renderers are deleted.
- `battery/admin` composes `ui.DataTable`, `ui.StatCard`,
  `ui.FilterToolbar`, and `ui.Tag`; its second stylesheet (a `baseCSS`
  string of unprefixed `.card`/`.btn`/`body`/`table` rules served outside
  the registry) is gone, along with two `.badge` classes that had no CSS
  anywhere.
- `<html lang>` is configurable through `app.WithLang` and
  `uihost.WithLang`, defaulting to `en` (WCAG 3.1.1). It was hardcoded on
  every rendered surface.
- SSR-inlined widget chrome renders with the request context, so a
  context-aware slot no longer renders anonymous on first paint and
  personalized when reopened.
- The route table and component catalog are serialized once instead of on
  every request.
- Documentation corrections, with tests pinning them to the code:
  `auth.md` described a `framework/auth` package that does not exist and
  argon2id hashing (it is bcrypt in `battery/auth/password.go`), and said
  password reset fails closed when it returns 200 and sends nothing;
  `security.md` used two `CORSConfig` fields and a `Tracing` signature
  that do not exist, named a `gofastr export` subcommand that does not
  exist, and said `app.Use` disables the default middleware chain;
  `query-dsl.md`'s quickstart did not compile and omitted that the DSL
  applies no owner, tenant, or soft-delete scoping;
  `hooks-and-transactions.md` had `BeforeCreate`/`BeforeUpdate` running
  after validation when they run before.
- A second documentation pass, likewise pinned by tests, caught pages
  teaching APIs this release deletes: `widgets.md` opened its quickstart
  with `Builder.RPCWithSignal` and listed `Definition.Bootstrap`, and
  `presence.md` and `live-dashboards.md` assigned to
  `Manager.AuthorizeTopic` / `OnPresenceChange`, now setters. The
  roster-disclosure remediation snippet on the security page was among
  the samples that did not compile. Also: `isolation.md` pointed at
  `gofastr.yml` when `gofastr.isolation.yml` wins the discovery order, so
  following it configured nothing; `semantic-search.md` documented 200s
  for routes that answer 401 without a token; `queue.md` told you to
  write a `RedisClient` adapter without mentioning that `RPop` must map
  its driver's empty-sentinel onto `queue.ErrRedisEmpty`; `auth.md`'s
  `UserStore` omitted `UpdateRoles`; `access-control.md` used a
  `framework.Wildcard` that is not re-exported; `query-dsl.md` named a
  `framework.ParseDSL` that does not exist and claimed an uncapped
  `limit()` executes when the parser rejects anything over 10,000; and
  the README twice called `/api/docs/` "Swagger UI", the claim this
  release renamed the handler to stop making.
- `gofastr test` and `gofastr build` document the flags they actually
  parse, where the help advertised `-v`, `-count N`, `-race`, `-run <regex>`
  and a bare `-o <output>` that the parsers ignore or reject, and
  `gofastr init` names `gofastr.isolation.yml` in its created-files list
  instead of the `gofastr.yml` it does not write.

## [0.54.0] - 2026-07-31

The zero-carryover release: a three-agent deep analysis of the whole
framework at v0.53 concluded the feature surface is complete but the
contract is not freeze-ready, and this release clears everything the
analysis found: every deprecated field, every dead exported surface,
every advertised-but-unimplemented behavior, and every place the docs
contradicted the code. Every behavioral fix landed with a test that
failed first.

### Breaking

- **BREAKING: the generated resource engine is a framework package.**
  Blueprint apps used to own a ~946-line generated `resource.go`
  containing generic list/detail/form/filter/pagination machinery, a
  private fork that framework fixes could never reach. The engine now
  lives once in `framework/ui/resource` (Config/Field/Relation/Filter/
  Transition/RelatedList + a per-app Registry), and the generated
  `resource.go` is a 34-line thin seam. Each entity's `resource.Config`
  lives in its own generated screen file, so `generate --add` stays
  additive and never rewrites the seam. `--force` removes the retired
  generated `resource_test.go` only when it carries the generated
  marker. The flagship test now proves OpenAPI paths match the live
  mounted routes (the API-prefix parity oracle both agent evals tripped
  on), executes a real MCP tool call through the auth path, and drives
  a full REST create→get→update→delete cycle.
- **BREAKING: framework facade re-exports are functions, not assignable vars.**
  The subpackage function re-exports in `framework/reexports_*.go` (e.g.
  `framework.AutoMigrate`, `framework.NewCrudHandler`, `framework.Define`) are
  now real wrapper functions with identical signatures. Assigning to them
  (`framework.X = myReplacement`) or taking their address (`&framework.X`) no
  longer compiles. Global mutation of the public API is gone. Plain call sites
  `framework.X(...)` are unchanged.
- **BREAKING: `WithConfig` replaces `AppConfig` wholesale.** It no longer
  merges field-by-field into prior options. Later options win (including
  granular setters placed after it), and a field `WithConfig` leaves at the zero
  value becomes the zero value, not "keep whatever a previous option set".
- **BREAKING: removed deprecated UI shims.** `framework/ui.BaseCSS()` (a no-op),
  `framework/ui.ClusterConfig.Wrap` (ignored, `NoWrap` is authoritative), and
  `interactive.Confirm` (use `Action.WithConfirm`) are deleted.
- **BREAKING: pattern component names are bare.** `disclosure`, `multiselect`,
  and `sortablelist` register under their package names instead of
  `ui-disclosure` / `ui-multiselect` / `ui-sortable-list`; the emitted
  `data-fui-comp` value, the CSS scope, and the runtime selectors in
  `multiselect.js` follow. CSS or JS keyed on the old `ui-*` names must update.
- **BREAKING: `data-fui-sticky` and `data-fui-viewport` are no longer emitted.**
  Both were documented emit-only markers with no runtime or CSS consumer
  (styling keys off the `.ui-sticky--*` / `.ui-responsive__*` classes); the
  attribute-table rows are removed from the docs.

- **BREAKING: the flat `EntityConfig` fields are gone; the grouped
  configs are the model.** `SoftDelete`, `MultiTenant`, `TenantField`,
  `OwnerField`, `CrossOwnerRead` → `Scope`; `CursorField`,
  `CursorFields`, `MaxListLimit` → `Pagination`; `CRUD`, `MCP`,
  `Public`, `Access` → `Exposure`. The fields were deprecated through
  the v0.40 line and, worse, were still the canonical runtime
  representation: normalization copied the groups into them and the
  runtime read the flat copies. The inversion makes the groups the
  resolved model. Blueprint YAML flat keys (`owner_field:`,
  `soft_delete:`, …) still parse as the documented shorthand, and a
  flat key that conflicts with its grouped twin is now a loud error
  instead of a silent precedence pick. `gofastr upgrade` maps each
  removed field to its replacement.
- **BREAKING: `EntityConfig.Timestamps` is `*bool`.** A literal
  `false` used to silently mean "default true" because an unexported
  set-bit carried the real value; only `WithTimestamps(false)` worked.
  The visible value is now the semantic value, with nil defaulting to true,
  so the struct survives copying, serialization, and generic
  construction.
- **BREAKING: the agent harness moved to
  `framework/experimental/harness`.** It is a v0.1 subsystem and now
  carries the experimental-path stability exemption. Its architecture
  doc no longer describes unbuilt machinery: the TOFU
  acknowledgement flow, dir-trust, and diff-class detection were
  documented as shipped but never implemented, and are now labeled
  design intent; the package map is generated from the real tree. A
  layering test (with a red/green proof) replaces the fictional
  `depscheck` guard the doc used to claim.
- **BREAKING: upload metadata and `FileField` emit camelCase JSON.**
  `core/upload.Metadata` (`originalName`, `mimeType`, `uploadedAt`)
  and `framework/file.FileField` (`mimeType`, `storageRef`) join the
  camelCase wire convention. The persisted `<field>_variants` column
  format deliberately stays snake_case: it is a stored database
  format, and renaming its keys would orphan existing rows.
- **BREAKING: dead exported surface is deleted.** `battery/storage`'s
  `BackendFactory` registry (`Register`/`New`/`NewBattery`, where `New`
  always errored "no backend registered"), the unwirable
  `battery/experimental` redis stores, the harness's orphan providers
  (`failover`, `routing`, `copilot`), its inert plugin seam and stub
  packages, kiln's never-populated `render.Deferred` surface, and the
  never-returned `freeze.ErrGenerateViaBlueprint` sentinel.

### Fixed

- **`gofastr migrate` honors the pure-Go sqlite driver.** The dialect
  branch keyed only on `"sqlite3"`, so the zero-CGO `"sqlite"` driver
  silently ran Postgres-dialect SQL.
- **`gofastr dev --flag=value` parses.** The help documented the `=`
  form; the parser accepted only the space-separated form, silently
  ignoring `--addr=:9000`.
- **`gofastr init` emits the flat owned layout the scaffold-and-own
  contract documents**: `main.go` + `screens.go` at the root, no
  `screens/` subpackage, no `gen/` directory, Owned headers on every
  scaffold file.
- **Kiln no longer advertises actions it cannot run.** The agent tool
  schema and prompt offered `create_entity` and `respond_query`;
  nothing executed them. They are gone from the schema and the node
  catalog, and journal application now rejects any unsupported action
  kind at authoring time with an error naming it. Dropped entity
  endpoints, previously silently nulled, log a warning naming the
  endpoint and why.
- **`TestRequest.WithBody` cannot silently run the wrong body.** A
  JSON marshal failure is retained and surfaced by `Execute` as a
  failed response instead of dispatching the request unchanged.
- **The blueprint generator emits no empty group literals**, and
  `gofastr pack` normalizes an all-zero group to nil, so generated Go
  packs back to exactly the declaration the YAML parsed to.

### Documentation

- Every embedded doc page matches the code it describes again: the
  core-ui architecture package map lists the real packages, admin.md
  describes the real no-host failure mode, search.md the real battery
  lifecycle, widgets.md the real `PageTheme` signature,
  project-structure.md the real `init` output, and the doc index links
  the pages that existed but were unreachable. New pages: `email.md`,
  `storage.md`, and `core-packages.md` (a map of the exported `core/*`
  packages that had no page).

## [0.53.0] - 2026-07-30

The eleven pre-existing bugs the v0.52.0 review pass found in shipped code
but deliberately left out of that release, plus a security audit of the
packages no previous pass had ever read: kiln's expression and render
layers, the module wire protocol, the router, config, i18n, migrations,
file handling, and the SDK and pack tooling. Every fix landed with a test
that failed first.

### Security

- **BREAKING: a module's reverse calls now carry only the delegation
  handle.** `EntityQueryParams`, `EntityMutationParams`,
  `SearchQueryParams` and `EventEmitParams` take a `moduleproto.CallerRef`,
  `Delegation` and nothing else, in place of `moduleproto.Caller`.
  `Caller` travels in both directions: outbound the host fills it, inbound
  the child writes it, and the child is the untrusted party. A broker
  reading a child-supplied `Subject` or `Tenant` and dispatching under it
  is a confused deputy. The shipped `Broker` never did, since it resolves from
  `Delegation` alone, so nothing was exploitable, but a comment telling
  future authors not to trust those fields is weak protection while the
  fields sit on the decoded struct. Anyone implementing a process module
  gets a compile error and drops the two fields from the reverse call; the
  host already sent them outbound and does not need them back.
- **A trailing-slash redirect skipped the route gate and every
  middleware.** `core/router` handed net/http's synthesized redirect
  straight to the mux, so a path needing completion or dot-segment
  cleaning bypassed the gate the 404 and 405 branches both honour. Against
  a gate rejecting everything, `/admin/panel/` answered 404 while
  `/admin/panel` answered 307, the disabled-module existence leak the
  gate's contract explicitly promises not to produce, and `//admin/panel/`,
  `/admin/./panel/` and `/x/../admin/panel/` reached it too. Those
  responses also carried no security headers, recovery, timeout, request
  logging, CORS or rate limiting, and Go's 307 preserves method and body,
  so it answered POSTs. Redirects are now gated on the target route and run
  through the chain; a gated target falls through to an identical 404.
- **A DEFAULT clause could close its own literal.** `SQLDefault`'s
  fallback arm rendered `fmt.Sprintf("'%v'", v)` unescaped, and
  `Field.Default` is `any`, so a named string type, `[]byte`, a
  `fmt.Stringer` or a decoded map took that arm. A kiln `add_entity`
  payload over HTTP created and committed an extra column. Both arms share
  one escaper now.
- **kiln quoted SQL identifiers with a third, broken copy.** It escaped
  `"` and then wrote each rune as `byte(r)`, truncating after the escape
  test, so `U+2022` and every other rune ending in `0x22` became a real
  quote. Seed entity names and row keys are agent-authored, so it was
  reachable. The private copy is deleted in favour of `core/query`, which
  was always correct.
- **A kiln expression could kill the process.** The depth guard counted
  only grouping and prefix recursion, so a flat `1+1+1…` chain built a
  depth-O(n) tree and overflowed the stack during eval. That is fatal, and
  `recover()` cannot catch it. Comparing two uncomparable operands
  (`[1] == [2]`) panicked outright. Both are bounded now, and `Compile`
  caps source size.
- **The CSRF exemption keyed on the decoded path.** The mux matches the
  escaped path segment-wise, so `POST /api%2f..%2f..%2fadmin/wipe`
  presented `/api/../../admin/wipe` to the skipper, granting the `/api`
  exemption, while dispatch went to whatever wildcard route matched.
- **`required:"true"` was satisfied by an empty value.** A var that exists
  but is blank reports `("", true)`, so a blank `SECRET=`, a ConfigMap key
  with no value, or a secret-manager miss silently produced an empty
  signing key. Separately, a `sensitive` value survived verbatim in a
  `ConfigValidator`'s error, leaking a full DSN through `MustLoad`'s panic.
- **An attacker-chosen locale reached the catalog extension point.**
  Cookie-supplied tags were length- and charset-bounded; `X-Locale` was
  not, and won outright, and the nil-translator branch passed the entire
  raw `Accept-Language` header through. Both flow to `Catalog.Get`, whose
  documented wiring example is an `os.DirFS`, so a host with a lazily
  loading catalog received `../../etc/passwd` as a locale.
- **Uploads accepted magic-byte polyglots.** The content scan was skipped
  entirely for anything sniffing as a raster, PDF or font, so `GIF89a`
  followed by `<script>` passed wherever it sat. Tokens that never appear
  in binary media are now matched across the whole body; the ones that do
  appear in EXIF and XMP keep the windowed scan.
- **Typed After-hooks could not redact the response body.** `OnAfterCreate`
  and `OnAfterUpdate` unmarshalled into a fresh value and never merged
  back, while the untyped path mutates the live map that becomes the
  response. Identical redaction code worked untyped and silently no-opped
  typed. Both now merge back like their Before counterparts.
- Also: an uncapped goroutine spawn on the module notification branch
  (the request branch had the cap its own doc describes), a second
  `Peer.Start` crashing on a double channel close, duplicate inbound
  request ids clobbering each other's cancel, `PRAGMA table_info`
  interpolating an unvalidated identifier while every sibling call site
  validates, a dialect probe falling back to SQLite on any transient
  error and thereby skipping the cross-replica DDL lock, `Entity.Validate`
  missing case-folded wire-key collisions, unvalidated file metadata and
  LQIP placeholders, `pack` writing recovered secrets at mode 0644, a
  Redis flag store validating its rollout percentage on write but not on
  read, and a `docs --grep` panic on folding runes.

### Security (v0.52.0 review backlog)

- **A revoked 2FA backup code went live again after re-enrolment.**
  `EntityTwoFAStore.ConsumeBackupCode`'s CAS keyed on `(user_id, version)`,
  and `version` is not monotonic across a row's lifetime: `DeleteTwoFA`
  drops the row and `SetTwoFA`'s INSERT arm recreates it at version 0. A
  consume in flight during a disable-and-re-enrol passed its CAS against
  the new row and overwrote the freshly issued code sheet with the stale
  one: new codes silently dead, old sheet still working. The CAS now also
  predicates on the raw `backup_codes` bytes that round's SELECT returned.
  Bytes compared against themselves are formatting-proof, so the
  non-canonical-row case that ruled out a re-marshal comparison does not
  apply. `MemoryTwoFAStore` was never affected; the two stores agree again.
- **A revoked RBAC grant survived on every peer replica and came back on
  restart.** Each replica rebuilds a role as `baseline ∪ DB`, where
  `baseline` is the code-declared grants it captured at its own boot, so a
  revoke took effect locally and was merged back by every peer's next
  reconcile, and by any replica that booted later. `Revoke` now writes a
  revocation tombstone (`<grants-table>_revoked`, created by
  `EnsureSchema`, no migration needed); every reload and every `LoadInto`
  computes `(baseline ∪ DB) − tombstones`, so the revoke propagates and
  survives boots even for grants declared in code. Re-granting via
  `GrantStore.Grant` deletes the tombstone, the one way to un-revoke. A
  permission both granted and tombstoned resolves to revoked: fail closed.
  A new `transitionMu` serialises Grant/Revoke/reload/LoadInto so a reload
  that read a stale DB snapshot can no longer overwrite a newer local
  transition. `scaling.md`'s claim that `WithGrantStore + WithFanout`
  propagates revokes is now true.
- **`EagerLoad` applied no tenant or owner scope.** Its SECURITY block
  enumerated the scrubs it applies (soft-delete, Hidden columns) and read
  as exhaustive; a `MultiTenant` or `OwnerField` target loaded every
  tenant's and every owner's rows. It now ANDs in the same owner and
  tenant predicates the `?include=` path applies, from the same ctx
  sources, with the same fail-closed empty-ctx behavior and, like the
  include path, never consults `CrossOwnerRead` for related rows. Zero
  callers in the repo; this was an exported-API footgun for host apps.
- **The embed routes stripped only the cookie.** `framework/embed`'s
  middleware deletes `Cookie`, `Authorization`, and `X-API-Key`, since cached
  Basic credentials attach `Authorization` to same-origin subresource
  fetches, the exact channel the cookie defence exists for. But the
  uihost embed routes deleted `Cookie` alone. `stripAmbientCredentials`
  now strips all three, so the two surfaces agree on what "no ambient
  credential" means.
- **The embed content route published the whole static origin allowlist.**
  The shell resolves per-customer origins precisely so `frame-ancestors`
  does not enumerate every customer; the content response of the same
  handshake then used the static list. It now names exactly
  `grant.Origin`, the one origin the verified grant was minted for.

### Fixed

- **Sibling apps on one origin deleted each other's PWA caches.** The
  cache prefix joined slugs with `-` and `pwaSlug` maps `/`, space, `-`,
  `_` all to `-`, so app "Acme"'s activate prefix was a prefix of "Acme
  Docs"'s cache names, static exports at `/gofastr` and `/gofastr-docs` on
  one `user.github.io` wiped each other's offline mode, and `/a/b` vs
  `/ab` collapsed to the same prefix outright. Cache names now carry a
  fixed-length hash discriminator dot-separated from the slug
  (`gofastr-pwa-<slug>.<12hex>.<version>`; the static hash covers the raw
  basePath before slugging), so no app's delete-prefix can prefix a
  sibling's cache name. Activate also reaps the orphaned old-format own
  cache. The all-hex remainder test keeps sibling old caches safe.
- **One auth-gated screen aborted the whole static export**, leaving a
  partially written OutDir; marketing pages plus one `/dashboard` behind
  auth could not be exported at all. `RenderStaticPage` now returns a
  typed `PolicyBlockedError` for Redirect/Block decisions and the builder
  skips the route with a WARN naming path and decision, mirroring the
  llm.md loop, which already handled gated screens gracefully. RenderAlt
  screens export their alternate, as before. Skipped pages never enter
  `res.Pages`, so the static service worker's precache cannot 404 on them.
- **A push-only WebSocket was closed by its own keepalive.** `awaitingPong`
  cleared only inside `readFrame`, which ran only when the app called
  `Read()`. A server that only pushes never did, so a healthy connection
  whose peer answered every Ping was force-closed every
  `ReadIdleTimeout + PongTimeout` (70s at defaults). An internal read pump
  now owns all socket reads from Upgrade: control frames are processed
  even if the app never reads, `Read()` consumes complete messages from
  the pump, concurrent `Read` callers became safe, and `Close()`'s
  handshake completes as soon as the peer reciprocates instead of burning
  `CloseTimeout`. The keepalive skips its tick while a message handoff to
  a slow consumer is pending: a stalled app is not a dead peer.
- **Retention deleted dead-letters and silently disarmed `Replay`.**
  `completeParent` marks a parent dispatched with dead/abandoned
  deliveries outstanding (by design, they await Replay), and
  `purgeExpired` deleted by parent status alone, taking the dead-letter,
  its `last_error`, and the parent payload with it: an operator who fixed
  a consumer and called `ReplayConsumer` got nil and nothing replayed.
  Both purge DELETEs now exclude any parent still carrying a dead or
  abandoned delivery, which is what both functions' comments already
  promised. Retention is not a dead-letter TTL.
- **A stale success resurrected a terminal outbox delivery.**
  `markDeliveryDispatched` guarded on `status<>'dispatched'` while both
  failure updates had been fixed to `status='pending'`, so a worker with
  an expired lease could flip a dead or abandoned delivery to dispatched,
  clearing `last_error` and burying the dead-letter as a clean first-try
  success. Now symmetric: only pending settles; dead and abandoned are
  terminal until an explicit `Replay`.
- **WebSocket close-code echo obeys RFC 6455 §7.4.1.** The peer's first
  two Close bytes were stored and echoed verbatim, so 1005/1006, codes
  reserved as never-on-the-wire, could appear in our Close frame and turn
  a clean close into a 1002 from a strict peer; an illegal 1-byte Close
  body read as "no status". Received codes are sanitized at capture:
  reserved, sub-1000, and undefined-range codes echo as 1002, a bodyless
  Close still echoes bodyless.

## [0.52.0] - 2026-07-29

Security and correctness fixes across everything v0.42.0 through v0.51.0
shipped. Five entries are BREAKING; `gofastr upgrade` lists the ones that
touch your project, with the change to make in each.

Two of them stop an app at startup rather than at runtime: a registration
panic when two versions of one entity disagree about who reaches its rows,
and another when a pattern redirect covers an exact screen. Both used to be
silent.

### Security

- **A cross-site `enctype="text/plain"` form could complete a magic-link
  sign-in.** `rejectCrossSiteForm` only ran for `x-www-form-urlencoded` and
  `multipart/form-data` bodies. `text/plain` is the third enctype an HTML form
  can send and needs no CORS preflight, and `verifyHandler` reads its token
  from the query string, so an attacker could auto-submit their OWN magic-link
  token from their page and land the victim's browser in the attacker's
  account, which is precisely what the confirmation step exists to prevent. The
  guard now keys on whether a cross-site page could have sent the request at all
  (the three CORS-simple content types, plus an absent `Content-Type` for a
  bodyless `fetch`). JSON stays exempt, deliberately: it is preflighted, and a
  cross-origin SPA reaching these routes through configured CORS depends on it.
- **`EagerLoad` disabled every hidden-column scrub when a relation target had
  two API versions.** It called `registry.Get`, discarded the error, and treated
  a nil target as "no columns to hide", turning off the `Hidden` scrub and the
  soft-delete filter together. `framework/entity`'s own doc comment already said
  `Get` is unsafe for relation targets and to use `ResolveTarget`; it now does,
  against the source entity's version, and fails closed when the target cannot
  be resolved.
- **BREAKING: untrusted IR could name the attributes the runtime dispatches actions
  from.** `core-ui/noderender` treated `data-action*` and `data-param-*` as inert
  host markers while `frag/boot.js` was resolving the nearest `[data-component]`
  and calling `__gofastr.trigger()` with them: at hydration for
  `data-action-mount`, and again on every `gofastr:navigate`, with the IR
  choosing the arguments. `RenderNode`/`RenderKind` now strip them, and
  `RenderTrustedNode`/`RenderTrustedKind` are the first-party entry points the
  blueprint generator uses. `data-island` is refused outright: it is the SSE
  swap target, which is hydration identity.
- **BREAKING: versions of one entity may no longer disagree about who reaches
  its rows.** Two versions share one physical table, so a tenant-scoped v1
  beside an unscoped v2 meant v2 read what v1 hid. The registry now rejects a
  mismatch in `MultiTenant`, `OwnerField`, or `SoftDelete`, the predicates
  CRUD adds to every statement, and in `Public`, `Access`, or
  `CrossOwnerRead`, the gates it runs before issuing one. A v2 declaring
  `Public: true` beside a session-required v1 made the whole table anonymously
  readable; a v2 with a blank `Access` block skipped the permission v1
  required. Per-version *column* projection still varies freely: visibility of
  a field is presentation, visibility of a row is authorization.
- **`VerifyAuthEntitiesPrivate` went quiet on exactly the apps that need it.**
  It called `reg.Get(name)`, which reports an ambiguity error once a name is
  mounted under several versions with none unversioned, which is what
  `App.GroupEntity` produces, and that error was indistinguishable from "not
  found". A versioned app exposing its users table through auto-CRUD got the
  "entity not registered" hint instead of the warning. It now audits every
  version registered under the name, and names the mount in the warning.
- **BREAKING: the embed CSRF path exemption is safe methods only.** It covered the whole
  `/__gofastr/embed/` prefix; the framework mounts exactly two `GET` routes
  there, so the breadth bought nothing and pre-approved any future POST.
- **An embed grant is refused on `/__gofastr/sse` instead of falling through.**
  A presented grant used to fall through to the ambient session cookie, so a
  framed page could stream the viewer's island updates while authenticated as
  someone else.

### Added

- **`gofastr build --allow-unverified-embeds`** keeps a provable embed
  violation fatal while downgrading the one note class that otherwise stops the
  build. For a surface that genuinely cannot be analysed statically and has
  been checked by hand, this is the narrow escape hatch; `--no-embed-check`
  remains the blunt one.

### Fixed

- **`TypedQuery.Find`/`First` ignored `crud.WithReadHooks`**, returning stored
  values from the one API whose entire purpose is to match what the HTTP surface
  shows. `Find` now runs `AfterList` and `First` runs `AfterGet`, mirroring the
  routes.
- **Event records no longer alias containers reached through a pointer.** The
  reflective deep copy skipped every pointer; a hook injecting `*map[string]any`
  still shared storage with the record handed to the event goroutine, the
  concurrent-map-write race the copy was written to prevent.
- **Both embed server-action gates follow the rendered component tree.** An
  embeddable root whose CHILD registered `G.serverAction` passed the build-time
  analyzer and the boot walk, and failed only in the customer's page. The boot
  walk no longer stops at `core-ui/island`, a one-field wrapper around the
  component it renders, and the shape the blueprint emits for every island
  block, so stopping there hid the child the wrapper exists to render. It also
  now walks map keys as well as values. The analyzer reports what it cannot
  follow statically instead of passing silently, and `gofastr build` prints
  those notes. One class of note fails the build: a child built inside
  `Render()` whose type lives in another package, which the boot walk cannot
  see (it does not exist as a value) and the analyzer cannot either (its
  `Actions()` body is in another syntax tree). The rest stay advisory, because
  the boot walk covers them. `cmd/check-embed` exits non-zero on the blocking
  class too, rather than printing a green tick for a tree `gofastr build`
  refuses.
- **A versioned relation target no longer degrades the admin screens or
  contradicts the startup warning.** `App.warnUnresolvableRelations` and both
  relation lookups in `battery/admin` called `Registry.Get`, which errors on a
  name mounted under several versions, so the framework warned that
  `?include=` would be refused on a relation the request path resolves fine,
  and the admin's relation labels silently fell back to raw UUIDs and its
  pickers to bare text inputs. All three now use `entity.ResolveTarget`
  against the source entity's version, the same call `framework/crud` makes.
- **The SDK schema hash no longer reports drift that regenerating cannot
  clear.** The route-group prefix was part of the hash identity, and
  `App.GroupEntity` stamps that prefix as the entity's `Version`, so every
  app using a route group saw a permanent drift banner. Where a thing is
  mounted is routing, not schema. Versions that project to the same schema now
  collapse, compared on their canonical JSON rather than `fmt.Sprint`, which
  renders `[]string{"draft ready"}` and `[]string{"draft","ready"}`
  identically and so could silently merge two versions with genuinely
  different enum sets. `SchemaVersion` is 2: a manifest from an older
  `gofastr` encodes the prefix, and is reported as unknown provenance
  (regenerate it) rather than as a drifted schema.
- **A malformed durable-scheduler payload is refused at enqueue** rather than
  stored and failed at every dispatch. An empty payload still means JSON
  `null`, which is what the column held before.
- **BREAKING: a pattern redirect that covers an exact screen now panics at registration**
  in both orders, like every other collision. It used to register cleanly and
  308 the screen away forever, with no diagnostic.
- **The disclosure focus trap releases `inert` on detach.** Opening a mobile
  drawer and navigating across layouts detached the `<details>` without firing
  `toggle`, leaving every other `<body>` child out of the focus order and the
  accessibility tree for the life of the tab.
- **A wildcard CORS response strips `Access-Control-Allow-Credentials` even when
  the handler writes nothing**, an ordinary shape for an empty `DELETE`, and
  while a panicking handler unwinds to `Recovery`. The strip is now keyed on
  the `Access-Control-Allow-Origin` value actually on the response rather than
  the one in config, because a downstream handler can replace it: a handler
  that narrowed ACAO to a real origin used to lose the credentials header it
  was entitled to set.
- **An inline `data:` URI survives into a srcset.** The comma separating media
  type from payload was percent-escaped along with candidate separators, so a
  URI the allow-list admits was destroyed by the emitter. Only that first comma
  is preserved; every later one belongs to the payload and is still escaped,
  because a trailing raw comma ends the candidate before its width descriptor.
- **Only a pending outbox delivery may record a handler failure.** Both
  `markDeliveryFailure` updates guarded on `status<>'dispatched'`, so a worker
  whose lease had expired could overwrite a dead or abandoned delivery with a
  fresh `last_error` and push it back to pending, resurrecting a delivery an
  operator had already triaged. They now guard on `status='pending'`; every
  other state is terminal until an explicit `Replay`.
- **A local RBAC revoke is no longer undone by the next reconcile.**
  `GrantStore` rebuilds each role as `baseline ∪ DB`, where `baseline` is the
  code-declared grants captured at boot, so revoking a permission that was
  seeded in code took effect in memory and then came back on the next
  peer-driven reload. `Revoke` now narrows the baseline as well. (Peers that
  captured their own baseline at boot still merge it back; that half is
  tracked separately.)
- **`PgVectorStore.Add` threads its context** instead of discarding it and
  calling `Exec`, so a cancelled request no longer keeps writing chunks.

### Changed

- **`SSEBrokerConfig.Principal`** decides who may evict a subscriber id. The
  principal used to be `RemoteAddr`'s host, which behind any reverse proxy is
  the proxy for every request, so all callers collapsed into one principal and
  `?subscriber_id=<victim>` dropped a stranger's stream. With no `Principal` the
  broker now evicts nothing, which costs nothing: subscriber ids address no one
  (delivery is a broadcast) and a dropped connection already unregisters.
  `MaxSubscribers` stays exact: past the cap `Subscribe` rejects. A half-open
  connection keeps its seat until the next heartbeat write fails and that
  stream unregisters itself, which reclaims seats for every client rather than
  only the ones that send a `subscriber_id` (none of the framework's own do).
- **BREAKING: `ConfirmPageData.CSRFField` is required for a custom `ConfirmPage`.** The
  default magic-link confirmation page rendered no `_csrf` field, so with
  `auth.CSRF()` mounted, which `WithBFFPosture` does app-wide, its own button
  returned 403 and passwordless sign-in was unreachable. `ConfirmPageData` had
  no request and no context, so a custom page could not supply one either.
- **The runtime gzip budget is 12.5 KB, measured at gzip level 6.** It was 12 KB
  measured at level 9, which nothing ships at: GoFastr installs no compression
  middleware, so the wire bytes come from a proxy at its own setting. The same
  artifact measures 12287 at level 9 and 12317 at level 6: the code did not
  grow, the ruler was wrong.
- **The fragment symbol gate compares against a checked-in manifest**
  (`core-ui/runtime/frag/SYMBOLS.txt`) instead of `git show HEAD:runtime.js`.
  Since `runtime.js` is generated from the fragments, the old gate compared the
  artifact to itself and could not fail.
- **The doc gate resolves every relative link and anchor** across
  `framework/docs/content/`, which is how a batch of links pointing one
  directory short went unnoticed.

### Documentation

- The gallery catalog is 141 entries, not 139 (`catalog.go`, `gallery.go`,
  `agents.md`, and the v0.49.0 note all said 139).
- `framework/gallery/agents.md` documented `gallery.CodeSnippets["button"]`,
  which does not exist and never compiled. The accessor is
  `CodeSnippet(slug)`, and the map is private on purpose.
- `component-build`'s skill told maintainers to mirror registry fields into
  `core-ui/runtime/runtime.js`, a generated file, so the edit would not ship.
- The `Field.NoQuery` entry claimed "every query surface"; it is every *wire*
  query surface. The in-process Go API is deliberately ungated, as the entry
  beside it already said.
- The static exporter's CSP comment claimed the in-document `<meta>` covers
  hosts that ignore `_headers`. Per CSP L3 §3.1 a meta-delivered policy ignores
  `frame-ancestors`, so the fetch directives carry but the clickjacking guard
  needs the header. `security.md` now says which hosts that leaves uncovered
  and what to set there.
- The `auth.md` `ConfirmPage` example concatenated `d.Email` and `d.Token` into
  a body string. A magic-link request accepts any address, so `d.Email` is
  whatever the requester typed; the example now uses the escaping builders and
  renders `d.CSRFField`, which a custom page must include or its own submit
  403s under `auth.CSRF()`.
- `ui-getting-started.md` described redirect/screen overlap as a dynamic-screen
  rule. A *pattern* redirect overlaps an *exact* screen the same way, in either
  registration order.
- `api-versioning.md` promised "four structural invariants" and listed them by
  count, which went stale as soon as one was added. It now reads as a rule, and
  covers row reachability and the gate in front of it.
- `events.md` and `live-dashboards.md` document `SSEBrokerConfig.Principal`:
  what it must return, and why leaving it nil costs nothing.
- `framework/gallery/agents.md` pointed agents at `NoteOnlySlugs`, which is not
  exported. The accessor is `IsNoteOnly(slug)`.
- The `hooks-and-transactions.md` table of hook-skipping read paths omitted
  `TypedQuery.Find`/`First`, which are now in it.
- `embed.md` said both gates panic. The analyzer reports a diagnostic and
  `gofastr build` exits non-zero; only the boot walk panics.
- `core-ui/ARCHITECTURE.md` documented one `noderender` entry point. There are
  four, split by trust, and picking the untrusted one for first-party IR
  silently drops every action attribute.

## [0.51.0] - 2026-07-28

The SPA screen cache gained eviction, and its e2e test caught a
framework-wide bug: twenty demand modules re-bound their components on a
navigation event that never reached them.

### Added

- **Screen-cache invalidation (`X-Gofastr-Invalidate` +
  `ui.InvalidateScreens`).** The runtime keeps a 20-entry per-tab LRU of
  rendered screens for instant back/forward; until now nothing could evict
  an entry, so after a mutation the back button showed pre-mutation state.
  A handler names the screens it staled, `ui.InvalidateScreens(w,
  "/orders")`, and the runtime drops them when the response resolves 2xx.
  - Selectors: `"/orders"` evicts that pathname plus every cached query
    variant (`/orders?page=2`, …); `"/orders?page=2"` evicts exactly that
    entry; `"*"` clears the cache. No prefix matching: `"/orders"` never
    touches `/orders/42`. The header value is a JSON string array
    (commas are legal in query values) and accumulates across calls like
    `AddToast`. Paths with control characters are rejected: DEL survives
    JSON encoding un-escaped, and Go's HTTP/2 writer silently discards a
    header with an invalid value.
  - Consumed on every 2xx mutation or navigation dispatch: RPC, widget
    RPC, nav partials, full-shell fetches, intercepted nav,
    toggle/optimistic actions, sortable reorders. Never on poll replies,
    and before `X-Gofastr-Location`, so a mutated-and-redirected response
    evicts first and the redirect target is fetched fresh.
  - Eviction never re-renders the visible page; pair with
    `data-fui-rpc-navigate` (or the new `__gofastr.refresh()`, which
    re-fetches the current screen without touching history, `#fragment`
    included) when the user should also see fresh state now. Scope is the
    requesting tab: the cache is browser memory, other tabs and the
    server are unaffected, and cross-tab freshness stays on the polling
    rung.
  - The JS mirror is `__gofastr.invalidate(...selectors)` with the same
    selector rules.

### Fixed

- **SPA re-bind hooks in twenty demand modules never fired.** `nav.js`
  dispatches `gofastr:navigate` on `window`; dropdown, carousel,
  lightbox, taginput, toggleaction and fifteen more listened on
  `document`, which a window-dispatched event never reaches. Components
  bound at initial load worked; after a SPA navigation swapped in fresh
  DOM they were inert until a hard reload. All twenty now listen on
  `window`. The one test covering a navigate-dismissal simulated the
  event on `document`, matching the broken listener instead of the
  runtime, and now dispatches on `window`.
- The initial page was cached under `pathname` without its query string,
  while every other cache key (and `currentPath`) is `pathname+search`, so
  landing on `/orders?page=2` cached page-2 HTML under `/orders`.
- A redirect-followed cross-layout fetch dropped the redirect target's
  query string from both the URL bar and the cache key
  (`/login?next=/admin` became `/login`).
- The widget RPC's inline `X-Gofastr-Toast` parse now delegates to the
  kernel dispatcher, so a toast survives a failed `toasts` module load
  via the fallback renderer (previously it was silently dropped).

## [0.50.0] - 2026-07-28

Closes [#150](https://github.com/DonaldMurillo/gofastr/issues/150): every gap
v0.49.0 shipped embeddable surfaces with.

Onboarding a customer is now a row in your table rather than a config change and
a deploy: a domain typed into your settings page is framed *and* granted without
a restart. Multi-tenant entities compose with embeds. A surface that would rely
on `G.serverAction` fails at `gofastr build`, and at boot if the build gate could
not see it, never in a customer's page. And a surface carries the screen it
renders instead of a path string, which is what let the last two be checked at
all.

### Changed

- **BREAKING: `embed.Surface` carries a screen, not a path string.** `Surface.Path`
  (a `string`) is replaced by `Surface.Screen` (a screen value). A surface
  renders a screen, and the screen, not a string the framework resolves, is
  the link to the component tree, so a human, a static analyzer, and the
  boot-time server-action walk can all follow it. Pass the same `*app.Screen`
  you register:
  ```go
  reports := app.NewScreen("/reports", &Reports{})
  application.RegisterScreen(reports, app.EmbedLayout())
  embed.Surface{Name: "reports", Screen: reports, Origins: ...}
  ```
  `*app.Screen` satisfies the new `embed.Screen` interface (`RoutePath()`)
  structurally; `framework/embed` still does not import `core-ui/app`. The route
  is still validated exactly as `Path` was (absolute, normalized, not `/`, not
  covering a reserved prefix); a nil screen is a boot error. Read the resolved
  route via `ResolvedSurface.Path()` where you read `Surface.Path` before.
  `framework/uihost` reads it for you (`embedSurfacePath`, the shell config,
  the content render).
- **BREAKING: `MintNonce`, `VerifyGrant` and `Refresh` take a `context.Context`
  first.** Every caller must thread a context through: the grant path consults
  the `OriginSource` (and any store) on each call, and a context is how a
  deadline and a trace ride along. Add `ctx` (or `r.Context()`) as the first
  argument: `MintNonce(ctx, surface, subject, origin, scopes)`,
  `VerifyGrant(ctx, token)`, `Refresh(ctx, token)`. `gofastr upgrade` flags
  every call site.

### Added

- **`embed.Config.ResolveTenant`**: multi-tenant entities behind an embed.
  `Middleware()` clears the tenant along with every other ambient identity
  value, because inheriting the *cookie* user's tenant is a cross-tenant read.
  It then had no way to install the right one, so a multi-tenant entity behind a
  surface simply errored. Give it a lookup and it works:
  ```go
  ResolveTenant: func(ctx context.Context, subject string) (string, error) {
      u, err := users.FindByID(ctx, subject)
      return u.TenantID, err
  },
  ```
  The tenant comes from that lookup **on the grant's subject**, never from
  anything the request carried: a stolen grant cannot pick its own tenant. A
  resolver error fails the request closed. Nil behaves exactly as before.
- **`embed.OriginSource`**: runtime origin management, the thing that makes a
  white-label embed a product rather than a deploy pipeline. An app implements
  it against its own table; the shell serves only the requesting customer's
  origins in `frame-ancestors`, and the grant path (`MintNonce`, `Exchange`,
  `VerifyGrant`) consults it for origins the static list does not know.
  - The static allowlist is checked first: a map lookup, and all an app
    without a source ever pays.
  - `Allows` is on the hot path (`VerifyGrant` calls it per request for
    non-static origins). Cache it.
  - Removing a customer takes effect on the **next request**, not when their
    grant expires, the same property de-listing a static origin has.
  - A source error, an unknown customer, or an over-size list all fail closed to
    `frame-ancestors 'none'`. An outage must not become an open framing policy.
  - Two consequences of per-customer responses, both improvements: your customer
    list is no longer enumerable from one URL, and one customer's list growing no
    longer breaks the surface for everyone.
  - The snippet carries the customer id as `data-customer`; the loader forwards
    it onto the frame URL (bounded and encoded), and the shell reads it as the
    `customer` query param to resolve that customer's origins. Without that
    forwarding an `OriginSource` app serves `frame-ancestors 'none'` for every
    customer, so the row you added is never reached. Framing and granting a new
    domain with no restart works only because the loader carries the id.
- **`check-embed`**, a build gate wired into `gofastr build` beside `check-csp`
  and the accessibility gate. It resolves `embed.Surface{…}` → `app.NewScreen`
  → the component type → its `On(...)` registrations, and fails naming the
  surface, component and action. It reports only what it can prove: a screen
  built in a loop, or a component in another package, it stays silent about,
  because the boot walk already covers those and a false positive in a build
  gate is worse than a miss.
- **Boot-time refusal of a server action on an embeddable surface.** `G.serverAction`
  is refused inside a frame (the action registry is app-global with no
  relationship to any surface), and that refusal stays, but the developer now
  learns about it at boot. `uihost` walks each declared surface's screen on
  `Mount` and panics if the component registers an action whose client handler
  calls `G.serverAction`, naming the surface, the component and the action and
  pointing at island RPC, a form POST, or polling. Detection mirrors the action
  compiler exactly: it flags the `G.serverAction(` token in a registered
  action's `ClientJS`. A separate `check-embed` analyzer will catch the
  statically-resolvable cases at build time; this walk is the backstop that
  sees what actually registered.

## [0.49.0] - 2026-07-27

Two features that turned out to share one piece of plumbing. Embeddable
surfaces let an app hand out a screen to a website it does not control.
`gofastr theme edit` previews a theme against the whole component gallery
while you edit it.

The shared piece is per-request theme resolution: `AppCSS()` read a boot-time
`App.Theme`, and both the embed frame and the theme preview need a different
palette per request without mutating process-global state.

### Added

- **Embeddable surfaces (`framework/embed` + `uihost.WithEmbed`).** An app
  declares a screen embeddable and names the exact origins allowed to frame
  it; its customer pastes one `<script>` tag and gets a live, themed,
  authenticated piece of the app. Delivery is an iframe, so inside the frame
  GoFastr is same-origin with itself and the runtime's origin guards,
  same-origin fetches and ownership of the document all hold unchanged.
  - The credential is a **single-use handshake nonce** exchanged for a
    short-lived grant. Single use exists to make a *shared* token impossible:
    the predictable customer failure with a TTL token is hardcoding one into a
    page template so every visitor arrives as the same identity. Minting is
    stateless; only the exchange touches a store, claiming the nonce id against
    a unique constraint so "already used" is decided by the database rather
    than by a read-then-write two replicas could both win.
  - The exchange is POST-only and idempotent within the grant's lifetime. A
    prefetched iframe, a double-mounted loader and a page refresh all fire it
    twice; without idempotency the feature surfaces as "the embed randomly
    doesn't load".
  - `embeds.RequireScope("reports:read")` gates the routes an embed may reach.
    `Middleware()` installs the grant's subject with that subject's full
    authority, including any admin role it holds, and a grant lives in a third
    party's page where devtools can read it, so the surface's declared scopes
    have to be enforceable somewhere. The framework has no route-to-scope map
    and does not invent one; the app names the pairing. Ordinary first-party
    traffic passes through untouched.
  - `Middleware()` must be installed **outermost**, before any authentication
    middleware. It discards `Cookie`, `Authorization` and `X-API-Key` so an
    authenticator running inside it finds nothing to authenticate; installed the
    other way round it cannot undo context values another package already wrote,
    and a bearer token overwrites the grant's identity.
  - Grants refresh while the frame lives, bounded by an absolute deadline the
    grant has carried since it was issued (`Config.GrantMaxAge`, 12h). A frame
    left open in a tab does not hold a credential forever.
  - Origins are exact and compared **normalized**: `https://acme.com`,
    `https://acme.com/`, `https://acme.com:443` and `https://ACME.com` are one
    origin and four strings. There is no wildcard spelling, and a host that
    looks like one (`*.acme.com`) is rejected at config time rather than
    compared literally.
  - `frame-ancestors` lists every allowed origin, because no `Origin` header
    is sent on a navigation GET: at the moment the header is written the
    server does not know who is framing it. The browser enforces against the
    real ancestor chain.
  - Embed routes **discard cookies** before any handler reads one. Inside a
    cross-site frame none is sent, but an app at `app.acme.com` framed by
    `www.acme.com` is same-site and a Strict cookie does ride along; honouring
    it would hand a signed-in user's session to a third party's frame.
  - `BurnStore` is pluggable with a SQL implementation for multi-replica
    deployments. Embeds require an app secret. Unlike sessions, there is no
    per-boot fallback, because a nonce that fails to verify is gone and it was
    rendered into a page the app cannot re-render.
  - **`embeds.Middleware()`** authenticates the island RPCs the surface fires
    after first paint. Those target ordinary app routes, which know nothing
    about embeds; without it a surface paints as its viewer and then acts as
    nobody, or, in a same-site framing, as whoever the cookie says. It also
    exempts grant-carrying requests from CSRF, which they have to be: no cookie
    is ever sent from inside a frame, so a double-submit check has nothing to
    compare and would 403 every embed interaction.
  - The frame attaches its grant to **every same-origin fetch**, not to one
    header builder. Polling, toggles, optimistic actions, infinite scroll and
    sortable lists each assemble their own headers, so hooking the RPC path
    alone left all of them fetching anonymously.
  - Customer brand tokens go through `style.ApplyTokens` behind a per-surface
    `AllowTokens` allowlist and a cap on distinct registered variants.
  - `examples/embed-demo` runs both halves on two ports, which is the only way
    to exercise the parts that exist because of the origin boundary.
- **`gofastr theme edit`**: a local theme configurator. Controls are generated
  from `style.ThemeToTokens`, so a token added later gets a usable control
  without touching the tool. The preview is the real component gallery served
  by a real `UIHost`, and an edit swaps `app.css?t=<key>` with no page reload.
  Contrast is checked in the browser via `getComputedStyle`, which resolves
  `oklch()`. The repo's only prior contrast code was hex-only and returned 0
  for the colours real themes actually use. Write-back regenerates `theme.go`
  whole, with an injection test modelled on the blueprint emitter's.
- **API versioning that works.** The entity registry is keyed on
  `(name, version)`, so `app.GroupEntity(v1, "posts", …)` beside
  `app.GroupEntity(v2, "posts", …)` finally does what
  `framework/docs/content/api-versioning.md` has described. It used to panic.
- **`schema.Field.WireName`**: a JSON key alias that leaves the DB column
  untouched, so a v2 can rename a field on the wire without a migration.
  Honoured on every read path: filters, sorts, includes and nested filters all
  accept the wire name and rewrite it to the column.
- **Runtime compositions.** `core-ui/runtime/runtime.js` is now assembled in Go
  from `frag/*.js` inside one IIFE. Three compositions ship: `full`, `static`
  (SSG export, 19% smaller, retires the `_staticMode` request-time branch),
  and `embed`. **Edit the fragments, never `runtime.js`.**
- **Per-request theming**: `UIHost.AppCSSFor(theme)` and
  `RegisterThemeVariant`, served at `/__gofastr/app.css?t=<key>`. A request
  names a pre-registered key; it never describes a theme, which is what makes
  CSS injection unrepresentable and bounds cache cardinality.
- **`framework/gallery`**: the 141-entry component catalog, extracted from
  `examples/site` so a CLI binary can render it. It now also ships the layout
  classes its own demos emit (`ContributeCSS`), which the docs site had been
  defining on its behalf.
- **`app.EmbedLayout()`**: a chrome-less layout: the `<main>` landmark and
  nothing else.
- **`ui.Workbench`**: a viewport-height inspector shell: a fixed-width rail
  that scrolls on its own beside a pane that fills the rest, with an `<iframe>`
  in the pane filled edge to edge. It stacks below 720px.
- **`ui.ColorField`**: a colour swatch beside a text input holding the same
  value, as one control. The text input is the source of truth, so `transparent`,
  `inherit` and `var(--x)` survive; a native colour input silently falls back to
  black for all three. Use `ui.ColorPicker` when a swatch plus a label is
  enough.

  Both were added because `gofastr theme edit` needed them and had grown ~25
  bespoke classes and ~21 hardcoded hex values standing in. The catalog being
  incomplete is what bespoke CSS in a generator usually means.

### Changed

- **BREAKING: `battery/embed` is now `battery/semantic`.** The semantic-search
  battery moved so the new feature could take the name that describes it. The
  import path, the package qualifier, the `gofastr embed` subcommand
  (now `gofastr semantic`), the `/embed/*` routes (now `/semantic/*`) and the
  `~/.gofastr/embed` snapshot directory all move together, as does
  `kiln/agent.NewEmbedContextHook`, now `agent.NewSemanticContextHook`.
  `gofastr upgrade` carries the entry.
### Documented late: API versioning shipped in 0.48.0

These landed in the 0.48.0 tag with no release note, including a breaking
change. Recording them here rather than leaving them undocumented.

- **BREAKING (0.48.0): `Registry.Get` errors instead of guessing** when one
  entity name has several versions. Use `GetVersioned(name, version)`.
  `Registry.All` still returns one representative per name, so anything
  iterating it sees only the lex-first version.
- Versions of one entity share a table and migrate the **union** of their
  columns; a column only v2 declares is an additive change. A *conflicting*
  definition across versions panics at wire time naming both call sites.
  `Hidden` and `WireName` are wire concerns and are never conflicts.
- OpenAPI, `llm.md` and the generated MCP tools identify a versioned entity
  rather than documenting both versions at the same path and silently
  overwriting.

### Security

Seven review rounds ran against this release. These are the findings from the
last one, all in the new embed surface.

- **An encoded path could outrun the reach allow-list.** `MayReach` decided on
  the percent-decoded path and cleaned it; `net/http`'s `ServeMux` matches on
  the escaped one, where `%2e%2e` and `%2F` are ordinary bytes inside a single
  segment. `GET /api/docs/%2e%2e/%2e%2e/reports` therefore cleaned into the
  surface's own subtree, passed the gate, and dispatched to `/api/docs/`, a
  prefix `reservedPrefixes` exists to keep grants away from. `RoutedPath` now
  decodes segment by segment and refuses any path whose segments do not survive
  decoding intact, so the gate and the router read the same string. Cleaning
  cannot fix this class: normalising one of the two strings is what opens it.
- **`Surface.Path` was not validated against reserved prefixes.** `Reach:
  []string{"/auth"}` failed at boot with a clear error while `Path: "/auth"`
  booted clean and handed every grant the whole auth battery, and `Path` is
  the field an author reaches for. Both now get the same check. This also fixes
  a trailing slash silently disabling a surface's own subtree.
- **A relocated battery lost its protection.** The reserved list could only
  name defaults, so `admin.Config.PathPrefix = "/back-office"` kept the guard on
  `/admin` and lost it where it mattered. `framework.EmbedReserving` lets a
  battery report the prefix it actually mounted; admin, print and api-tokens do,
  and surfaces are re-validated once everything is mounted.
- **`auth.HasScope` gave an embed grant full user capability.** The embed
  middleware deletes `Authorization` and `X-API-Key`, so `TokenMiddleware` never
  ran and no token scopes were set, which every scope gate read as "session,
  unscoped". A grant declaring `orders:read` passed `RequireScope("orders:write")`
  and deleted through `RequireAPIScopes`. `TokenScopes` now reports the grant's
  own scopes, which are already in the same `resource:verb` grammar.
- **The frame could navigate itself, or the customer's page.** The runtime's
  navigation paths were neutralised; the browser's were not. An embeddable
  screen containing `<a target="_top">` replaced the hosting page when clicked.
  The iframe now carries a `sandbox` without any top-navigation token, and
  native links and forms are cancelled in the capture phase with a visible
  notice and an `onError` event. `target="_blank"` links open a new tab.
- **`Middleware()` cleared the user but not the tenant.** Installed inside a
  tenant resolver, the documented-wrong order that nothing enforced, an embed
  ran as the grant's subject with the cookie user's tenant. It now refuses such
  a request outright with a message naming the fix.
- **A grant-authenticated response carried no cacheability signal.** No
  `Set-Cookie`, no `Authorization`, so two different subjects looked
  byte-identical to a CDN. Now `Vary: X-Gofastr-Embed` and `private, no-store`.
- **A held stream outlived its grant.** Verification happened once, at entry, so
  an SSE stream kept emitting after fresh requests with the same grant had
  started answering 401. The request context is now bounded by the grant expiry.
- **Idempotency read only the first `Cookie` header field.** Behind a proxy that
  prepends its own, two authenticated users shared one namespace and the second
  received the first's stored response.
- **The server-action grant refusal was bypassable in one extra request**, since
  `/__gofastr/session` mints an anonymous session to anyone. That route now
  refuses a grant-bearing request. Server actions remain reachable by any
  same-origin anonymous caller and are not an authorization boundary; the
  comment claiming otherwise has been corrected.
- **The origin allowlist was unbounded.** All of it ships in one
  `frame-ancestors` directive on every shell response, and a few hundred
  customers exceeds common proxy header limits, which breaks the surface for
  every customer at once. Refused at boot past 4 KiB, with the arithmetic in the
  message.
- Smaller: `crud.Redispatch` dropped the grant header, so reach and expiry were
  not re-evaluated on the re-dispatched path; a `SubjectResolver` returning a
  typed-nil pointer installed it as a logged-in user.

### Fixed

- **The component *bundle* CSS ignored the requested theme.** The
  single-component handler was converted to per-request theming and the bulk one
  was not, so a component whose `StyleFn` reads `style.Theme` directly kept
  painting the base palette under a requested variant. The theme editor could
  write a theme its operator had never actually seen.
- **A stalled embed request left the panel loading forever.** The parent's
  watchdog was cancelled on `ready`, before the exchange and content load, and
  neither fetch had a deadline. All three phases now share a 15s abort.
- A repeated nonce exchange now logs a warning. One replay is normal; a run of
  them is the only visible symptom of a nonce baked into a cached customer page,
  which hands one identity to every visitor of that copy.
- **Hidden-column oracle in nested filters.** `include.go` and
  `nested_filter.go` resolved a relation target with `Registry.Get`, which
  preferred the unversioned entity, so a v1 request adopted an unversioned
  declaration's `Hidden` set and scopes. Worse, the nested-filter check was
  wrapped in `err == nil`, so version ambiguity disabled it entirely and
  `?author.password_hash_like=` became a substring oracle over other users'
  password hashes. Both now resolve version-aware and fail closed.
- **Authorization bypass in `RequireAPIScopes`.** A path made entirely of
  version-like segments left the resource name empty and fell through
  unchecked: `GET /api/v1` returned 200 with unrelated scopes. A version
  segment is now only skipped when a real segment follows.
- **`isValidColor` accepted `rgb(0 0 0) URL(...)`.** Prefix-plus-balance is not
  a grammar; a value must now close its function call at the end of the value,
  and nested functions come from an allow-list: a deny-list misses
  `image-set()`, `cross-fade()` and `element()`, all reachable through a
  `var()` fallback.
- **`CrudHandlerForEntity` never set `BasePath`** and accepted an unregistered
  entity.
- **`gofastr theme edit`'s contrast checker reported "no issues" for every
  theme.** Three independent defects, each fatal on its own: the probes carried
  their colours in an inline `style` attribute, which the framework's own CSP
  discards; the colour regex was written with a doubled backslash inside a Go
  raw string, so JavaScript saw a literal backslash and it never matched
  anything; and the check ran before the stylesheet it measures had applied. It
  also read `color-mix()` output, which Chromium serializes as
  `color(srgb 0.95 …)`, as 8-bit RGB, turning a light tint into black and
  inventing four failures. Colours are now converted by the browser itself, a
  pair it cannot convert is reported rather than skipped, and a check that
  throws says so instead of rendering an empty (clean-looking) panel.
- **`gofastr theme edit --addr=:8090`, the form the docs gave as an example,
  refused every request.** The wildcard bind reports its address as `[::]:8090`,
  which no browser sends, so the Host pin rejected everything including the URL
  the tool printed. A bare `:port` now resolves to loopback, and an explicit
  non-loopback bind is refused: the page carries its own bearer token and the
  write-back endpoint rewrites a Go file, and a Host pin does not stop a direct
  TCP client.
- **Widgets inside an embedded surface silently did nothing.** The catalog
  endpoint gated on the session cookie, which a frame never sends, so a modal
  or drawer trigger was dead DOM cross-site, while a same-site framing answered
  it from the viewer's unrelated app session. It accepts an embed grant now, and
  discovery names the surface's own route so `.Pages()` scoping applies.
- **Everything else gated on the session cookie was still shut to a frame.**
  Fixing the widget catalog fixed one endpoint out of four. `actions.js` and the
  server-action endpoint gated the same way, and nothing emitted `actions.js`
  into the frame at all, so `__gofastr.register` was never called, `handlers`
  stayed empty for the life of the frame, and the failure was silent in both
  directions: every `data-action-mount` node rendered and never filled (that is
  every generated entity list and every relation `<select>`), while every
  `data-action` click was `preventDefault()`ed by the delegator and then
  dropped, so the control looked alive and did nothing. The frame's runtime now
  carries the compiled actions, and the predicate behind
  `Definition.RequireSession` accepts a grant so widget chrome loads one hop
  further along.
- **The widget catalog answered any grant with any page.** `embedGrantOK` only
  checked that the token verified, so a grant minted for a public surface could
  ask for `?page=/admin`, or omit the parameter and read the unfiltered
  registry. The grant's own surface path is substituted for whatever the caller
  asked for.
- **`GrantFromContext` was empty on the first render.** Only the island RPCs
  that came *after* first paint went through `Middleware()`, so a screen writing
  `if !embedded { firstPartyControls() }` rendered those controls inside the
  customer's iframe, and a scope-gated section appeared on first paint and
  vanished on the first swap.
- **Rejected theme parameters were retained forever.** `reserve` recorded the
  attacker-supplied parameter in two maps and `release` cleaned one, so every
  malformed `?theme=` on the unauthenticated shell route left a permanent entry
  that eviction never reached and the cap never counted. The size bound also sat
  *after* the value became a map key, so the retained key was bounded only by
  the request line. Both fixed; an oversize parameter can no longer take a slot,
  which means it can no longer evict a customer's real branding.
- **Two visitors arriving at once got different palettes.** `reserve` reported
  "someone is already resolving this exact theme" and "there is no room" as the
  same refusal, so on a cold process one of two concurrent requests for one
  customer's theme rendered in the app palette.
- **Component stylesheets ignored the per-request theme.** The catalog and both
  component-CSS handlers read the boot-time app theme, so an embed came back
  half-rebranded: `var()`-driven components turned the customer's colour and
  anything whose `StyleFn` read theme values directly stayed the app's.
- **An expired grant made the frame anonymous instead of failing.** Clearing the
  grant stopped the header being sent, and a request with no header is an
  ordinary anonymous request the server passes straight through, so a dashboard
  polling every 30s quietly swapped its authenticated numbers for the logged-out
  render while still reporting `data-fui-embed-state="ready"`. The dead token is
  kept so the server answers 401, and the new `expired` state says so.
- **`history.pushState` from inside a frame wrote to the customer's back
  button.** Removing the nav fragment removed navigation but not push-state, so
  widget deep links, pane deep links and `X-Gofastr-Push-State` responses each
  appended to the *top-level* page's session history: two entries per modal
  open and close. The frame's `pushState`/`replaceState` are inert.
- **A grant could survive a cross-origin redirect.** Same-origin was decided
  from the URL given to `fetch`, and a redirect changes the origin after that
  check; browsers strip only `Authorization` across one. Credentialed embed
  requests now refuse to follow redirects.
- **Reloading a frame bricked it permanently.** The loader handed the nonce over
  exactly once, but a frame re-runs its document on "Reload frame" and on some
  bfcache restores, leaving it waiting forever with an empty root and nothing
  in either console. The loader answers every `ready` (the exchange is idempotent
  by design), and the frame gives up with an error after 15s rather than
  spinning.
- **The documented snippet had no error surface.** `onError` was reachable only
  through the programmatic API, so a spent nonce, from a cached customer page or a Back
  button, produced a blank 150px box with the explanation trapped in a
  cross-origin console. The auto-mount path reports failures, and names the
  missing attribute when `data-surface` or `data-token` is empty.
- `VerifyNonce`/`VerifyGrant` accepted an empty HMAC key where the minting side
  had always refused one, so a caller reaching a verifier before its key was set
  authenticated forgeries rather than failing.
- `GrantMaxAge == GrantTTL` passed the boot guard and produced exactly the
  failure that guard names: every grant born at its deadline, every refresh
  clamped back to it, no forward progress.
- **SECURITY: `gofastr theme edit` published the harness signing key.** Its
  bearer token was `deriveListenerSecret()`, which returns the same bytes that
  HMAC every harness control-plane token when `GOFASTR_HARNESS_MACHINE_KEY` is
  set, and it was emitted into a `<meta>` tag on a page served with no authentication,
  because that page is where the token comes from. Any local process, any other
  user on the machine, or any screenshot in a bug report carried it away, and
  the machine key is stable across restarts. The token is now per-session
  random, which is all this listener ever needed.
- **SECURITY: the framework MCP gate admitted an embed grant.** The previous
  release note claimed this was closed; it patched `battery/auth`'s
  `MCPUser`/`MCPRole`, which have no production call sites. The gate actually
  wired to every entity endpoint's MCP twin and to the module enable/disable
  control tools is `framework`'s own, and it was untouched. Both now refuse.
- **`battery/admin`'s embed refusal sat below the custom-`Authorize` early
  return**, so an app supplying its own authorize hook, what the surrounding
  comment recommends for a different role model, got no refusal at all.
- **`RequireAuth` admitted subject-less grants and `RequireRole` admitted any
  grant.** A public surface's grant has no subject, so passing it through
  handed handlers a request with no user while implying one was verified; and a
  grant scoped to one surface satisfied `RequireRole("admin")` whenever its
  subject held the role.
- **`gofastr theme edit` wrote theme files that panicked the app at boot.**
  `style.ApplyTokens` accepts a zero spacing, radius or z-index; `Theme.Validate`,
  which `app.WithTheme` runs at startup, rejects them. One keystroke in a
  number field produced a green "updated", a written `theme.go`, and a panic on
  the next run. Edits are now validated against the boundary the artifact has
  to survive.
- **The theme editor's Write button worked exactly once per session.** The
  no-`--force` guard re-ran on every write and refused the second one, citing a
  file the tool itself had written seconds earlier, with no recovery but a
  restart that discarded every edit. A file this session wrote is now its own to
  rewrite.
- **The contrast checker's readiness gate could not fail.** It tested that the
  sentinel's colours were *parseable* rather than that they were the literal
  black-on-white it declares, and `getComputedStyle` always yields a parseable
  colour, so a slow stylesheet swap measured an unstyled document, scored ~21:1
  on every pair, and reported a clean theme it had never measured.
- **SECURITY: idempotency replayed one embed subject's response to another.**
  The idempotency middleware is installed by `NewApp`, so it runs outside
  `embeds.Middleware()` and before any grant is verified: every embed request
  looked anonymous and shared one key namespace. Two grant holders sending the
  same `Idempotency-Key`, `order-1` is enough, meant the second received the
  first's stored response and its own handler never ran. The namespace is now
  bound to the grant.
- **JWT-protected routes 401'd every embed request.** `embeds.Middleware()`
  deletes `Authorization` so nothing competes with the grant it just verified;
  `RequireAuth` then demanded the header it had just removed. A request already
  carrying a verified grant now passes, while an uncredentialed one is still
  refused.
- **The contrast checker invented failures it should never have reported.** It
  hardcoded `#ffffff` as the foreground on status fills, but the design system
  paints `var(--color-primary-fg)` there: `.ui-button--danger` sets exactly
  that, and `styles_components.go` says so. In the default dark scheme that
  token is `#111827` and the status tones are light, so the tool reported four
  failures (white on `#F87171` at 2.77:1) for text nothing renders. Measured
  against what is actually painted, the shipped theme passes every pair, and a
  browser test now pins that as an invariant. A checker that cries wolf is worth
  no more than one that never fires.
- **Two concurrent requests for the same customer theme could drop the
  variant.** The second resolved its own copy and released it immediately,
  reasoning that the key is a content address so both land on the same one.
  That is true, except when the duplicate arrived first: its register took the refcount
  0→1 and its release took it 1→0, deleting the variant before the owner had
  registered anything, and the frame rendered unthemed under a `?t=` pointing at
  nothing. The duplicate now waits for the owner instead, bounded so a failed
  owner cannot hang it.
- **BREAKING: an embed grant now reaches only its own surface.** A grant is
  valid for its surface's `Path` subtree, the runtime's `/__gofastr/*`
  endpoints, and whatever the surface lists in the new `Surface.Reach`.
  Everything else answers 403.

  The previous default, a grant reaching every route the app author has not
  explicitly gated, is not a property anyone can hold, because the framework
  and its batteries mount `/mcp`, `{auth}/tokens` and `/admin/*` themselves.
  Each of those had to be patched individually as it was discovered, and the
  patch set was not converging. `Reach` is validated at boot: `"/"` is refused,
  as is any prefix covering a framework-mounted route, and a refused request
  answers with the surface, the path, and the `Reach` entry that would allow it.

  A surface whose islands post outside its own subtree needs one line:
  `Reach: []string{"/api/orders"}`.
- **A stalled nonce claim can no longer mint a second grant.** Verification
  happens before the burn and is not atomic with it, so a request that verified
  a nonce and then stalled past its retention deadline could land after `Prune`
  removed the winning row and insert a fresh one. The row is still written, because it
  is the tombstone and withholding it would leave the nonce spendable, but no
  usable grant comes back. `PruneGrace` keeps rows a further five minutes so a
  fast pruner cannot delete one a slow verifier still needs.
- **SECURITY: an embed grant could mint a permanent `*:*` API token.**
  `POST {auth}/tokens` refuses API-token callers precisely so a scoped token
  cannot escape its own leash; an embed grant is a third credential with the
  same property and the gate did not know it existed. A credential deliberately
  bounded to one surface, one origin and a 12-hour deadline, and living in a
  third party's JavaScript, could exchange itself for an unscoped, never-
  expiring one. The same classification gap opened `battery/admin` (past whose
  gate a wildcard access policy is installed, so any admin who viewed an
  embedded surface left an admin credential on someone else's website) and
  every `mcp.Gated` tool. All three now refuse a grant.
- **The documented middleware order erased the grant's identity.**
  `embeds.Middleware()` installs the subject and deletes the cookie; the
  `SessionMiddleware` inside it then found no cookie, took its anonymous branch
  and cleared the user, so the wiring the docs prescribe produced a handler
  that saw nobody, losing owner-scoped CRUD, policies, tenancy and audit.
- **A refused grant still fell back to a session cookie on two widget routes.**
  The shared gate was fixed for this; the catalog and the widget-session
  predicate were not, so an expired embed, which the runtime keeps sending on
  purpose, was answered as the viewer's unrelated logged-in user, skipping the
  per-surface scoping.
- **A combobox selection destroyed the embed.** Modules fall back to a hard
  `location.href` when `__gofastr.navigate` is absent, which inside a frame is
  not a race but the only path; the frame navigated to an ordinary app route
  that refuses to be framed, and nothing was left alive to report it. The frame
  now installs a no-op navigator, so every guarded call site takes the harmless
  branch.
- **The response cache replayed one embed subject's page to another.**
  `battery/cache` classified a request as credentialed by looking for `Cookie`
  or `Authorization`. An embed grant carries neither by construction, so a
  grant-authenticated GET was stored under the shared key and returned to the
  next grant holder as a `HIT` without the handler running.
  `X-Gofastr-Embed` now counts as a credential.
- **A grant opened any widget's own endpoints, not just its surface's.**
  Scoping the catalog decided what a caller was told about; `/state` and
  `/chrome` are predictable URLs, and the gate behind them reduced the grant to
  a boolean. Anyone with devtools on a legitimate customer page could read the
  state of a widget scoped to `/admin`. The gate now resolves the widget and
  checks it belongs to the grant's own surface.
- **Component stylesheets in a frame were requested with a malformed URL.** The
  catalog began emitting `?t=<variant>` and the runtime appended its version
  with a hard-coded `?`, so the server parsed one unusable parameter: the theme
  key was never found (the per-request theme fix was inert on the only path that
  uses it) and the version was empty, which also silently dropped `immutable`
  caching from every component sheet an embed loads.
- **A second visitor arriving mid-resolve still got the app palette.** An
  in-flight reservation is stored as an empty key and `lookup` reported it as a
  hit, so the caller took the empty key and never reached the duplicate branch
  added for exactly that case. The window is the whole of a stylesheet
  compose-and-hash, so it was the common path rather than a narrow race.
- **A refused grant fell back to an ambient session.** The shared gate consulted
  the session cookie after rejecting a presented grant, so in a same-site
  framing an expired embed request was answered on the strength of the viewer's
  unrelated logged-in session. A presented credential's verdict is now final.
- **An embed grant could invoke any server action in the app.** The action
  registry is keyed app-globally with no relationship to a surface, so a grant
  minted for one surface reached every registered action, including from a
  surface with no subject. `/__gofastr/action` requires a session again;
  wiring it for embeds needs the app to say which surfaces may invoke what.
- A refused refresh treated every non-OK status as fatal, so one 502/503 during
  a rolling deploy permanently expired every open frame on every customer page.
  Only 401/403 end a grant now; anything else backs off and retries.
- A form submit whose handler answered a redirect failed silently inside a
  frame: the rejection landed in a bare `catch {}`. It now logs and raises a
  toast, and the limitation is documented.
- The loader had no watchdog, so the most common integration mistake, the
  customer's origin missing from the surface's allowlist, showed as an empty
  box with nothing in either console, because the blocked frame never runs the
  script that would have reported it.
- The frame's fetch wrapper now sets `credentials: 'omit'`, making "no cookie
  reaches an embed request" true at the source rather than assumed.
- `TestEmbedExchangeIsIdempotent` compared whole response bodies including a
  wall-clock `expires_in_ms`, so it failed roughly once in a thousand runs with
  the message "one nonce bought two identities", for two byte-identical grants.
## [0.48.0] - 2026-07-27

`AfterGet` and `AfterList` are documented as the way to mask a field on
the way out, "redact fields, drop rows", and they worked on `GET /x`
and `GET /x/{id}`. Six other paths did not. The filter surface never saw
the mask, so the stored value was recoverable a character at a time from
which rows came back:

```
GET /cards?number_like=4111  → 1 row    every response still
GET /cards?number_like=4112  → 0 rows   reads "****1111"
```

`framework/filter` already names that attack and blocks it for `Hidden`
columns. Hook-based redaction had none of that protection. Chasing it
turned up five more paths that returned the stored value outright, not as
an inference: keyset pages, `?include=` children, `_events` deliveries,
create/update response bodies, and the in-process reads a generated app
renders its own screens through. All are closed.

### Added

- **`schema.Field.NoQuery`**: a column that stays in API responses but
  is refused by every wire query surface: flat filters, `?sort=`, `?where=`
  predicate trees, nested `?rel.field=` filters, scoped `?include=`
  filters, and the DSL. The in-process Go API (`ListAll`, `CountAll`,
  `TypedQuery.Where`/`Order`) is not gated: a caller-built filter on a
  NoQuery column still runs there, so server-side read-modify-write and
  aggregates keep working (stored values stay the default for the Go API,
  as the next entry spells out). `Hidden` already closed this by removing
  the column from responses; `NoQuery` is for values the caller must still
  see in a reduced form. Declarations use `no_query: true`. The rejection
  names the field, unlike `Hidden`'s: a `NoQuery` column is visible in
  the response, so its existence is not a secret worth protecting and a
  precise error saves a developer hunting for a typo.
- **`crud.WithReadHooks(ctx)`** (also `framework.WithReadHooks`): makes
  an in-process read apply `AfterList`/`AfterGet`. Generated blueprint
  list, detail, and related-list screens use it, so an app's own pages
  show what its API shows. Stored values stay the default for the Go API,
  which is what keeps read-modify-write, seed lookups and aggregates
  correct. The context handed to a hook has the flag stripped, so a hook
  that reads its own entity cannot re-enter itself.
- **`crud.CrudHandler.ChildHooks`**: resolves another entity's hook
  registry by name so `?include=` rows run the read hooks of the entity
  they belong to. `framework.App` wires it on every mounted handler.
- **`webhook.WithBridgeRedactor`**: transforms an event before the
  webhook bridge POSTs it to subscriber URLs. The bridge is an outbound
  delivery to third parties and has no handler to run read hooks through,
  so masked fields would otherwise leave the server there in full.

### Fixed

- **`?cursor=` skipped `AfterList` entirely**, writing stored values
  straight to the wire while the offset path returned masked ones. The
  hook now runs over the page, after the continue-cursor is derived from
  the stored keyset values, so a hook that masks the keyset column
  cannot corrupt paging.
- **`?include=` children skipped the child entity's hooks.** Rows come
  from a join/`IN` loader rather than the child's handler, so a redaction
  applied to `GET /children` but not to the same row one hop away. The
  child's `AfterList` now runs over the attached rows, after key
  conversion so the hook sees the keys its own endpoint returns, plus its
  `AfterGet` for a to-one relation: that serialises as a single object,
  so the surface it mirrors is `GET /child/{id}`, and an app masking only
  there was still serving the stored value through `?include=`.
- **`_events` published stored values.** The record is captured from the
  write's `RETURNING`, before any read hook. Deliveries are redacted per
  subscriber, which keeps the hook out of the write transaction. Delete
  stubs pass through intact; a hook error omits the record rather than
  publishing it raw.
- **Create and update responses echoed stored values.** `RETURNING`
  yields every visible column, so a partial `PUT` returned fields the
  caller never sent, unmasked, to anyone with update permission.
  `AfterGet` now runs over the response body.
- **The admin edit form wrote masks back over stored values.** It
  prefilled through the HTTP `Get` handler, which runs `AfterGet`, then
  posted every field back on submit, so editing one field persisted the
  mask over every other. Pre-existing. It now reads the row twice and
  treats any column the hook rewrites as write-only: rendered empty with
  a hint, kept as stored unless an admin types a new value. Reading raw
  and prefilling it, the first fix here, swapped the bug for a
  disclosure aimed at the one reader who can see every row.
- **A nested `?rel.field=` filter skipped every schema check when the
  relation's target was not a registered entity.** The `Hidden`/
  `NoQuery`/declared-column checks all sat inside `if registry.Get(...)
  == nil`, and `isSafeIdentifier` only constrains the SHAPE of a name, so
  the column went into an `EXISTS` predicate unvalidated. A relation may
  legitimately point at a table no entity registers, such as the auth battery's
  self-migrated `auth_users`, which made `?author.password_hash_like=`
  a working oracle over a column that is in no response. `?include=`
  already refused that shape; nested filters now match it, in-process
  callers included.
- **A write response's redaction reached the event record.** The hook
  runs on a copy because the raw record has already gone to the async
  event goroutine, but the copy was one level deep, so a hook masking a
  field inside an embedded object wrote through the shared nested map
  into the bus, the fanout tap and the webhook bridge, and raced
  whichever subscriber was reading it, which is a runtime throw
  `recover()` cannot catch. The copy is now reflective rather than a
  list of named types: the containers that matter are the ones an
  application's hook injects, and a hand-written type switch covers only
  the shapes its author thought of: `[]byte`, `[]string`,
  `map[string]string` and `[][]map[string]any` were all still shared.
- **The read-hook opt-in survived into `payload.Request.Context()`.**
  `hookCtx` strips it from the context argument, but on the in-process
  path the request handed to a hook is synthesised from the caller's
  context, so a hook reading through `p.Request.Context()` re-entered
  itself until the stack was gone.
- **A child `AfterList` that reordered rows corrupted the `?include=`
  payload.** The fold paired the hook's output with the loader's rows by
  index, so a permutation attributed each record to the wrong parent
  and, because it mutates rows later iterations still read as sources,
  duplicated one and destroyed another: `[A,B,C]` came back `[C,B,C]`.
  The fold now matches rows by PRIMARY KEY, so sorting `Results`
  (ordinary list-hook behaviour, and correct on the child's own route) is
  a no-op rather than either a corruption or a 500, since the order a client
  sees comes from the attachment, which the fold never touches, and a
  hook that projects *and* reorders folds correctly too. A many-to-many
  child shared by two parents arrives as two distinct maps carrying one
  id, so the index is multi-valued and each replacement claims a distinct
  slot. Refused: a changed row count, a projection that dropped its id,
  an id the query never returned, and one row returned more often than
  the query produced it.
- **A failed response hook turned a committed write into a 500.** The row
  was in the table and the event had shipped, so the caller retried and
  wrote it twice; in a `_batch` it lost the ids of every row that
  committed. The response degrades to the new row's id instead. `_batch`
  also skips the redaction pass entirely when the transaction rolled
  back, where it was replacing a per-item error report with an opaque
  500.
- **`?cursor=&sort=<NoQuery>` answered 200** where `?sort=<NoQuery>`
  answers 400. Keyset mode ignores `?sort=` because the cursor fields control
  `ORDER BY`, but it returned before the sort was parsed, so appending
  one empty parameter skipped the refusal. The sort is still ignored in
  keyset mode; it is now validated first, so any `?sort=` the offset path
  rejects (an unknown column as well as a masked one) is rejected here
  too.
- **The admin edit form mangled `date` and `timestamp` values.** Reading
  through the in-process API returns what the driver scanned, a
  `time.Time` where the old JSON round trip produced a string, and the
  cell formatter had no case for it, so the input rendered Go's default
  layout, failed RFC 3339 validation on submit, and took the user's
  other edits down with it. `date` columns render `yyyy-mm-dd`, which is
  the only value `<input type="date">` accepts. RFC 3339 there blanks
  the control and the save wipes the column.
- **A masked `bool`, `enum`, or foreign key was overwritten on save.**
  The write-only contract held only for text inputs: a checkbox cannot
  express "unchanged", and `formToJSON` emitted a bool either way, so an
  edit to an unrelated field cleared `is_admin` to false. A `<select>`
  with nothing preselected posted its first option, silently reassigning
  an enum or a relation. Masked columns of every control type now offer
  an explicit "— unchanged —" choice, drop `Required`, and the save path
  recomputes which columns are masked server-side rather than trusting
  the form.
- **A blueprint screen could name a framework-managed column the entity
  does not have.** `created_at`/`updated_at` exist only under
  `timestamps:`, `deleted_at` under `soft_delete:`, `tenant_id` under
  `multi_tenant:`. The validator treated the NAME as proof of
  existence, so a `search:` or `group_by` on one passed generate and
  failed as an anonymous SQL error at runtime.
- **The DSL selected `Hidden` columns.** `BuildDSLQuery` projected
  `Schema.Names()`, which includes them, and accepted them in `where`
  and `order`. It has no callers outside its own tests, so nothing was
  exposed, but the projection is `VisibleFields()` now and both clauses
  are guarded.
- `gofastr pack` dropped field flags it did not name explicitly, so a
  pack/regenerate round trip silently removed `no_query`.
- **The generated Go SDK README documented a request that returns 400.**
  Its filter example took the entity's first column without checking
  `NoQuery`, so the snippet whose job is to demonstrate the snake_case
  query contract could name a column the server refuses. The JS README
  already skipped them; the Go one now does too.

### Changed

- **BREAKING: `entity.Define` panics on a `Hidden` or `NoQuery` cursor
  column.** Keyset paging orders and compares on that column and encodes
  its value into the cursor token, which is reversible base64. The check
  covers a declared `CursorField`/`CursorFields`, the primary-key
  default, and the primary key auto-appended as a composite tiebreak.
- **BREAKING: `entity.Define` panics on a `NoQuery` `SearchFields`
  entry**, and the blueprint decoder rejects the same. `?q=` matches on
  the stored value.
- **BREAKING: blueprint `entity_list` `search:` and `filters:`, a
  `stat_card` `source.filter` or summed `source.field`, and a chart's
  `group_by` reject `hidden` and `no_query` columns.** Each reaches
  `ListAll`/`CountAll` with a hand-built `ParsedFilter` or a raw read,
  bypassing the filter parser, so a masked column there is a value
  oracle on the app's own page. The chart is the sharpest: `group_by`
  renders every distinct stored value as a bar or slice LABEL, so the
  same page printed the mask in its table and the full value in its
  chart. `group_by` was not validated as a column at all, so a typo
  silently grouped every row into one bucket. `search:` was previously
  unvalidated; `id`, the `timestamps:`/`soft_delete:` stamps, and
  relation-derived foreign keys all stay valid: none is in
  `decl.Fields`, and rejecting them broke blueprints that generated
  before.
- **BREAKING: blueprint `cursor_field`/`cursor_fields` reject `hidden`
  and `no_query` columns at decode time.** `entity.Define` already
  panicked on them, but a generated app that dies at boot is a much
  worse diagnostic than the error `search_fields` gets.
- Generated CLI filter flags, the JS SDK `<entity>Fields` constant,
  typed column constants, OpenAPI filter parameters, MCP list-tool
  arguments, `llm.md`, and the SDK docs examples all omit `NoQuery`
  columns. Each previously advertised a filter the server answers with
  400. The columns stay in output structs, input and patch structs, and
  mutation flags: a field can be writable without being queryable.
- `SchemaHash` covers `NoQuery`, so flipping it moves the hash and the
  SDK drift banner fires.
- The admin renders a `NoQuery` column unsortable and never picks one as
  the quick-search column; either returned a 400 that blanked the grid.
  Relation labels use a separate picker, so a masked column can still
  label a foreign-key cell.

### Notes

Register a mask on **both** `AfterGet` and `AfterList`. Each response
path runs the hook matching the shape it serves, exactly as the entity's
own routes do, so a mask that exists on only one of them has a gap on the
paths that use the other. A to-one `?include=` consequently runs the
child's `AfterGet` once per distinct attached row. Rows are deduplicated
by identity and bounded by the page size, but a hook that queries the
database is doing that per row, where `AfterList` did it once. A hook
registered on both surfaces runs twice on those rows, so keep masks
idempotent: assigning a constant or deleting a key both are; appending or
truncating are not.

On `?include=`, a child hook may redact in place or by replacing a row
with a projection; both are folded back into the attached row. It may not
change the row count: the loader has already keyed each row to its
parent, so a dropping hook fails the request rather than serving the
rows it tried to remove. Filter in the child's `BeforeList` instead.
Sorting `Results` is harmless and has no effect on the payload; keep the
id when projecting, since that is what identifies the row.

Three surfaces stay raw on purpose. `?stream=true` refuses when an
`AfterList` hook is registered rather than bypassing it. The durable
outbox row stores the unredacted record, since it is server-side state
used for replay. And `webhook.Bridge` POSTs to third-party URLs with no
handler to redact through. `webhook.WithBridgeRedactor` is available for
apps that need it.

`Hidden` is unaffected throughout: it is enforced in the SQL projection,
so no path can return it.

## [0.47.0] - 2026-07-26

Image placeholders now actually paint. The pipeline could produce a
BlurHash since 0.5; nothing could turn one back into pixels, and the
`data-placeholder` / `data-blurhash` attributes the UI components emitted
were read by no CSS rule and no JavaScript anywhere in the tree. The
feature was inert end to end, and the tests only asserted the attributes
were present.

### Added

- **`framework.WithImagePipeline`: uploads now derive their own renditions
  and BlurHash.** Previously the generate half of the story was entirely
  BYO: nothing in the framework or any battery ever called `VariantSet`, so
  "when does a BlurHash get made?" had one answer: whenever you hand-wrote
  the call in an upload handler. Now declaring a `schema.Image` field is
  enough. Every upload on that field produces the configured renditions
  plus a BlurHash (and optionally an LQIP), stores the renditions beside
  the original, and writes the metadata to sibling columns the entity
  declares: `<field>_blurhash`, `<field>_placeholder`, `<field>_variants`.
  Undeclared columns are skipped, so adopting one means adding the column
  and nothing else.
- **`framework.WithImagePipelineFor(entity, field, deriver)`**: per-field
  override of the app-wide pipeline, since an avatar (portrait components,
  reject animated) and a hero cover (wide renditions) cannot share one
  config. An explicit `nil` opts a single field out without unpicking the
  default.
- **`framework/imagefield`**: the adapter implementing
  `file.ImageDeriver` over `image.VariantSet`. A separate package on
  purpose: `framework/crud` is in the dependency graph of nearly every
  app, so a direct edge to `framework/image` would link every image
  decoder plus the WebP encoder into every binary. The dependency is
  inverted through an interface and only apps calling
  `WithImagePipeline` pay for the codecs, enforced by a `go list -deps`
  test, not a comment.
- **`file.ImageDeriver` / `file.ImageDerivatives` / `file.DerivedVariant`**
  plus `file.WithImageDeriver` on `ProcessFileField`, for callers driving
  uploads directly. `FileField` gains an `Image` field carrying the
  derived metadata, and `FileField.Validate` now checks it: derived
  references reach the same sinks as the primary file.
- **`image.DecodeBlurHash` + `image.BlurHashDataURL`**: the missing half
  of the BlurHash story. Store the ~28-char hash in a column at upload
  time; call `BlurHashDataURL` at render time to turn it back into an
  inline image. `DecodeBlurHash` returns a pipeline `*Image`, so it chains
  with the existing encoders. Hashes are treated as untrusted input:
  length, alphabet, and component count are validated before any pixel
  buffer is allocated, and output dimensions are capped at
  `MaxBlurHashRenderSize` (128 px).
- **Placeholder memoisation**: `SetBlurHashCacheSize` /
  `FlushBlurHashCache` / `BlurHashCacheLen`. A list view would otherwise
  re-decode the same handful of hashes on every request.
- **`urlsafe.ImageSource` policy**: `Resource` plus inline raster `data:`
  URIs, for the one sink where those are a feature rather than a mistake:
  `<img src>`. `data:image/svg+xml` is excluded (SVG is a markup
  surface), as is every non-image media type. `Resource` itself is
  unchanged, so `<script src>` and `<link href>` still reject `data:`.

### Changed

- **`OptimizedImage` / `PipelineImage` render the placeholder as a real
  stacked `<img>`** behind the image, positioned by static CSS. No
  JavaScript, no `attr()`, no per-instance CSS: an inline `style`
  attribute is blocked by both the default CSP and the repo's
  `noinlinestyles` linter, so an element is the only mechanism available.
- **`Placeholder` takes an inline raster `data:` URI. BREAKING.** A bare
  BlurHash string is no longer accepted. It is not an image until it is
  decoded. Bad values (a raw hash, a remote URL, a non-raster `data:` URI)
  are dropped and the image renders without a placeholder rather than
  panicking, matching how `PipelineImage` already treats missing intrinsic
  dimensions: a placeholder is data, not a caller-code bug.
- **`data-placeholder` and `data-blurhash` are no longer emitted.
  BREAKING** for anyone who wired their own CSS or hydration to them.

### Fixed

- **A `data:` image URI was silently replaced with the 1×1 blank stub.**
  The components pre-filtered `Src`/`Fallback`/`Sources` with
  `urlsafe.Resource`, which rejects `data:`, so a generated or inlined
  image rendered as "broken" rather than "blocked". Every `<img src>` sink
  now uses `safeImageURL`: `OptimizedImage`, `PipelineImage`, and
  `Gallery`'s thumbnails. `Avatar` has no pre-filter of its own and is
  fixed by the `html.Image` policy change; a test pins that. Caught by
  looking at a screenshot, not by any test.
- **`PipelineImage` applied no URL allow-list at all**: `OptimizedImage`
  ran `safeResourceURL` on its `Src` and every `Sources` URL; its sibling
  ran it on neither `Fallback` nor `Sources`, despite rendering storage
  URLs that routinely originate in user data.
- **`OptimizedImage` did not escape srcset separators.** A URL containing
  a comma or whitespace (presigned links, keys with comma-separated
  segments) split one candidate into several malformed ones.
  `PipelineImage` already escaped these; both now share one
  implementation.
- **The `Fit: contain` placeholder no longer bleeds into the letterbox
  bars.** The placeholder is never removed, so a cover-cropped blur behind
  a contained image was a permanent visual change, not a loading state.
  It now mirrors the image's `object-fit`.

### Internal

- A `go list -deps` gate enforces that `framework/ui` never imports
  `framework/image`, previously only a comment. The edge would put every
  image decoder plus the WebP encoder in the binary of any host that
  renders any UI at all.
- A `go list -deps` gate likewise keeps `framework/crud` and
  `framework/file` clear of `framework/image`, so the upload path's
  dependency inversion cannot be "simplified" away.
- The `/components/pipelineimage` showcase is a rendered demo instead of a
  paragraph pointing at a demo that did not exist, and a new chromedp test
  against it asserts the browser actually decoded the placeholder, that it
  covers the real image's box, that the real image paints in front, and that
  with the real image hidden the remaining pixels are a colourful blur
  rather than the flat resting grey. The predecessor tests asserted a
  `data-placeholder` attribute was present, which stayed green for as long
  as the feature was entirely inert.
- The demo's source image is a drawn mockup rather than a gradient: a
  gradient's blur is indistinguishable from the gradient, so the first
  version of the demo demonstrated nothing.

## [0.46.0] - 2026-07-26

The v0.43.0 audit backlog (#135) closed, three filed issues fixed (#138,
#139, #141), and the first audit of surfaces the previous passes had never
opened (#136, partially; its two concrete bugs are fixed and four of its
listed surfaces audited, but most of its never-opened list is still never
opened, and one pass does not clear a surface by this repo's gate).

Same shape as 0.45.0: **guard drift**, a check one path has and its
sibling skips. Three of the findings below are the *same* property
(a schema is a disclosure even when the data behind it is refused) showing
up on three different surfaces.

### Security

- **`tools/list` no longer hands the schema to callers who cannot call
  the tool.** `mcp.Gated` wraps a *handler*, so it only ever reached
  `tools/call`. An unauthenticated POST came back with every tool's
  `inputSchema`, and for entity CRUD tools those are built from live
  entity definitions: every entity name, every non-`Hidden` field, its
  type and full enum set. New `mcp.WithToolGate` registration option runs
  the gate on both `tools/call` and `tools/list`; the MCP control tools
  and `log_set_level` use it. **BREAKING** for anyone reading a gated
  tool's schema anonymously.
- **`framework.WithMCPGate` + `framework.MCPRequireUser`** close the whole
  `/mcp` data surface (`tools/list`, `tools/call`, `resources/list`,
  `resources/read`) in one call. `initialize` and `ping` stay open by
  design: they carry only the protocol version, capability booleans and
  the server name.
- **`Endpoint.MCPHandler` twins require an authenticated caller by
  default. BREAKING.** An `Endpoint` has two front doors for one
  operation: `Handler` inherits the route's middleware chain, `MCPHandler`
  does not, so an endpoint behind `auth.RequireRole("editor")` was
  role-checked over HTTP and ungated over MCP. New `Endpoint.MCPGate` for
  something stricter, `Endpoint.MCPPublic: true` to opt out.
- **`log_set_level` is gated.** It mutates the running app's
  observability: an anonymous caller could flip it to DEBUG, or to ERROR
  to go quiet before doing something else. Dev-implied registration stays
  ungated (the dev loop has no auth to satisfy; its exposure is bounded by
  the loopback bind instead), but an app that set `AllowMCPMutation`
  itself keeps the gate even under `gofastr dev`.
- **`/{table}/llm.md` runs the entity's full scope chain.** It checked for
  a session but not for the entity's declared permission, so an
  authenticated caller with no `orders:read` grant got 403 on the rows and
  200 on the schema. **BREAKING** for a caller that has a session but not
  the read permission.
- **The plugin iframe sandbox sanitizer is an allow-list on both sides.**
  v0.45.0 flipped the Go half; `host/pluginhost.js`, the sink that
  actually sets the attribute, was still a one-token deny-list, so
  `allow-popups-to-escape-sandbox`, `allow-top-navigation` and
  `allow-downloads` all passed it. **BREAKING** for a manifest asking for
  those.
- **`Manifest.Entry` must be a same-origin absolute path.** It was an
  unvalidated arbitrary URL, and the opaque-origin guarantee has two
  carriers: the sandbox attribute and the `CSP: sandbox` header
  `AssetServer` emits for assets *it* serves. A cross-origin entry escapes
  the second entirely. Dual-enforced in Go and in the JS broker.
- **A blueprint entity `table:` must be an identifier.** It reaches two
  sinks that neither re-escape nor re-validate it: the generated typed
  client emits it into Go string literals, and the runtime interpolates it
  into DDL. `name:` was already constrained to a Go identifier; `table:`
  was the way around that. Found by a property sweep over every IR field
  the emitters read. The tail of the injection class from #134.
- **Kiln enforces its semantic guards during replay, not only at the tool
  call. BREAKING journal format.** The journal is replayed at boot and by
  `kiln freeze`; it recorded *what* was deleted but not that anyone
  approved it, so a hand-authored `.kiln.session.jsonl` installed world
  state the API refuses. Destructive entries now carry the authorizing
  `plan_id` and replay re-checks approval, target match and single-use.
  Plan consumption moved onto the session; the previous per-process map
  was never rebuilt by replay, so a restart re-armed every spent approval.
- **CORS's wildcard writer forwards `Flush` and `Hijack`.** Every other
  wrapper in `core/middleware` does, with a test pinning it;
  `stripCredsWriter` did not, so behind `AllowedOrigins: ["*"]` the SSE
  bus lost its `Flusher` (a hard failure; the SSE constructor
  type-asserts it) and WebSocket upgrade lost its `Hijacker`. `Flush` now
  strips the credentials header too, and `Vary` is appended rather than
  clobbering an upstream `Vary: Accept-Encoding`.
- **`core-ui/urlsafe` is now genuinely the single URL guard.** v0.45.0
  claimed this and delivered one call site. The remaining six now call
  it: `framework/ui`, `framework/uihost`, `framework/crud`,
  `framework/experimental/apiversions`, and the `tree` / `nestedlist` /
  `breadcrumbs` pattern builders. `framework/ui` splits
  anchor vs subresource policy (`mailto:` on an `<img src>` is dropped),
  and a `repolint` rule fails the build if a seventh copy appears.

### Fixed

- **`WithAPIPrefix` applies to `EntityConfig.Endpoints`** (#139). A
  relative `Endpoint.Path` is documented as resolving against the entity
  table path, but the prefix was applied only to the generated CRUD
  routes, so an app using both had its API split across two prefixes with
  no warning. Absolute paths still bypass the prefix. **BREAKING** for an
  app relying on the unprefixed mount.
- **`webhook.NewSQLStore` works on Postgres** (#141). Both stores
  dialect-switched their timestamp columns but hardcoded `payload BLOB`,
  and Postgres has no `BLOB` type, so the outbound webhook battery could
  not be constructed against the dialect it is most likely deployed on.
- **`queue.DBQueue.Close()` returns when `Start` was never called**
  (#141). It waited on a channel only `Start`'s goroutine closes, so a
  failed startup sequence hung on shutdown. `Start` is now idempotent and
  refuses to spawn workers after `Close`.
- **Grouped entities' MCP tools reach their routes** (#136). `App.Entity`
  sets `crudHandler.BasePath` from the API prefix; `GroupEntity` never
  did, so a grouped entity's tools dispatched to `/widgets` while the
  routes lived at `/api/widgets`. Fail-closed (a 404), but the tools were
  simply broken for every grouped entity.
- **`RouteGroup.Prefix()` composes nested prefixes.** It returned only its
  own segment, so `app.Group("/api").Group("/v2")` reported `/v2`, and
  both the route-collision pre-flight and the MCP dispatch above were
  checking a path that does not exist.

### Added

- **`upload.RangeGetter`** (#138): an optional capability a `Storage`
  backend implements to expose seekable reads, so `upload.ServeHandler`
  answers `Range:` requests with a 206 instead of the whole body. Local
  and memory backends implement it; S3 declines (a network backend would
  have to buffer the whole object to `Seek`; use `WithPresigner`).
  `Storage.Get` keeps its narrow `io.ReadCloser` contract.
- **`mcp.Server.ListToolsFor(ctx)`**: the caller-filtered listing.
  `ListTools()` stays unfiltered for in-process introspection and is
  documented as never safe to serve to a remote caller.

## [0.45.0] - 2026-07-25

A full security pass: two-profile audit, 30 findings, every one fixed
with a test that failed on the bug path first. The shape of the whole
audit is **guard drift**: a check one code path has and its sibling
skips. Fixing the shape mattered more than fixing each instance, so four
duplicated guards collapsed into single definitions.

Most entries below are BREAKING. That is deliberate: the framework is
young enough that the right default is worth more than compatibility with
a wrong one.

### Security

- **`?include=` requires a registered relation target.** A leaf segment
  whose target was not in the registry set `Target = nil` and loaded
  anyway, dropping every guard keyed off it: the Hidden-column scrub,
  owner scope, tenant scope, the soft-delete filter, and the
  scoped-filter field allow-list. The blueprint emits exactly the arming
  config, `auth.NewEntityUserStore(db, "auth_users")` is a self-migrated
  table holding `password_hash` that is never registered, so a generated
  entity with a writable author FK let any caller read another user's
  row and iterate the FK to dump the table.
- **`?include=` is bounded.** No depth cap, node budget, or LIMIT
  existed: 23 request bytes produced a 13.7 MB response, and two levels
  deeper exhausted memory. Now 4 relation hops and 20,000 related-row
  references, with shared subtrees converted once instead of once per
  parent.
- **Two-factor enforcement covers every login path.** It read the
  negative flag `PendingTwoFactor`, set in exactly one place: the
  password handler. Magic-link verify and the OAuth callback minted
  sessions the enforcement never saw, so both yielded a fully-privileged
  session for a 2FA-enrolled user, which could then disable the factor.
  `AuthManager.MintSession` is now the only way a session is created.
- **Step-up is checked positively.** A session minted *before* the user
  enrolled kept `PendingTwoFactor=false` forever and stayed "stepped up"
  for its whole life. The 2FA self-service endpoints now require
  `TwoFactorVerified`.
- **The OAuth callback is bound to the browser that started the flow**,
  and **a magic link no longer signs you in on click**; it renders a
  confirmation naming the account. Both were GETs minting a session from
  a credential an attacker can hold, so an attacker could sign a victim
  into the *attacker's* account.
- **Five browser gadgets closed**, each proved in a real browser:
  `data-behavior` as an unvalidated `<script src>` sink; a
  protocol-relative form action defeating the cross-origin check; 13
  runtime fetch sites taking their URL from a DOM attribute with no
  origin check (and forwarding the CSRF token); a query-string-seeded
  signal reaching `innerHTML`; and `__proto__` through `setSignal`, the
  one gadget CSP cannot stop.
- **The untrusted node IR emits a narrower attribute set.** Its
  pass-through was a deny-list of three names, so `style`, every `on*`,
  `srcdoc`, and every privileged `data-fui-*` went through.
- **`gofastr dev` will not serve its mutating MCP surface off-loopback**,
  and **`WithMCPControl` tools require an authenticated caller.** The
  transport's loopback Host pin stops DNS rebinding but not a direct TCP
  client, which sets Host freely.
- **Harness quiet mode cannot auto-allow a command that spawns a process
  or writes a file** (`find -exec`, `find -delete`, `rg --pre`,
  `git --output=`). An auto-allow publishes no permission request, so
  none of it surfaced.
- **`gofastr generate` stops looking for a config at the repo root**, and
  a *discovered* config's `command` extension needs an explicit opt-in. A
  config planted in any shared ancestor ran as the developer.
- **The plugin iframe sandbox is an allow-list.** Stripping only
  `allow-same-origin` left `allow-popups-to-escape-sandbox`, which yields
  a fully unsandboxed same-origin popup.
- **Kiln stops serving and freezing secrets.** `/kiln/world` returned
  `jwt_secret` and `seed_password` verbatim, linked from every page;
  freeze wrote them into `gofastr.yml` and dropped `world.json` at 0644.
- **PDF rendering has a network gate**, and the print CSP is emitted
  in-document. The renderer navigated a `data:` URL with unrestricted
  network access; a `data:` URL has no headers, so the route's CSP
  never applied. Fetched bytes are rendered into the downloaded PDF.
- **`_like` means literal substring at every depth.** Nested filters
  passed the value through as a raw pattern while the top level escaped
  it, so the same parameter meant two things.
- **Request-layer defaults that looked configured and were not:** the
  `__Host-` CSRF cookie never promoted behind a TLS-terminating proxy;
  idempotency's nil `Principal` shared one key namespace across users and
  replayed response bodies between them; the default CSP named neither
  `form-action` nor `object-src`, neither of which falls back to
  `default-src`; static exports shipped no CSP at all;
  `X-Forwarded-Host` was trusted and reflected into the `Link:` header
  naming the MCP endpoint.
- **One shared guard replaces four drifted copies each:**
  `core-ui/urlsafe` (URL schemes; `core-ui/html` had none at all),
  `core/netguard` (internal addresses; the webhook copy missed CGNAT and
  IPv4-mapped IPv6), `AuthManager.MintSession`, and the runtime's
  `_originOK`.
- **Supply chain:** `govulncheck` pinned, `actions/checkout` SHA-pinned
  in release.yml, `permissions: contents: read` at the top of ci.yml, and
  `make secret-scan` widened past `*.go` to yml/json/env/sh/Dockerfile;
  a credential in any of those was never scanned. The two secret gates
  also disagreed on their exemption marker, so a fixture annotated for
  one tripped the other.

### Changed

- The **disclosure** feature (aria-expanded mirroring, Escape-to-close,
  menu focus-on-open, the inert focus trap) moved from the core runtime
  bundle into a demand-loaded module. The browser fixes pushed core past
  its 12 KB gzip budget, and the budget's rule is to carve, never to
  raise the line.

### Upgrading

Every breaking change and its upgrade action is listed in
`cmd/gofastr/upgrades.yml`; `gofastr upgrade` prints the ones that apply
to your project.

## [0.44.0] - 2026-07-25

Closes the screen-router umbrella (#130) with its last open slice, adds
URL round-tripping to `ui.PaneHost` (#132), and settles what
`examples/meridian` is (#131). One core-runtime carve made the first of
those possible.

### Added

- **Intercepting routes**: `app.InterceptFrom("/products",
  app.ScreenDrawer)` as a registration option makes a detail screen
  present as a drawer or sheet over its list when reached by a soft
  navigation from that list, while staying an ordinary page for a hard
  load, refresh, external link, or navigation from anywhere else. The
  deep link remains the canonical render; one `RenderCtx` serves both
  presentations. The **server** decides: the client sends
  `X-Gofastr-Intercept` plus where it navigated from, the framework
  re-resolves that origin against the route table, and only agreement
  produces `X-Gofastr-Overlay`. A forged origin header changes the
  wrapper element and nothing else: policy, params, `Load`, and content
  are identical on both paths. Closing routes through `history.back()`,
  so Back, Escape, the backdrop, and `data-fui-intercept-close` are one
  path and the list underneath is never refetched. Costs nothing on apps
  that don't use it: the demand module and `app.InterceptOverlayCSS()`
  both load only when a route declares an intercept. Fixture:
  `/examples/catalog`.
- **PaneHost deep-linking**: `PaneHostConfig.DeepLinkParam` plus
  `interactive.PaneKey` round-trip pane state through the URL
  (`?pane=secondary:4021`). Opening writes it, closing strips it, and
  Back replays the state by re-clicking the matching trigger, so the RPC
  runs again and the pane returns **filled** rather than empty. The
  server owns first paint: `ui.PaneDeepLink` parses the parameter so a
  screen can render the pane open and populated in the first response.
  Opt-in throughout: a host without `DeepLinkParam` emits no marker and
  no existing `PaneHost` changes behavior. An unkeyed trigger opens its
  pane without touching the URL.
- **Blueprint build gate for meridian**:
  `examples/meridian/blueprint_gate_test.go` generates `gofastr.yml`
  into a scratch package and compiles it, so the blueprint cannot rot
  into one that no longer produces a buildable app.

### Changed

- **`gofastr generate --force` says what it destroys.** It previously
  skipped the conflict check entirely and discarded hand edits in
  silence, which was the mechanism behind #131. It now compares each target with
  the bytes it is about to write and names every file that differs. It
  warns rather than refuses, because `examples/ecommerce` regenerates
  with `--force` by design.
- **`examples/meridian` is documented as hand-maintained**, not
  regenerable. It was seeded by the blueprint and hand-evolved since;
  the generator does not emit `inkTheme`, `appIconPNG`, the `sdkdocs`
  mount, `ResourceConfig`'s island table, or its keyboard/visual/API-
  token suites. Misleading "Code generated by gofastr" headers became
  provenance notes, `doc.go` states the model, and README plus
  `blueprints.md` drop the claim that it *is* generated.
  `examples/ecommerce` remains the app the generator owns outright.
- **Widget deep links derive from the widget catalog.** Core kept a
  parallel index, built at boot and rebuilt after every SPA navigation,
  whose `params` field was never read. Deriving it on demand deleted
  both copies from the core bundle and removed the
  two-copies-can-disagree failure mode. The two core `popstate`
  listeners also merged into one. Core runtime 12286 → 12149 B gz
  (headroom 2 → 139), which is what made intercepting routes fit.
- `App.Register` takes optional `ScreenOption`s. Existing three-argument
  calls are unaffected.

### Fixed

- **A server-rendered open pane was invisible to the pane-host runtime.**
  Its state started with an empty stack, so `topmost()` reported nothing
  open and ESC, overlay-drawer mode, and bare close all skipped a pane
  the user could plainly see. The stack now seeds from the DOM.

### Known issue

- Widget deep links do not survive **Forward** navigation. The SPA
  `popstate` handler keys on `pathname + search`, so a query-only change
  reads as a page navigation and re-fetches the screen, discarding the
  client-mounted widget. Back works and is covered by a test; Forward is
  deliberately not asserted. Fixing it means teaching the router which
  query parameters describe in-page state.
## [0.43.0] - 2026-07-25

Security audit remediation. A dual-model pass (breadth + depth) across
the previously unaudited surfaces, plus the deterministic gates. Every
fix ships with a `_security_test.go` that fails against the old code.

### Security

- **`/mcp` transport now validates `Origin` and `Host`.** The JSON-RPC
  and SSE handlers had no origin check at all, and `gofastr dev`
  auto-enables the mutating control tools plus every entity's write
  tools with no auth in front of them. A DNS-rebound page could drive
  the whole surface. `core/mcp.Server` gains `SetAllowedHosts`,
  `SetAllowedOrigins` and `SetRequireLoopbackHost`; dev pins loopback.
- **`kiln serve` no longer accepts request-borne agent commands.**
  `POST /kiln/agent` with `name="custom"` supplied the entire argv of a
  spawned process: unauthenticated code execution for anything that
  could reach the tool API. Now behind `--allow-custom-agent`.
  `originGuard` additionally pins `Host` to loopback on a loopback bind,
  closing the rebinding path to the same surface.
- **`gofastr harness` sidecar pins `Host`.** Its chat page serves the
  bearer token in a meta tag on an unauthenticated route, so a rebound
  page could read the token and then drive the agent and auto-approve
  its own tool permissions.
- **Harness quiet-mode allow-list no longer prefix-matches raw shell
  text.** `git status; curl …| sh` was auto-allowed with no permission
  prompt (`QuietMode` is on by default). Shell metacharacters now
  disqualify a command and the allow-listed verb must end on a word
  boundary, so `ls` no longer admits `lsof`.
- **Approvals are stored literally, not as globs.** Approving
  `git diff *` persisted a `filepath.Match` pattern that also matched
  `git diff ; nc attacker 9`, written to disk under `ScopeAlways`.
  `Rule.Glob` opts into pattern semantics for deliberately-authored
  rules only.
- **Blueprint spec strings can no longer escape emitted Go or CSS.** The
  generated e2e test interpolated enum values and field names into a
  backtick raw literal, which has no escape mechanism; the injected
  code was valid Go, compiled, and ran at `go test` time. Sinks now use
  `%q`, field names are validated, and font families are allow-listed
  before reaching the emitted stylesheet.
- **WebSocket handshake and frame parsing follow RFC 6455.** `Upgrade`
  now requires `GET`, `Connection: Upgrade`, version 13 and a 16-byte
  key (accepting looser shapes is an upgrade-desync primitive behind a
  pooling proxy). Frame payloads are read incrementally with a deadline
  instead of allocating the peer-declared length up front. Fragmented
  messages are reassembled; previously a `FIN=0` text frame delivered
  half a message as if whole. Reserved opcodes are rejected.
- **`memory.Save` validates entry names**, which became filenames under
  the store root.
- **Harness `ws`/`rest` origin defaults flipped to deny.** An empty
  `AllowedOrigins` admitted every browser `Origin`, contradicting the
  convention `core/middleware/cors.go` already states.
- Bumped `go.opentelemetry.io/otel` to v1.44.0 for **GO-2026-5158**
  (unbounded `baggage` header parsing), which `middleware.Tracing`
  reached on every request.

### Changed

- **BREAKING**: `?slow=block` / `X-SSE-Slow` is ignored unless the
  broker sets `SSEBrokerConfig.AllowClientSlowMode`. `deliver` walks
  subscribers on the publisher's goroutine, so a request-selected block
  mode let one unauthenticated subscriber stall every other subscriber
  and the calling handler. `BlockTimeout` (default 5s) bounds the stall
  even when enabled; `MaxSubscribers` caps concurrency by rejecting
  newcomers rather than evicting incumbents.
- **BREAKING**: `subscriber_id` only replaces an existing SSE
  subscriber when the reconnect comes from the same caller. Previously
  `?subscriber_id=<victim>` dropped the victim's stream.
- `widget.Definition.RequireSession` gates `/state` and `/chrome` for
  widgets whose signals are not safe to expose anonymously; the gate
  fails closed. The default remains unauthenticated, now documented.

### Added

- CI runs a blocking `security` job (`govulncheck` + secret scan +
  `go mod verify`) on every PR **and** weekly on a schedule: an
  advisory can land against a dependency nobody touched, which is
  exactly how GO-2026-5158 went unnoticed.
- `core/mcp.Server.SetAllowedHosts` / `SetAllowedOrigins` /
  `SetRequireLoopbackHost`: pin the authorities and browser origins the
  MCP transport answers on. `SetRequireLoopbackHost` is the port-agnostic
  form used by `gofastr dev`; `SetAllowedOrigins` is the escape hatch for
  tunnels (ngrok, Codespaces).
- `widget.Definition.RequireSession` plus `widget.SetSessionCheck` /
  `widget.SessionCheck`: the host-installed predicate the gate
  consults. `framework/uihost` installs it at `Mount`, so the gate works
  out of the box; a host that mounts widgets itself must install one or
  every gated widget stays closed.
- `permission.Rule.Glob`: opts a rule into `filepath.Match` semantics.
  Rules are matched literally without it.
- `SSEBrokerConfig.AllowClientSlowMode`, `BlockTimeout`,
  `MaxSubscribers`.
- `kiln serve --allow-custom-agent`.

## [0.42.0] - 2026-07-24

The screen-router feature pack (#130) and the typed island-wiring
migration (#129) ship together, plus the dynamic-route agent surface
they exposed: per-URL llm.md, post-Load titles and SEO on SPA
navigation, and policy-aware markdown everywhere.

### Added

- **Catch-all route segments**: `site.Register("/docs/{path...}", …)`
  (or `:path*`) captures one or more trailing segments into a single
  joined param delivered via `SetParams`. Must be the final segment; at
  least one remainder segment is required; registration order remains
  the only precedence rule. The Go resolver and the client-side route
  matcher stay in sync. The example site's per-slug docs registration
  loop is now a single catch-all with `StaticPaths` (same URL set for
  export, sitemap, llm.md, and the strict axe-coverage manifest).
- **Typed param constraints**: `{id:int}`, `{id:uuid}`,
  `{handle:alpha}`, `{handle:alnum}` restrict what a segment matches;
  a non-matching value falls through to the next route. There is
  deliberately no `string` constraint (it would be an unconstrained
  no-op). Constraints are enforced server-side; the client matcher
  treats them as plain dynamic segments and lets the server 404.
- **Declarative redirects**: `App.Redirect(from, to)` and
  `App.RedirectPattern("/old/{id}", "/new/{id}")` with param
  passthrough. Hard GETs get a permanent 308; SPA navigation follows
  via `X-Gofastr-Location`; chains collapse to one hop and cycles fail
  closed. Open-redirect hardening covers the registered target AND the
  substituted result (backslash, `//host`, scheme forms). A redirect
  whose pattern *overlaps* a dynamic screen (any shared URL; param
  names are irrelevant) panics at registration in either order, because
  redirects are consulted before screens. Redirect entries appear in
  `Routes()` (`RedirectTo`) and the client route manifest (`redirect`);
  static export, sitemap, and llm.md skip them.
- **Fail-loud `SetParams` guard**: registering a dynamic route whose
  component doesn't implement `SetParams` now panics at boot, naming
  the path and component type. Params were previously dropped silently.
- **Per-URL llm.md for dynamic routes**: `app.ScreenLLMMDForPath`
  builds the same per-request instance the page render uses (SetParams
  → DI → Load), so `/products/42/llm.md` (live, gated by
  `WithPublicLLMMD`), the static export, and markdown content
  negotiation all serve that page's real title, SEO front matter, and
  content instead of one generic pattern doc.
- **Post-Load titles and SEO on dynamic routes**: `RenderResult` now
  carries the effective post-Load `Title` and the loaded `Component`;
  SPA partials send the loaded title via `X-Gofastr-Title`, and the
  HTML head's meta description / SEO bundle resolve against the loaded
  instance (live and static export). Dynamic pages no longer show a
  stale tab title on in-app navigation or a missing meta description.
- **Typed interactive wiring (#129)**: `core-ui/interactive` gains
  `Action.WithBody` (validated JSON body), `Action.Attrs()` (merge
  typed wiring into an existing attrs map byte-identically),
  `OpenOnClick`, `ToastOnClick` (+ typed `Toast`), `OpenPaneOnClick` /
  `ClosePaneOnClick`, and `BindText` / `BindHTML` / `BindAttr` signal
  region bindings. The blueprint generator, `battery/admin`, and the
  examples now emit typed wiring, so generated apps teach the typed form,
  and a repolint gate bans raw `"data-fui-rpc"` literals in the
  generator and batteries. Emitted HTML is byte-identical (documented
  inert deltas only).

### Fixed

- **Policy-gated screens no longer leak through markdown surfaces.**
  Every llm.md surface now evaluates the screen's policy chain
  with the live request: per-screen handlers, the dynamic-route
  fallback, markdown negotiation, the `/llm-pages.md` index, and the
  static export. A non-Allow decision serves a metadata-free withheld
  doc (route path and type only); the index lists gated screens
  path-only. Previously a gated screen's rendered content, title,
  description, and SEO bundle could be read via its llm.md sibling.
- **Generated apps pass their own accessibility gate.** Status-badge
  text colors now mix toward `--color-text` (AA contrast in both
  schemes for any reasonable palette), generated empty states declare
  correct heading levels (h2 under a page h1; h1 when they are the
  whole page), and the scaffolded example palette's `text-muted` was
  corrected. The blueprint's axe gate is green on every page in both
  color schemes.
- **Generator emits `SetParams` for every dynamic screen**: including
  static-body screens and `:colon`-syntax routes (both previously
  panicked at boot under the new guard), reading the record param
  (`id` when declared, else the last param) rather than the first.
- **SSG no longer silently skips dynamic routes**: the static builder
  warns, naming the route and the `StaticPaths` fix, when a dynamic
  route can't be expanded.
- **Route manifest ordering is deterministic** (redirect entries were
  map-ordered), and llm.md for expanded static pages is written
  per-URL instead of stamping one pattern doc.

### Docs

- `ui-getting-started`: dynamic params + guard, catch-all syntax,
  twin-registration for optional segments, redirects (incl. overlap
  rules), constraints. `static-export`: `StaticPaths` section.
  `agent-ready`: dynamic-route docs + policy-withheld contract.
  `interactive-patterns`: the new typed helpers.
- #130 slices 4 (named outlets) and 5 (intercepting routes) have design
  notes on the issue; slice 4 resolved into #132 (PaneHost
  deep-linking), slice 5 remains open pending a runtime-budget carve.

## [0.41.0] - 2026-07-23

Readiness and UI contracts: the long-deferred DX roadmap items ship
together: a one-option secure browser-auth posture, grouped entity
sub-configs, typed form fields, both-order collision diagnostics, plus
a widget-runtime module split that deletes every size-budget override,
and the docs that make the v1 gate concrete (stability policy, external
pilot program).

### Added

- **`auth.WithBFFPosture(mgr, cfg)`: cookie-only browser auth in one
  option.** Cookie-only login (no JWT in the body), `__Host-session`
  upgrade of the dev cookie name, exact-origin allowlist + credentialed
  CORS on the API prefix (composes with `WithAPIPrefix` in either
  order), bearer JWTs rejected on the BFF surface while `gfsk_`
  automation tokens pass, and global CSRF with exactly the auth logout
  route exempt (that handler enforces same-origin submission itself, so
  the static `ui.SignOut` form keeps working). See `gofastr docs auth`.
- **Grouped `EntityConfig` sub-configs**: `Scope`, `Pagination`, and
  `Exposure` pointer groups (plus declaration mirrors and blueprint
  `scope:`/`pagination:`/`exposure:` keys) make the semantic
  relationships between the 17+ flat fields visible. Grouped values are
  authoritative, including at the App layer, so `Exposure.CRUD=&false`
  really skips route mounting. Flat fields keep compiling through the
  documented compatibility window.
- **Typed form-field wrappers**: `ui.TextField`, `ui.NumberField`,
  `ui.DateField` compose `FormField` + `html.Input` with typed
  Required/Placeholder/bounds/Error config and the ARIA wiring; a form
  built with them has zero `html.Attrs` literals at the call site.
- **Collapsible sidebar variants**: persistent, collapsible
  (local-storage persisted via `data-fui-sidebar-storage`), and
  off-canvas; the toggle demand-loads the sidebar runtime module and
  keeps `aria-expanded` synchronized.
- **`app.public_openapi` blueprint key** → `framework.WithPublicOpenAPI()`,
  closing the last declaration-first follow-up.
- **`gofastr init` pins the generated `go.mod`** to the CLI's own
  framework release; `init`/`generate`/`validate` answer `--help` with
  their own usage.
- **API stability policy** (`gofastr docs stability`) and the
  **external pilot program** (`docs/pilot-program.md`): the v1 gate's
  paperwork, written down and linked from the roadmap.

### Changed

- **Widget runtime split into demand-loaded modules**: widgethelpers /
  widgetfocus / widgetlinks carry the optional behaviors, so a basic
  modal stops paying for every form helper. Every gzip-budget override
  is deleted; core is back under 12KB. All split modules follow the
  full self-registration contract (scanner + loaded flag), so remounted
  widgets and poll-swapped content re-wire correctly.
- **Entity/screen collision diagnostics fire in both registration
  orders**: mountables expose `RoutePatterns()` and `Mount` pre-checks
  them against entity CRUD space, so screen-mounted-second gets the
  same actionable message as entity-registered-second.
- **`gofastr audit lint` precision**: AST-based `t.Skip` detection,
  statement-anchored SQL rule, `csrf-exempt:`/`safe-html:` annotations;
  zero findings on both repo examples.
- **Composed-page a11y on the example site**: one main landmark,
  heading order, unique nav labels; banner/status-pill/terminal-block
  drop decorative stripes and glows for token-driven outlines.
- Idempotency table setup and expired-claim eviction fail closed on DB
  errors; outbox delivery logs previously ignored exec errors.

## [0.40.0] - 2026-07-23

Strict mode: missing launch hygiene becomes errors instead of silently
shipping. An opt-in `uihost.WithStrict()` refuses to serve an app whose
screens lack SEO or, in dev, an accessibility test, every check
individually tunable; `gofastr dev` now enforces the same static a11y
gate `gofastr build` always had; and generated apps ship the whole bar
turned on with a surface that passes it honestly. Hardened by a
two-round review (Claude + GLM ×2 + Sol, then a Sol verification pass
over the fixes): 12 findings, all closed.

### Added

- **`uihost.WithStrict`: launch hygiene as boot failures.** At Mount
  the host validates its declared surface and panics listing every
  finding at once, each with its remedy: page-screen titles and
  descriptions (a zero-value `ScreenSEO` return stays the documented
  per-page opt-out), site description, icon, sitemap (with a validated
  bare-origin `BaseURL`), robots, and, under `gofastr dev` only, an
  axe scan on record for every page route. Battery-registered screens
  (admin back-office) sit outside the checks by architecture. See
  `gofastr docs strict-mode`.
- **`uihost.StrictConfig`: every check individually tunable.**
  Per-check `StrictEnforce`/`StrictWarn`/`StrictOff` levels,
  `ExemptScreens` route patterns (exact or `/prefix/*`), and a
  dedicated posture for the missing-manifest case (default: warn, so
  `gofastr generate` → `gofastr dev` is never walled behind a Chrome
  run). The zero value of every field is the strictest setting;
  configuration only ever relaxes, visibly. Composition is last-wins
  and total: a later bare `WithStrict()` restores full enforcement.
- **`framework/testkit/axetest`: the axe harness is public.** The
  vendored axe-core harness (browser/tab factories, the color-scheme
  freeze, `Scan`) moved out of `internal/` so host apps can pin the
  runtime audit as their own test. Every successful `Scan` records the
  scanned page into the axe-coverage manifest
  (`.gofastr/axe-coverage.json`, new `framework/axecov` package,
  `GOFASTR_AXE_COVERAGE=0` opts out) under a canonical root shared
  with strict mode: Go's own discovery rule (nearest `go.work`, else
  nearest `go.mod`), `GOFASTR_AXE_COVERAGE_DIR` to override. Coverage
  resolves through the router: one scanned `/docs/install` covers
  `/docs/:slug`, and strict only demands dynamic routes whose
  `StaticPaths` returns real instances.
- **Generated apps ship strict.** `gofastr generate` emits
  `WithStrict()` plus the surface that passes it: a site description
  (`app.description`, or derived honestly from the app name +
  entities), a sitemap rooted at `APP_BASE_URL` (fallback
  `app.base_url`), robots + sitemap excluding the admin back-office,
  title fallbacks, `ScreenSEO` opt-outs instead of empty describers,
  and an `axe_test.go` that boots the built binary and scans every
  sitemap page under both color schemes in two passes: an anonymous
  browser for public/guest pages and a separately-authenticated one
  (HTTP login through the auth battery, prefix-aware cookie
  transplant, so `__Host-` production cookies work) for gated screens,
  with a redirect assertion so a bounced scan can never cover the
  wrong page. The generated `.gitignore` now always ships and covers
  `.gofastr/`.
- **`dev.Enabled()`**: the base `GOFASTR_DEV` predicate the
  per-feature dev gates refine.

### Changed

- **`gofastr dev` enforces the static accessibility lint on every
  rebuild**: the same gate `gofastr build` has always run, with the
  same `--no-a11y` escape hatch. An app with standing findings now
  stops at the dev rebuild (watcher keeps running; fixing and saving
  retries) instead of surfacing them at build/CI time. Fix the
  findings or run `gofastr dev --no-a11y` while you do.

A first-contact pass over the README and the product site (gofastr.dev):
lead with what the framework does, in plain words, before the origin
story. Plus two small additive surfaces: a full-corpus `/llms-full.txt`
tier and `.md`-aware dev rebuilds, plus public-hosting hardening of the
example site's live interactive demos.

### Added

- **`/llms-full.txt`: the whole-corpus llms tier.**
  `uihost.WithLLMsFullTxt(content)` (or `AgentReadyConfig.FullText`)
  serves the entire docs corpus as one `text/plain` file, alongside the
  existing `/llms.txt` index that links each doc as raw markdown. Nothing
  links it automatically; you supply the concatenated content. See
  `gofastr docs agent-ready`.
- **`gofastr dev` rebuilds on `.md` changes.** The dev watcher now treats
  `.md` files as build inputs, so editing embedded docs (or any markdown
  the app renders) triggers the same rebuild + livereload as a `.go` edit.

### Changed

- **README and product site lead with the product.** The README is
  restructured product-before-biography and run through a plain-words
  pass (no marketing vocab), with one architecture map instead of several
  and a gated get-started tutorial. The site's hero, hubs, comparison,
  and get-started page teach the real scaffolding flow and are pinned by
  tests so the copy can't drift from the code. A new `gofastr` CLI
  reference page, a tiered llms.txt over the embedded docs, and a home
  "numbers you can check" strip (every claim measured live or gated by a
  test) round it out.

### Security

- **The example site's live demos are isolated and bounded per visitor.**
  The interactive demos (`examples/site`: kanban, optimistic
  create/delete, counter) mutated shared package globals: one anonymous
  visitor could vandalize every other visitor's demo, and one list grew
  without bound (memory DoS). Each browser now gets its own demo state
  keyed by a site-owned `site-demo` cookie (an isolation key, not the
  auth session), held in a map bounded by a hard LRU cap and a TTL
  janitor; the `/__site/*` endpoints gained body caps, sortable-order
  dedup, and cookie-shape validation. Example-app hardening (no framework
  API change). The pattern is what any public deployment of a stateful
  demo wants. Reviewed by a Claude + GLM + Sol pass.

### Fixed

- **Distinguishable get-started link.** A bare `http://localhost:8080`
  inline anchor (an axe `link-in-text-block` violation, and a dead link
  on the deployed site) is now a code literal.

## [0.38.1] - 2026-07-21

Post-v0.38.0 cleanup: one e2e deflake and the doc drift found by the
maturity review.

### Fixed

- **Kiln stop-button e2e no longer flakes under CI load.**
  `TestBrowser_StopButtonCancelsInFlightTurn` gave the stop button 2s to
  appear, but since #112 the panel learns `in_flight` solely from its
  2s±10% `/state` poll; the next tick can land after the deadline on a
  loaded runner (which is how the post-#121 main CI run went red while
  the identical PR run passed). Both waits now use the 5s poll-cadence
  headroom every sibling test in the file already uses.
- **`AuthorizeTopic` doc example matches the shipped hook.** The
  live-dashboards tenant-isolation example used a pre-ship
  `(ctx, topic, sid) error` signature; the shipped hook is
  `(ctx, topic) bool` with silent-drop semantics (rejected topics are
  simply never subscribed). `presence.md` already had it right.
- **Doc drift.** `ROADMAP.md` §7 (the v0.20 assessment findings) is
  deleted: all six items shipped between v0.36.0 and v0.38.0, so the
  section was advertising fixed security holes as open work, and
  `CONTRIBUTING.md` named go 1.26.4 while `go.mod` says 1.26.5.

## [0.38.0] - 2026-07-20

The reactivity release (#112). The interactive layer is now truly
stateless, any replica serves any request, and liveness follows an
explicit pull-first ladder: client signals → RPC → polling → SSE push.
The new model doc is `framework/docs/content/reactivity.md`
(`gofastr docs reactivity`).

This release also folds in the v0.32.0–v0.37.0 weekend-range review
fixes (#120): a two-round multi-model pass (Claude + GLM + Sol) over that
range found twenty-two confirmed bugs: SQLite engine correctness, queue
timezone/DST, outbox lease normalization, and one include-filter security
fix, all landed test-first.
### Added

- **Stateless session tokens** (#112). The uihost session map is gone;
  sessions are HMAC-SHA256-signed tokens verified by signature, so a
  session minted on one replica is accepted by every other. The cookie
  carries the signed token; pages embed only the bare stream id, which is
  no longer a credential; the SSE endpoint requires the cookie token to
  verify AND match the requested stream, closing a hole the map never
  covered (subscribing to another session's stream with a leaked id).
- **App-wide secret**: `framework.WithSecret(secret)` or the
  `GOFASTR_SECRET` env var (composes with the `.env` autoload; explicit
  option wins). Subsystem keys are HKDF-derived per purpose; one secret
  is all a multi-replica deployment configures. Single replica with no
  secret keeps today's zero-config semantics (per-boot key; sessions roll
  over on restart, re-minted transparently on next render).
- **Polling: the missing middle rung.** Page level:
  `data-fui-poll="30s"` + `data-fui-poll-src="/path"` re-fetches a
  server-rendered fragment and swaps it through the existing region-swap
  pipeline (Go-duration syntax, 5s floor, ±10% jitter, pauses while the
  tab is hidden, backs off on failures; demand-loaded module, core
  runtime budget untouched). Widget level: `Builder.Poll(interval)`
  re-fetches `/state` and re-applies changed signals (trusts Go callers
  down to 100ms). Polling needs no fanout and no held connection; any
  replica answers. The live-dashboard demo now shows the same metrics
  polled (rung 3) next to SSE-pushed (rung 4).
- **`data-fui-rpc-refresh="<widget>"`**: a successful RPC can trigger an
  immediate `/state` re-fetch on a *named* polling widget, not just the
  one the button lives in (e.g. a Reset button inside a confirm modal
  refreshing the chat panel).


- **Cron schedules in a named timezone.** `DurableScheduleBuilder.RegisterAt`
  with a time in an IANA zone (e.g. `America/New_York`) evaluates the cron
  spec in that zone's wall-clock time, across DST transitions. The zone name
  persists with the schedule (`tz` column, idempotent migration). `Register()`
  still anchors in UTC: existing schedules keep their fire times, and
  `time.Local` / fixed-offset zones deliberately collapse to UTC because they
  would resolve differently per replica.
### Fixed

- **Two tabs sharing one session now BOTH receive every SSE update.**
  `island.Manager` previously handed all of a session's subscribers one
  shared channel, so same-session tabs competed for frames (first
  receiver wins). Each subscriber now gets its own buffered channel and
  delivery broadcasts. **BREAKING**: `Manager.Subscribe` returns
  `(<-chan IslandUpdate, func())`: the cancel replaces the removed
  `Manager.Unsubscribe`; `ConnectSession` changed the same way.
- **SPA session rollover: both nav branches.** A partial navigation
  (`X-Gofastr-Navigate`) with a stale/expired session token re-mints the
  cookie and names the fresh stream id in `X-Gofastr-Session`; a
  cross-layout navigation (full fetch) copies the freshly rendered
  head's SSE meta. Either way the runtime rewires the live meta so the
  SSE reconnect loop recovers without a hard reload.
- **Presence hook panics no longer strand state.** `OnPresenceChange`
  fires under a recover (roster stays consistent, replica announcements
  still go out), and the SSE handler defers its stream cancel before the
  presence join so a panicking hook can't leak the subscription.
- **`data-fui-poll` durations parse like Go.** Fractions (`1.5m`) and
  full-string validation: a typo now leaves the region unwired instead
  of silently polling at the wrong cadence.
- **Long poll intervals no longer hammer the server.** `Builder.Poll`
  values above ~24.8 days used to wrap through 32-bit coercion and the
  32-bit `setTimeout` ceiling into a ~10 req/s loop; scheduled delays are
  now magnitude-preserved and clamped, so a long poll fires slightly
  early instead of continuously.
- **SPA rollover recovers on error responses too.** A partial navigation
  to a 404 or policy-blocked route with a stale token re-mints and the
  runtime applies the fresh stream id before it bails on the non-2xx,
  and every re-mint response (success or error) is `Cache-Control:
  no-store`, as is the widget `/state` endpoint.
- **kiln reload converges after a dropped connection.** A page-structure
  edit missed during the reconnect window now refreshes on the next
  `ready` frame instead of leaving the page stale.
- **Idle pages recover a dead session without navigating.** When a
  restart/rotation/expiry kills the token under an open tab, the SSE
  module (which can't see the 401) re-mints via `POST /__gofastr/session`
  after repeated reconnect failures, rewrites the stream id, and
  reconnects, so recovery no longer depends on the user navigating.
- **Poll back-off on HTTP errors.** Both pollers treated a non-2xx
  response as a silent no-op; it now reaches the back-off path. A
  hidden→visible flip while a fetch is in flight can no longer arm a
  second timer chain (single-chain guard), and a successful widget RPC
  triggers an immediate `/state` re-fetch so mutations reflect at once.


From the v0.32.0–v0.37.0 weekend-range review (#120):

- **SQLite: failed statement no longer breaks transaction rollback.** A
  statement failing inside an explicit transaction (e.g. a multi-row `INSERT`
  hitting `UNIQUE`) used to flush and replace pager state, so a later
  `ROLLBACK` silently kept the transaction's earlier writes. Statement
  rollback is now a pure in-memory snapshot that preserves the transaction's
  pre-`BEGIN` page images.
- **SQLite: multi-row `UPDATE` enforces `UNIQUE` across its own rows.**
  `UPDATE t SET u = 3` over two rows used to commit both; pending rows are
  now checked against each other (with partial-index predicates honored) and
  the statement fails like real SQLite.
- **SQLite: `UPDATE` maintains secondary indexes.** The index write sat in an
  error branch, so a successful update never refreshed indexes and stale
  entries kept matching the old value. Updates now delete the old index
  entries and re-insert per the new row.
- **SQLite: dynamic column defaults survive reopen.** `DEFAULT
  CURRENT_TIMESTAMP` (and other expressions) now serialize with the schema
  (`default_expr`), so file-backed databases no longer fail `NOT NULL`
  inserts after a restart. Legacy files keep their constant defaults,
  including empty-string defaults like `lane TEXT NOT NULL DEFAULT ''`.
- **SQLite: `INTEGER` affinity keeps fractional values REAL.** `DEFAULT 1.5`
  stored `1`; conversion now happens only when lossless, matching SQLite's
  affinity rule.
- **SQLite: partial unique indexes honor their `WHERE` predicate** at
  creation, on insert, and on update. Out-of-predicate duplicates no longer
  reject index creation, and enforcement applies only to matching rows.
  Partial indexes stay out of scan planning (correctness over speed).
- **Queue: non-UTC cron schedules no longer kill the scheduler.** A schedule
  registered with a non-UTC anchor errored on its first tick, and that error
  stopped `Start` and took every other schedule with it. Evaluation now
  follows the registration zone, and a schedule that cannot produce a due
  tick is logged and skipped instead of being fatal.
- **Queue: changing a schedule's cadence self-heals.** Re-registering with a
  different cron/interval stranded the old watermark and hit the same fatal
  error; the scheduler now advances to the next valid occurrence of the new
  spec through the same version-guarded compare-and-swap as a normal tick.
- **Outbox: legacy timestamp formats no longer corrupt lease comparisons.**
  Rows written by mattn/go-sqlite3 (space-separated timestamps) compare
  lexicographically against the pure driver's RFC3339 values, so an active
  future lease read as expired. That reclaimed in-flight deliveries (double
  delivery) and mis-timed grace cutoffs. The relay now normalizes legacy
  values to the canonical format at start (idempotent, sqlite-only); the
  atomic single-`UPDATE` claim path is unchanged.
- **Security: hidden columns can no longer be probed through include
  scoped filters.** `?include=rel(password_hash_like=SEC%)` accepted
  filters on `Hidden` columns of the include target; the related row's
  presence in the response leaked whether the value matched
  (prefix-bruteforceable), the same oracle the strict-filter release
  closed on the flat, nested, and `?where=` paths. A hidden field now gets
  the identical "not on target entity" rejection a nonexistent field gets.
- **SQLite (round 2): nine more engine fixes**, found by reviewing the
  first round's fixes and sweeping untouched paths for the same defect
  classes: DDL inside a rolled-back transaction no longer resurfaces after
  reopen (schema flushes are deferred to commit; rollback restores the
  header, schema pointer, allocated pages, and file length); `ALTER TABLE
  ADD COLUMN` no longer corrupts indexed reads (index records were decoded
  as table records and padded with defaults, returning the wrong row);
  plain multi-row `VALUES` inserts now enforce separately created unique
  indexes; `RENAME COLUMN` renames through index metadata, unique
  constraints, and partial-index predicates; a valid multi-row `UPDATE`
  key shift (`SET u = u - 1`) is no longer rejected; partial-index
  predicates use SQL truthiness (`0.5` is true); `INSERT OR REPLACE` is
  implemented (it previously failed to parse); `DELETE` and upsert paths
  maintain index entries; REAL/NUMERIC/TEXT affinity conversions match
  SQLite (signed/padded numeric text, lossless-integral to INTEGER,
  numbers to text, blobs stay blobs).
- **Queue: legacy timestamp formats normalized, like the outbox.** The
  same mixed-format lexicographic comparison reclaimed in-flight jobs with
  active leases (double execution) and ran future-scheduled jobs
  immediately (retry backoff voided). `NewDBQueue` now normalizes the job
  and scheduler tables the way the outbox relay does.
- **Queue: DST transitions follow vixie cron.** A schedule in a zone that
  falls back no longer fires twice in the repeated hour, and one whose
  wall time is skipped by spring-forward fires once at the transition
  instant instead of silently losing the day (registration and watermark
  advance included).
- **Outbox: normalization is idempotent on the CGO driver too.** The
  canonical-format check now probes the layout the connected driver
  actually binds, so hosts on mattn/go-sqlite3 no longer rewrite every row
  at every relay start; a failed table probe other than "no such table"
  (e.g. a locked file) is now logged instead of silently skipping.
- **Auth: scope-denied responses use the canonical error envelope.**
  `RequireAPIScopes` / `RequireScope` returned a nested
  `{"error":{...}}` with `Content-Type: text/plain`, which the generated JS
  SDK rendered as `api: 403: [object Object]`. Both now emit the flat
  `{"error","success","code"}` envelope as `application/json`; every
  `battery/auth` error response now carries the `code` field.
### Changed

- **BREAKING: `WithFanout` now requires an app secret.** Boot fails with
  an actionable message when a fanout is attached and no
  `WithSecret`/`GOFASTR_SECRET` is set: a multi-replica deployment
  without a shared session key would 401 half of all session checks, so
  it fails at boot instead of in traffic. Sticky sessions are no longer
  part of the scaling contract.
- **kiln chat panel** polls its `/state` every 2s instead of holding
  per-event SSE bindings. Page-structure world edits (add/delete
  page/route, session reset) still force an SPA refresh of the current
  page via a new kiln-owned build-mode reload client
  (`/.kiln/reload.js`), the same dev-mode-SSE exception class as
  `framework/dev` livereload.

### Removed

- **BREAKING: the dead stateful island/signal surface**: all with zero
  production callers, retained live state in one replica's RAM, and the
  modern runtime never called them: `UIHost.RegisterSignal`,
  `UIHost.RegisterWidget`, `UIHost.PushIsland`, `UIHost.GetSession`, the
  `SignalAny` interface, the `POST /__gofastr/signal/{id}` endpoint (now
  a plain 404), and `island.Manager`'s island-object retention
  (`Register`, `Unregister`, `Push`, `Get`, `ListBySession`, plus
  `Island.SessionID`, `Island.Update`). Surviving push surface: render the HTML yourself
  and `Manager.PushUpdate`, or presence. The client-side signal seed
  (`#gofastr-signals`) is unrelated and unchanged.
- **BREAKING: widget SSE bindings**: `SSEBinding`, `Builder.SSE`,
  `Builder.SSERefetch`, `Builder.SSERefresh`, `Builder.SSEReload`, the
  `"sse"` widget-catalog key, and the per-widget `EventSource` block in
  the runtime (each widget opened private connections, contradicting the
  one-bus contract). Widgets that need passive freshness use
  `Builder.Poll`; genuinely push-shaped surfaces use the shared bus.

## [0.37.0] - 2026-07-19

Accuracy-and-fixes release: closes two adapter/queue issues and runs a
multi-model audit over the README and the shipped agent skills, correcting
claims that had drifted from the code. No breaking changes.

### Added

- **Scheduled-job delivery options** (#116). `ScheduleBuilder` and
  `DurableScheduleBuilder` gain fluent `Lane` / `Priority` / `MaxAttempts`
  options. The durable scheduler persists them (additive, idempotent column
  migration) and carries them into every fired `Job`; re-registering a schedule
  updates the options without resetting its watermark. Omitted options keep
  today's defaults (empty lane, priority 0, max attempts 3). Recurring bulk work
  can now target a dedicated lane instead of standing up a separate table/worker.
- **`CURRENT_TIMESTAMP` / `CURRENT_DATE` / `CURRENT_TIME` column defaults** in
  the bundled pure-Go SQLite adapter (part of #115), evaluated per insert. The
  auth OAuth-links table (`created_at ... DEFAULT CURRENT_TIMESTAMP`) now works
  on the bundled adapter.

### Fixed

- **SQLite adapter applies column `DEFAULT` before `NOT NULL`** (#115). An
  omitted `NOT NULL DEFAULT ...` column no longer fails. Bare `TRUE`/`FALSE` now
  parse as integer literals (quoted `"true"`/`` `true` ``/`[true]` still resolve
  as identifiers), the no-primary-key insert path is unified through the same
  builder as the conflict path (so `NOT NULL` is always enforced and an explicit
  `NULL` still fails even when a default exists), and omitted defaults inherit
  column affinity.
- **SQLite `INSERT` fidelity** (#118, #119, surfaced during the #115 review). An
  `INSERT` with a column/value count mismatch now errors instead of silently
  defaulting the shortfall or dropping the excess, and `INSERT OR IGNORE` skips a
  row that violates a constraint (e.g. `NOT NULL`) rather than erroring, while a
  plain `INSERT` and non-constraint (arity) errors still fail.

### Changed

- **README accuracy pass.** A multi-model audit corrected ~20 claims that had
  drifted from the code (the Swagger UI path, `core/`/`battery/` counts and
  lists, the MCP/auth reality of the smallest-app snippet, the batch `curl`
  content type) and softened several overclaims. The smallest-app Go snippet is
  now covered by an executable-README gate that boots it and asserts anonymous
  read/write and the registered MCP tools. `pack` and migration wording ("lossy,
  not an inverse"; "a Down section when a safe inverse exists") is aligned across
  the README, `pack.go`, `blueprints.md`, and `migrations.md`.
- **Shipped-skills accuracy pass.** Audited the `.claude/skills` + agent personas
  against the code and corrected stale references: the `battery/log` tool count
  and registration prerequisite, log-level restore semantics, the full set of
  runtime-mutating MCP tools, several `adversarial-tests` reference rows, a
  non-shipped agent/skill reference, a wrong default port, and a persona payload
  description. Two in-code MCP tool descriptions were brought in line with the
  runtime.

## [0.36.0] - 2026-07-19

Production-hardening release: closes the verified OAuth-identity and
multi-replica-correctness findings, cuts filtered-list overhead, and turns
GoFastr's live-data and optimistic-UI capabilities into runnable references.

### BREAKING

- **OAuth login requires a durable link store** (#98). `OAuth2Plugin.Init`
  fails closed when the `UserStore` does not implement `OAuthLinker`; the
  legacy email-only fallback is gone (an IdP emitting an unverified email could
  otherwise sign in as an existing account). `EntityUserStore` now implements
  it (creates a `<table>_oauth_links` table on `EnsureSchema`). A verified-email
  OAuth login refuses (409) an existing password account; the user adds the
  provider via the new authenticated `GET /auth/oauth/{provider}/link` flow.
  Set `AllowInMemoryStores` for local dev. See `upgrades.yml`.

### Added

- **Multi-replica RBAC invalidation** (#99). `framework.WithGrantStore` +
  `WithFanout` propagate grant/revoke across replicas as a refresh-signal
  (receivers re-read authoritative DB rows; code-defined baseline grants are
  preserved), via a non-blocking publish queue + a background refresh worker.
- **Authenticated OAuth link flow** (#98). `GET /auth/oauth/{provider}/link`
  lets a logged-in user bind an additional provider (the signed state carries
  the user id; the callback re-verifies the session and refuses a
  foreign-owned identity).
- **Live-dashboard reference** (#103). `/examples/live-dashboard` plus a new
  `live-dashboards.md` covering update scheduling, delivery semantics (SSE is
  not a durable ledger), tenant isolation, and performance evidence.
- **Optimistic-mutation cookbook** (#104). New `optimistic-ui.md` (lifecycle
  contract + seven executable recipes) and `ConfirmActionConfig.SuccessSignal`
  to reconcile a list region on a successful confirm.
- **Per-screen `llm.md` SEO** (#108). `/{path}/llm.md` now carries YAML
  front-matter mirroring the HTML head SEO (description, canonical, robots,
  Open Graph, Twitter, JSON-LD types, hreflang).

### Changed

- **Startup seeds are advisory-lock serialized** (#99). Entity seeds and
  `WithSeed` hooks run under a distinct Postgres advisory lock so N replicas
  seed once; a `MaxOpenConns(1)` pool cannot hold the lock and skips it with a
  WARN (keep the pool above 1 for multi-replica seed coordination).
- **`email_verified` enforced** (#98). OIDC logins consult `email_verified`
  (fetching userinfo when the id_token omits it, never overwriting a signed
  `false`); an unverified email never binds to an existing account.
- **Positioning pass** (#113). GoFastr is presented as a full-stack Go
  framework that doesn't get in the way of you or your agents: the blueprint
  generator and the entity declaration are optional features, not the
  identity. No API changes. The embedded `framework/docs/content/*` corpus is
  reframed in plain words, full-stack-first (facts/flags/links/samples
  unchanged); the README is repositioned with the blueprint demoted to a
  clearly-optional design bet and a "Built with GoFastr" section; and the docs
  site (`examples/site`) reworks its area hubs into taught pages, renames
  `/patterns` to `/framework`, and moves page metadata/footer onto the tagline.

### Performance

- **Filtered-list overhead cut** (#100). Parse the query string once and thread
  it through the List helpers, pool the row scan buffer, pre-size the query
  builder: ~11–14% fewer bytes and ~4–5% fewer allocs on SQLite and Postgres
  (benchstat), with no change to owner/tenant/RBAC/soft-delete/projection
  semantics.

### Security

- **Cross-issuer OAuth takeover closed** (#98). The `(provider, provider_id)`
  link namespace binds to the state-validated registry key, not the
  provider-returned `Name()`: two OIDC issuers both defaulting to `oidc` could
  otherwise collide and take over each other's accounts. Concurrent-link races
  now fail closed to the durable-PK winner.
- **Streaming soft-delete authorization** (#100). `ServeStreamingList`
  authorized the `?trashed=` gate against the DB-operation context instead of
  the request context (a data-disclosure the buffered List path did not have). It
  now uses `r.Context()`.
- **ConfirmAction signal safety** (#104). `SuccessSignal` names are validated
  (`^[A-Za-z0-9_-]+$`) to prevent selector injection, and a failed RPC no
  longer overwrites an html-mode signal region with an error object.

## [0.35.0] - 2026-07-18

Hardens the list data-plane and the presence surface, and makes GoFastr's UI
capabilities discoverable by job-to-be-done: strict list-filter rejection
(#100, **breaking**), opt-in presence topic authorization (#98), and a UI
capability map (#102).

### Added

- **UI capability map** (#102). A new `ui-capability-map.md` doc (browsable via
  `gofastr docs` and the `framework_docs_*` MCP tools, linked from the README
  and docs catalog) routes from a UI *problem*, whether a live dashboard, optimistic
  board, master/detail, or server-authoritative reactive state, to the GoFastr
  primitives that compose it and the runnable example that proves it, with
  "see also" cross-links across the interactive-patterns / signal-store /
  runtime-contract / events / presence / scaling docs. Adds a
  UI-capability-discovery eval suite.
- **Presence topic authorization** (#98). `island.Manager.AuthorizeTopic`, when
  set, gates which `?presence=<topic>` topics a connection may join. The hook
  runs once per requested topic at SSE-connect time with the request context
  (carrying the server-derived user), **before** any subscription or roster
  emission, so an unauthorized viewer never receives the roster (which can
  contain emails) or join/leave events. Rejection is silent (the topic is
  simply not joined), so the gate is not a private-topic existence oracle. A
  nil hook (the default) authorizes every topic; presence stays public unless
  an app opts in, so existing apps are unaffected. See [presence](framework/docs/content/presence.md)
  → "Topic authorization".

### Changed

- **BREAKING: strict filter parsing on the List endpoint** (#100). An
  unknown top-level filter query param (a typo like `?stauts=open`, or a
  suffixed op on a non-field like `?scor_gt=5`) now returns a **400**
  naming the bad key, with a "did you mean" suggestion when a field is an
  unambiguous near-match, instead of being silently dropped. Silently
  dropping a filter returned an **unfiltered** result set: a broken client
  read the whole table and an attacker's probe was indistinguishable from a
  real query. This aligns flat filters with the existing fail-closed policy
  for `?sort=` and `?where=`. Hidden columns are rejected with the identical
  "unknown filter" wording as a nonexistent column, so the error is not a
  hidden-vs-absent oracle. Reserved list controls (`sort`, `page`, `limit`,
  `offset`, `cursor`, `direction`, `where`, `fields`, `include`, `trashed`,
  `stream`, `q`) and nested relation filters (`?author.name=`) are never
  treated as unknown filters.

  A declared column whose name collides with a control word (a field named
  `stream`, `q`, …) still filters: a known field wins over the reserved-word
  skip, so it is never silently swallowed. `?per_page` is accepted as an
  alias for `?limit` (a common REST convention) so it neither 400s nor
  silently serves the default page size.

  **Migration.** An endpoint that reads its own non-column query params (a
  `BeforeList` hook scoping on `?region=`) declares them with
  `EntityConfig.AllowedFilterParams: []string{"region"}` so strict parsing
  skips them without disabling typo protection. To tolerate *arbitrary*
  extra params, restore the old drop-silently behavior per entity with
  `EntityConfig.LenientFilters: true`, or per call with
  `filter.ParseFilters(r, fields, filter.Lenient())`. Prefer the narrow
  options; a dropped filter is a data-exposure hazard. See
  [entity declarations → Flat filters](framework/docs/content/entity-declarations.md).

### Fixed

- **Hidden columns reachable via nested filters** (#100). A `Hidden` column
  on a relation's target entity could be probed through a nested predicate
  (`?author.password_hash_like=…`), resurrecting the same value-disclosure
  oracle the flat-filter Hidden exclusion blocks. Both the HTTP nested path
  and the in-process typed-repo path now reject a hidden target field with
  the identical error as a nonexistent field (non-leaky).
- **`?offset=` now honored on the List endpoint** (#100). The raw
  `?offset=` row-skip control was accepted but silently ignored (the paged
  query always derived its offset from `?page`), so a caller paginating by
  offset, notably the process-module broker, which sends `?offset=` without
  `?page`, silently received page 1. An explicit non-negative `?offset=`
  now overrides the page-derived offset on both the buffered and streaming
  list paths.

## [0.34.0] - 2026-07-18

Makes framework-owned SQL work with the bundled pure-Go SQLite adapter (#91),
adds package targeting to `gofastr build` (#92), and hardens durable scheduler
watermarks and occurrence retention (#96, #97).

### Added

- **`gofastr build --pkg`** (#92). `build` now accepts the same command-package
  target as `gofastr dev`, including both `--pkg value` and `--pkg=value`.
  Unknown flags, missing values, option-like package values, and unexpected
  positional arguments fail before code generation or compilation. Build
  failures identify the selected package target.
- **Bounded durable-scheduler catch-up** (#97).
  `DurableSchedulerConfig.MaxCatchUpOccurrences` limits the occurrence history
  materialized after downtime and defaults to 1,000. Fixed intervals
  fast-forward arithmetically, while cron schedules retain only the newest
  bounded window.

### Changed

- **Bounded scheduler occurrence retention** (#97). Occurrences default to a
  30-day retention window, pruning runs at most hourly after due work is
  committed, and a negative `OccurrenceRetention` disables pruning. New
  `(schedule_id, enqueued_job_id)` and `created_at` indexes keep overlap checks
  and retention sweeps bounded.

### Fixed

- **Bundled pure-Go SQLite framework compatibility** (#91). The adapter now
  supports the framework's numbered placeholders, conflict clauses,
  `RETURNING`, correlated `EXISTS`, and multi-statement parsing. Writes enforce
  primary-key, `NOT NULL`, and unique constraints consistently; failed
  statements roll back atomically, and unique indexes reject existing or
  future duplicate keys.
- **Durable scheduler watermark stalls** (#96). Watermark compare-and-swap now
  uses a monotonic schedule version instead of timestamp equality, so database
  precision or timezone normalization cannot silently stall a schedule.
  Re-registering a schedule also advances the version to fence stale
  definitions, and malformed persisted timestamps return errors rather than
  becoming zero values.

## [0.33.0] - 2026-07-18

Adds MCP Apps support to `core/mcp` (#90) and a durable, replica-safe
scheduler (#94); fixes ScreenGroup sibling navigation under a default layout
(#89) and dropdown Escape focus restoration (#93).

### Added

- **Durable replica-safe scheduler** (#94). `battery/queue` gains a
  `DurableScheduler` that persists schedule watermarks and occurrences in the
  same SQL database as the `DBQueue`, so schedules survive restarts and never
  double-fire across replicas: a heartbeat-expiry leadership lease avoids
  redundant evaluation while unique occurrences + transactionally enqueued
  jobs are the deduplication authority. `DurableSchedulerConfig` sets the
  replica `OwnerID` (defaults to a random process-local ID) and `LeaseDuration`
  (defaults to 30s). See [queue](framework/docs/content/queue.md).
- **MCP Apps in `core/mcp`** (#90). The tools-only server now speaks the
  richer surface MCP Apps and modern clients expect:
  - **Rich tool results.** A `ToolHandler` can return `mcp.ImageResult`
    (an `{type:"image"}` block, base64-encoded; renders inline instead
    of smuggling base64 through a text field), `mcp.ToolResult{Structured, Content}`
    (`structuredContent` + explicit blocks; a structured-only result mirrors
    a text block for plain clients), a `mcp.Content` / `[]mcp.Content`, or a
    plain value (unchanged: JSON-marshaled text). New `TextContent` /
    `ImageContent` / `AudioContent` / `ResourceContent` constructors.
  - **Resources.** `Server.RegisterResource(uri, name, mimeType, contents)`
    serves `resources/list` + `resources/read` (text or base64 blob);
    registering any resource makes `initialize` advertise the `resources`
    capability. `mcp.WithResourceGate(gate)` auth-gates a resource's
    contents (the resource-side analogue of `mcp.Gated`).
  - **Tool / resource `_meta` + `outputSchema`.** `RegisterTool` takes
    options: `mcp.WithToolMeta(...)` (serialized verbatim in `tools/list`,
    the MCP Apps `ui.resourceUri` linkage) and `mcp.WithOutputSchema(...)`;
    `mcp.WithResourceMeta(...)` / `WithResourceDescription(...)` for
    resources.
  - **`framework.WithMCPApp(mcp.AppConfig)`** wires an MCP App, a `ui://`
    HTML widget resource plus the linking tool (with the ChatGPT Apps SDK
    `openai/outputTemplate` compat alias), in one call. Explicit opt-in,
    registered during `InitPlugins`; a duplicate tool name / resource uri is
    a hard build error.

### Fixed

- **ScreenGroup sibling nav under a default layout** (#89). A group
  registered under `SetDefaultLayout` reports its INNER layout name in the
  route manifest but carries the OUTERMOST default layout name in the
  `[data-fui-layout]` shell marker, so `layoutWillChange` always misfired a
  full shell swap and rebuilt the group's persistent chrome (e.g. a tab
  strip) on every sibling click. The runtime now treats a shared
  `data-fui-screen-group` between the two paths as proof of a shared shell
  and does an in-shell content swap. `findCommonScreenGroup` also matches a
  slashless index path (`/studio` inside prefix `/studio/`).
- **`llm.md` dual-registration panic** (#89). A group index aliased at both
  `/studio` and `/studio/` collapsed to one `/studio/llm.md` route and
  panicked on the duplicate registration; the per-screen loop now dedupes
  the collapsed route.
- **Dropdown Escape focus restoration** (#93). Pressing Escape now closes the
  focused (or topmost) open dropdown and returns focus to its trigger only
  when focus was inside the panel, instead of mishandling focus across
  multiple open dropdowns.

## [0.32.0] - 2026-07-18

Ships the API-distribution pair: the customer-facing CLI (#85) and SDK
generation + in-app hosting (#86). A GoFastr app's HTTP API becomes a
branded terminal client its developer distributes to *their* customers,
and downloadable client SDKs (Go, JS/TS) the app itself serves behind a
live docs site.

### Added

- **`gofastr generate cli`** (#85). Run from an app root (entities are
  recovered from project source; no blueprint involved) to emit a
  standalone, stdlib-only `package main` under `cmd/<binary>/` (go-install-
  friendly; cross-compiles anywhere Go does) that imports only the
  app's `entities/client` package. Every selected entity gets
  list/get/create/update/patch/delete with schema-derived filter, sort,
  pagination, `--include`/`--fields`, `-q` (with `SearchFields`) and
  `--trashed` (with `SoftDelete`) flags, atomic `batch-create`/`batch-update`/
  `batch-delete`, and a live `watch` (SSE, one JSON line per event).
  Mutations are presence-faithful: only explicitly-set flags enter the body,
  so `--published=false` really sends `false`. `login`/`logout` store a
  scoped API token (flag > `<BINARY>_URL`/`_TOKEN` env > 0600 config file);
  exit codes are 0/1/2/4 (ok/API error/usage/auth). Selection is declarative
  (`--only`, `--exclude`, `--verbs` global or per-entity) and a typo'd name
  or reserved-flag/command collision fails generation. Regeneration is
  one-shot + `--force`, except `custom.go`, the dev-owned seam whose
  `customCommands()` merge over (and can override) the generated dispatch
  table; the file is only ever created when absent. See
  [app-cli](framework/docs/content/app-cli.md).
- **`auth.RequireAPIScopes(prefix)`**. One mount makes minted token scopes
  real across the whole auto-CRUD tree: the resource is derived from the
  path, GET/HEAD need `<resource>:read`, everything else `<resource>:write`;
  sessions/JWTs and off-prefix paths pass untouched. Without it (or
  per-route `RequireScope`) a token's scope list is advisory only.
- **Generated typed client: full CRUD surface + auth.** The
  `entities/client` package gains an opt-in `Token` field (sent as
  `Authorization: Bearer …`; bearer requests skip both CSRF layers by
  design), `BatchCreate/BatchUpdate/BatchDelete<Entity>` mapping the atomic
  `_batch` routes (a 400 rollback returns the `{committed, results[]}`
  envelope, not an error), `Watch<Entity>` (blocking SSE loop), and a raw
  `Do` escape hatch for custom endpoints and presence-faithful map bodies.
- **Meridian dogfoods the whole path**: TokensPlugin + TokenMiddleware +
  RequireAPIScopes wired in `app.go`, and its generated CLI is committed at
  `examples/meridian/cmd/meridian` so generator drift breaks CI
  (`go install github.com/DonaldMurillo/gofastr/examples/meridian/cmd/meridian@latest`).
- **`gofastr generate sdk`** (#86). Run from an app root (entities are
  recovered from project source, exactly like `generate cli`) to emit
  `gen/sdk/`: a standalone stdlib-only **Go SDK module** (the typed
  client + its own go.mod + README, zipped for download), a
  **zero-dependency JS/TS client**, one handrolled ESM `client.js` plus
  `client.d.ts`, deliberately no npm packaging, and a `dist/` directory
  (sdk-go.zip, client.js, client.d.ts, manifest.json) the app serves.
  Selection via `--only`/`--exclude`; defaults can live in
  `gofastr.codegen.yml` as a generator entry named `sdk`, which also runs
  under `gofastr generate --config` (the first first-party in-process
  codegen generator). Output is generator-owned and regenerates in place;
  archives are deterministic. Hidden fields never appear in any generated
  file. See [sdk](framework/docs/content/sdk.md).
- **`framework/sdkdocs`**: the SDK docs site. `sdkdocs.Mount(site,
  router, cfg)` serves a public site at `/docs/api`: tabbed install
  guides, download routes (ETag revalidation; client.js serves inline so
  browsers can `import` it straight from the URL), an auth guide
  (minting `gfsk_` tokens), an errors reference, and a live per-entity
  API reference rendered from the registry on every request. Fail-closed
  visibility: `Public` entities only by default; gated entities 404
  indistinguishably from missing ones; `Entities`/`IncludeGated` opt in;
  `Policy` gates screens and downloads together. Drift detection
  compares the manifest's schema hash (`framework/sdk.SchemaHash`)
  against the live registry: one WARN plus a page banner, downloads
  keep working.
- **`framework/sdk`**: the shared generator↔server contract: manifest
  schema, deterministic zip packer, and the schema hash both halves
  compute.
- **`ui.CodeTabs`**: the same snippet in several languages behind a
  zero-JS tab strip (patterns/tabs + CodeBlock with copy buttons).
- **`static.Builder.ExtraDirs`**: generic "copy this FS under this URL
  path" hook for static export (never clobbers files the export or the
  user static dir already own); the SDK artifacts ride it.

### Fixed

- **Generated clients no longer leak hidden fields.** `renderClient`
  (the `entities/client` package and therefore the Go SDK) now skips
  `Hidden` columns in all four generated struct walks; hidden schema
  never reaches a downloadable artifact.
- **`--tk-com` syntax-comment color now meets WCAG AA** on the dark code
  surface (#676E95 → #8C93B0, 3.6:1 → 5.8:1; caught by axe on the SDK
  docs pages).
- **`patterns/tabs` panels no longer stretch the page** when panel
  content is wider than the viewport (flex min-width); wide code
  samples scroll inside their own frame on mobile.
- **Widget registry enumeration is now sorted.** With two or more
  registered widgets, the catalog JSON, SSR chrome-inline order, and the
  static export's widget dump followed Go's random map order, which flapped
  export bytes and rotating the PWA cache version on no-op rebuilds.

## [0.31.0] - 2026-07-17

Ships process-isolated third-party modules (#37) and cross-replica presence
(#47), plus the capability/hygiene groundwork they stand on.

### Added

- **Process-isolated third-party modules** (#37). A third-party module runs
  as a separate child process, so the host can install, upgrade, crash, and
  revoke it without touching its own binary. The stdio trust boundary is a
  purpose-built full-duplex protocol (`core/moduleproto`), not MCP. The
  supervisor gives each module a fail-closed lifecycle: a state lease, an
  `Enabled → Ready` two-layer gate (disabled 404 / enabled-but-down
  503+`Retry-After`), a restart circuit keyed to `(module, generation)`,
  concurrent drain under the shared lifecycle budget, and fully-buffered
  responses (a mid-call crash yields a 503, never a truncated 200). The
  capability broker checks every reverse `host.*` data call as
  module-grant ∩ caller-authority through the CRUD chokepoint, derives the
  permission from the trusted method (never a child-supplied string), and
  makes `CrossOwnerRead` non-grantable and stripped on the reverse path. An
  optional `SandboxRunner` (P1–P7 conformance probes; `bwrap`/`sandbox-exec`/
  Job-Object wrapper backends, no new dependencies) is required before an
  untrusted module runs; with no conforming backend it fails closed rather
  than downgrade. Postgres migrations run under a per-module restricted
  schema+role; the module holds zero DB credentials. A module may expose
  `module.<name>.<tool>` MCP tools and return `ui.node.v1` UI trees the host
  validates and renders (`core-ui/uinodev1` + `framework/uihost/uinoderender`);
  the module never emits markup. See
  [process-modules](framework/docs/content/process-modules.md); operators get
  a lifecycle screen at `/admin/modules`.
- **Cross-replica presence** (#47). Presence rosters aggregate across `serve`
  replicas over a dedicated `gofastr.presence` fanout lane (15s heartbeat,
  45s TTL, prompt graceful-leave). `PresenceRoster` returns the merged roster
  and is byte-identical with no fanout configured; announcements carry only
  the server-derived identity already exposed, with no new HTTP surface.
- **`access.ScopeMatch`.** The `resource:verb` wildcard algebra
  (`teams:*`, `*:read`, `*:*`) gets one home in `framework/access`;
  `battery/auth`'s token-scope matcher delegates to it. `access.Can`, the
  exact-match RBAC hot path, is unchanged and never learns wildcards.

### Changed

- **BREAKING (narrow): `mcpclient.Spawn` scrubs the child environment.**
  Spawned MCP-server children no longer inherit the full host environment
  (which leaked `JWT_SECRET`, the DB DSN, and `OAUTH_*`); they get a minimal
  allowlist. Callers that relied on an inherited variable pass it explicitly
  via the new `SpawnConfig{Env, InheritEnv}`.

## [0.30.0] - 2026-07-17

Closes the Nexus fourth-batch tickets (#76, #77, #79, #80, #82, #83, #84)
and ships two new capability surfaces: managed stored routines and
background compute (Web Workers + WebAssembly).

### Added

- **Capability registry + grant validation** (#76). `RolePolicy.Register`
  declares the capabilities the app actually checks; `Capabilities()`
  returns them sorted. With a non-empty registry, an unknown grant emits a
  loud warning naming the grant and its nearest registered capability, and
  `StrictCapabilities()` upgrades that to a typed rejection
  (`*access.UnknownCapabilityError`; the admin grant screen answers 400
  to a typo instead of a generic 500). Resource wildcards (`teams:*`)
  expand at grant time against the registry, so `Can` stays exact-match.
  The admin RBAC screen feeds grant inputs from the registry via a
  datalist and flags persisted grants that match nothing as unknown/dead.
- **`access.NewCachedResolver`** (#79). Wraps a `func(ctx) []string` roles
  resolver with per-user TTL caching (default 30s, `WithTTL`),
  single-flight de-duplication, and `Invalidate`/`InvalidateAll` for
  event-driven invalidation, the caching layer every team-membership
  resolver was hand-rolling. `admin.Config.EffectiveRoles` lets the admin
  users screen show direct ∪ resolved roles labeled by origin.
- **Resource-scoped access decisions** (#80). The `access.Decider` seam
  (`Ref`, `Decision`, `WithDecider`, `DeciderMiddleware`, `CanResource`)
  answers "may this user edit project 42" without a tuple store: the CRUD
  entity gates consult the decider with the record id and fall back to the
  role policy on abstain. Fail-closed; apps without a decider are
  byte-identical. The `resource:id:capability` string convention is
  documented as an app-side alternative.
- **Managed stored routines (Postgres functions/procedures/triggers).**
  `App.RoutinesFS(fs, dir)` loads routines from embedded SQL files:
  `<name>.sql`, `<name>.down.sql`, dialect-scoped `<name>.pg.sql` /
  `<name>.sqlite.sql`, with loud rejection of empty files, collisions,
  and unknown suffixes. `Routine.Dialect` scopes a routine to one engine
  (mismatches skipped with one per-boot log line), so Postgres-only procs
  coexist with a SQLite dev database. A `gofastr_routines` ledger records
  name + checksum + applied_at in the same advisory-locked transaction;
  reporting-only: every Up still runs each boot (idempotent,
  self-healing), orphaned rows warn loudly and are never auto-dropped.
  The `app_routines` MCP introspection tool reports dialect, checksum,
  ledger state, and live `pg_proc`/`pg_views` presence per routine.
- **Background compute: registered Web Workers + WASM modules.**
  `compute.RegisterWorker` / `compute.RegisterWASM` register
  content-addressed assets served immutable at
  `/__gofastr/compute/<name>.js|.wasm`. The `compute` demand module
  (1,020B gzip, `data-fui-compute` trigger) exposes
  `window.__gofastr.compute`: `task(worker, fn, payload)` with a promise
  protocol, 30s timeouts, and worker recycling; `wasmURL(name)` for
  `instantiateStreaming` inside workers; `dispose(worker)`. The page CSP
  is unchanged: worker-script responses carry their own
  `script-src 'self' 'wasm-unsafe-eval'`. Docs include Go/TinyGo-to-WASM
  recipes. SharedArrayBuffer/cross-origin isolation deliberately out of
  scope.
- **Sortable 409 problem details** (#83). A 409 with
  `{"error":{"code","message"}}` is read under hard bounds (JSON
  content-type, ≤4KB, message capped at 300 chars) and announced through
  the polite live region + framework toast before the authoritative
  conflict refresh; malformed, oversized, HTML, or empty bodies keep
  today's generic copy.
- **`ui.LinkButton` Icon slot.** `LinkButtonConfig.Icon` names a registered
  icon (`ui.RegisterIcon`) to render before the label: external-link
  buttons can carry a recognizable mark (GitHub, docs) without bespoke
  anchors. Unknown names fall back to the plain label.
- **`seo.WebApplication`.** Typed JSON-LD for in-browser tools (schema.org
  SoftwareApplication subtype), the right @type for SaaS products and
  online generators, previously missing from core-ui/seo's catalog.
  `NewWebApplication()` defaults `operatingSystem` to "Web"; pair with a
  free Offer for "free online tool" rich results.

### Fixed

- **`sortablelist.Render` accepts empty columns** (#82). An empty Kanban
  column renders the full accessible `<ol>` wrapper, is a valid pointer
  and keyboard drop target, and conflict refresh can reconcile a column
  to zero items. `RenderItems` may return an empty fragment.
- **Same-container sortable commits carry `container=`** (#84) whenever
  the list configures `data-fui-sortable-container`, so one shared board
  endpoint can route every write. Container-less lists and the
  cross-container payload are unchanged.
- **`RolePolicy.Grant` de-duplicates** (#77), so boot-time code grants +
  `GrantStore.LoadInto` no longer show duplicate rows in `PermissionsOf`
  and the admin matrix.
- **Admin RBAC wiring is purely additive** (#77). The generator emits
  auth/policy handles at package level plus an
  `adminBatteryConfigurators` seam: activating the admin battery means
  adding a file, not editing owned-generated ones. Generated repo structs
  now point at their package-level event helpers, the
  refuse-to-overwrite error names `generate --add`, and the generated
  guidance steers toward `core-ui/html` over raw `render.Tag`.
- **Runtime preload markers match attribute boundaries**, so the new
  `data-fui-compute` marker cannot spuriously preload on pages using
  `data-fui-computed`.
- **Flash-free banner dismissal.** A `DismissID` banner was hidden only by
  the runtime's localStorage pass, so it painted for a moment on every
  page load after being dismissed. The runtime now mirrors the dismissal
  into a same-name cookie, and `ui.Banner` skips rendering entirely when
  its `Ctx` carries a request bearing that cookie.

### Changed

- **`RolePolicy.Grant` returns `error`.** Statement-form callers keep
  compiling; strict-mode and store callers get one consistent rejection
  contract.
- **The access docs say loudly that `EntityConfig.Access` is HTTP-only**
  (#77): in-process repo/CrudHandler calls bypass entity gates by design,
  so SSR per-row rules belong in hooks or handler checks.
- **`ui.ColorPicker` renders the swatch before its label**: control on
  the left, name on the right, matching Checkbox's reading order (it
  previously rendered label-first).

## [0.29.0] - 2026-07-16

### Added

- **Configurable security headers.** `AppConfig.SecurityHeaders` (and the
  `framework.WithSecurityHeaders(cfg)` option) configure the defensive
  headers emitted by the default middleware chain, so an app can relax a
  single directive (e.g. `style-src 'unsafe-inline'`) without shadowing
  the whole chain with a hand-rolled `SecurityHeaders` middleware. Unset
  fields keep their strict built-in defaults; the zero value reproduces
  the previous behaviour exactly.

- Auto-CRUD now mounts `PATCH /<entity>/{id}` for sparse updates. PATCH shares
  PUT's access, owner/tenant scoping, hooks, audit, transaction, and validation
  path while validating and changing only fields present in the request body.
  OpenAPI, MCP update tools, generated typed clients, and entity `llm.md` expose
  the verb too.

### Changed

- **BREAKING:** successful single-record CRUD responses (create, get, PUT, and
  PATCH) now consistently use `{"data": {...}}`, matching list's
  `{"data": [...]}` envelope. Errors and DELETE responses are unchanged.

- **BREAKING: auto-CRUD requires an authenticated session by default.**
  An entity declaring none of `OwnerField`, `Access`, or the new
  `Public` had ZERO enforcement: every operation (List/Get/Create/
  Update/Delete) was reachable by an anonymous caller; an unauthenticated
  `POST /api/<entity>` returned 201 and persisted the row (#65). Entity
  MCP tools inherited the same gap since they dispatch through the same
  router. `framework/crud`'s `requireScope` chokepoint now requires an
  authenticated session (`core/handler.GetUser`) for every operation
  unless an explicit mechanism already governs the entity: `OwnerField`
  or a declared `Access` block (unchanged, "as today"), or the new
  `EntityConfig.Public` / blueprint `public: true`, a deliberate, full
  opt-out for genuinely public entities (a contact form, a blog's
  comments). No `mcp.Gated` wiring was needed for entity MCP tools: they
  re-dispatch through the router and inherit the REST fix for free.
  `gofastr generate` now prints a warning listing every entity left
  publicly readable/writable (`public: true`), and the existing unscoped-
  entity lint's message was corrected: it no longer claims anonymous
  exposure (that gap is now closed); it flags the narrower cross-user
  ("every authenticated user can read every row") exposure instead.
  Existing apps with entities that declare neither `OwnerField` nor
  `Access` will see those entities start 401ing anonymous requests;
  add `public: true` for entities that are genuinely meant to be open,
  or a real `access:`/`OwnerField` for the ones that aren't.
  `framework.TestApp` (the in-memory test harness) gained
  `AsUser(user any)` to authenticate test requests under the new
  default. See [entity-declarations](framework/docs/content/entity-declarations.md)
  → "Default CRUD authentication" and
  [security](framework/docs/content/security.md) → "Default CRUD
  authentication".

### Fixed

- **Eager loading / `?include=` no longer fails on nullable foreign keys.**
  `BelongsTo`/`HasOne` relations over a nullable FK column (e.g.
  `work_items.milestone_id`, `assignee_id`) returned
  `sql: Scan error … converting NULL to string is unsupported` and failed
  the whole eager load (and the request). The BelongsTo loaders in both
  the `EagerLoad` helper and the live include path now scan the FK into
  `sql.NullString`, so a `NULL` FK yields the parent row with the relation
  absent/`null` instead of erroring.
- **Generated `e2e_test.go` is Windows- and Postgres-portable** (issue #68).
  The blueprint generator's end-to-end test template had two portability
  defects. (1) It built the binary to a bare `app` and exec'd it; on Windows
  that name has no `.exe` suffix, so the child can't start. The template now
  appends `.exe` when `runtime.GOOS == "windows"`. (2) It always booted the
  child with `DATABASE_URL=file:e2e.db` (a SQLite DSN), but a
  `db.driver: postgres` blueprint links only `lib/pq`, which cannot open a
  SQLite file; the server never became ready and the test timed out with a
  misleading message. The template now bootstraps from the blueprint's
  declared driver: SQLite/empty drivers still use a throwaway file DB; a
  postgres blueprint carves a disposable database from the env-provided
  `TEST_POSTGRES_DSN` admin DSN and `t.Skip`s when Postgres is unreachable, so
  driverless CI stays green-by-skip.
- **Pre-image casing contract documented; typed/snake accessors added
  (#69).** `crud.AuditPreImageFromContext(ctx)` keys the pre-update row by
  the handler's `JSONCase` (camelCase by default, e.g. `"statusId"`), not
  the snake_case DB column name every other hook-adjacent surface speaks.
  A hook doing `pre["status_id"]` silently got `nil` back; casing-identical
  keys (`"version"`, `"key"`) happened to work either way, masking the
  miss. Added `crud.AuditPreImageAs[T](ctx) (T, bool)`, which decodes
  through the same casing translation typed hooks already use, and
  `crud.AuditPreImageSnakeFromContext(ctx) map[string]any` for plain
  snake_case map access. The casing contract is now documented on
  `AuditPreImageFromContext`/`WithAuditPreImage` and in
  `framework/docs/content/hooks-and-transactions.md`.

- **Screen router accepts both `:param` and `{param}`.** A UI screen
  registered with the `{param}` brace syntax (the form used by the
  blueprint, REST/entity routers, and the docs) silently never matched:
  no error, just a 404. The core-ui router now normalizes `{param}` to
  `:param` at registration, so both syntaxes work identically. The HTTP
  router's `{param}`-only syntax is unchanged.
- **DevMode no longer locks local tooling out of `/auth/login`.** The
  per-IP login limiter tripped after a handful of rapid logins even with
  `DevMode: true`, blocking screenshot/verification tooling. DevMode now
  relaxes the per-IP login limiter (`RateLimiterConfig.DevMode`);
  production is unchanged and fail-closed. The per-account brute-force
  limiter is deliberately NOT relaxed in dev.

- PostgreSQL auth stores now create native `BOOLEAN` columns for password
  and 2FA flags, convert legacy auth `INTEGER` booleans during schema
  initialization, and accept native Go `bool` writes on a fresh database.
- Generated bootstrap-admin accounts now seed through `App.WithSeed`
  after auto-migration; missing-password, lookup, hash, and insert errors
  fail startup instead of being swallowed.

- **Authenticated accessibility audits report real coverage.** `gofastr audit
  a11y --url` accepts `--email` / `--password`, clicks the app's `/login` form,
  discovers and scans pages in that browser session, and reports `Audited N of
  M discovered pages`. Login redirects and a login-only run fail as incomplete
  instead of producing a misleading clean verdict.
- **Admin CRUD uses the app's fully wired handler.** Admin create/update/delete
  now runs the app hook registry, so `WithAuditLog` records transactional rows
  (including CRUD pre-images) instead of silently seeing `Hooks == nil`. The
  Queue overview/navigation is hidden when no `queue.Browsable` backend exists;
  backed queue browsing and replay remain available.

## [0.28.0] - 2026-07-16

### Added

- **Configurable API-token prefix.** Hosts brand their credentials:
  `TokenSpec.Prefix` at issue time, `TokensPlugin.WithPrefix` for the
  self-service route, `TokenMiddleware`'s `WithTokenPrefix` for recognition.
  A leaked token's prefix now identifies WHICH product leaked it. Default
  stays `gfsk_`; prefixes are validated (lowercase alnum, trailing `_`).
- **`auth.TokenID(ctx)`.** TokenMiddleware now stashes the authenticating
  token's own ID in the request context alongside owner and scopes: one
  owner can hold many tokens, and per-token metering/quotas/audit need to
  attribute the request to the specific credential.
- **Admin token operations.** `SQLAPITokenStore.ListAll` (every token across
  owners) and `RevokeAny` (revoke ignoring owner scoping) for host-built
  admin surfaces. Deliberately NOT on the `APITokenStore` interface, so the
  plugin's self-service routes can never reach them.

### Fixed

- **`ui.Gallery` grids are responsive by default.** `Columns` was a hard
  `repeat(N, 1fr)`; four columns stayed four columns on a phone, crushing
  every tile, and each consumer had to hand-write media queries. `Columns`
  is now a MAXIMUM: tracks are sized for exactly that many columns but
  never shrink below `--ui-gallery-min` (default `9.5rem`, override via a
  class), so `auto-fill` collapses to fewer columns as the container
  narrows. Masonry gets the same contract via `column-width` +
  `column-count`.
- **Session reads try every cookie candidate.** `SessionMiddleware`, the
  `/auth/me` handler, and logout read only the FIRST cookie with the session
  name (`r.Cookie`). A jar can hold several: a stale cookie from an old
  deployment at a more specific `Path`, or another localhost port's cookie.
  Browsers send the most path-specific first, so a dead cookie shadowed a
  live session: login silently failed while a valid cookie sat one position
  later. All session reads now try every candidate, and logout revokes ALL
  of them so a shadowed-but-valid session cannot survive.

## [0.27.1] - 2026-07-16

### Fixed

- **Phantom color tokens resolved theme-side.** framework/ui components
  referenced ten `--color-*` custom properties that no theme ever emitted
  (`--color-muted`, `--color-warn`, `--color-surface-hover`,
  `--color-primary-hover`, `--color-ring`, …). CSS custom properties fail
  soft: each reference silently rendered its hardcoded fallback, constants
  tuned for light themes, so dark themes hit contrast failures like
  `ui-copy-btn:hover` turning near-white under light-gray text. Themes now
  emit a derived-alias block mapping every legacy name onto canonical
  ColorSet tokens (via `var()`/`color-mix()`, so dark-scheme re-declarations
  flow through), CopyButton's hover/copied states use real tokens directly,
  and a framework/ui test fails the build if any component references a
  token that no theme defines.

## [0.27.0] - 2026-07-16

### Added

- **`gofastr dev --pkg` for `cmd/`-layout apps.** The build target is now
  independent of the watch root. Previously `dev` ran `go build .` at `--dir`,
  so an app whose main lives under `cmd/<name>/` had no working invocation:
  from the project root the build failed with `no Go files in <root>`, while
  `--dir ./cmd/<name>` moved the watch root and the server's cwd along with it,
  silently missing edits under `internal/` and resolving relative paths
  (sqlite `db_url`, static dirs) against the command directory, so the app came
  up against the wrong database. Use `gofastr dev --dir . --pkg ./cmd/<name>`:
  the watcher and cwd stay at the project root while only the build target
  moves. `--pkg` defaults to `.`, so the scaffold layout is unaffected.

- **Kiln is current again.** OMP with GLM-5.2 is the turnkey and first-choice
  live driver; the world/tool schemas now cover the current app, entity,
  screen, navigation, and owned-Go scaffold surfaces; pages render through the
  framework UI host and component registry; and `kiln freeze` deterministically
  emits generator-ready `gofastr.yml` plus lossless `world.json`. The removed
  `entities/*.json` graduation path is gone end to end.
- **Blueprint layout blocks.** Screens now preserve and generate the current
  `framework/ui` `stack`, `cluster`, `grid`, and `stat_grid` primitives, with
  semantic spacing/alignment validation and generate→pack recovery. This keeps
  Kiln's typed live composition intact across the owned-Go freeze boundary.
- **Windows embed WAL snapshots.** Snapshot completion now resets the
  append-mode WAL by closing and reopening it with truncation, avoiding the
  Windows `Access is denied` failure from truncating the live append handle.
- **Windows generator and dev-loop parity.** `gofastr dev` now builds a
  per-process `.exe` on Windows, its end-to-end harness kills the watcher tree
  before canceling the parent, codegen's symlink guard handles drive-qualified
  paths, additive generation normalizes platform separators, and generated
  app/extension tests execute platform-native binaries. Fresh-port allocation
  prevents stale dev servers from contaminating later browser tests.
- **Deterministic Meridian scheme captures.** The flagship visual gate now
  waits for Chromium to commit the scheme repaint before taking a from-surface
  screenshot and asserts that the dark marketing band keeps visible heading
  and paragraph contrast in both schemes.
- **`widget.Builder.SSERefresh`.** SSE-triggered screen changes now force the
  normal SPA navigation pipeline for the current URL. The old `SSEReload` name
  remains as a source-compatible alias but no longer performs a hard reload.

### Fixed

- **Freeze fails loudly on YAML-unrepresentable worlds.** The blueprint
  emitter quotes commas, quotes, and brackets wherever they appear (a seed
  value like `"a, b"` no longer re-parses as two items), leads list-item maps
  with an inline scalar list when no scalar key exists, and
  `BlueprintYAML` now errors, naming the offending key, on seed rows or
  props that `core/yaml` cannot round-trip (map-only rows, keys containing
  colons) instead of writing a silently corrupt `gofastr.yml`.
- **Typed kinds render at any depth.** A design-system kind (`card`,
  `stack`, `stat_card`, …) nested inside a semantic leaf container (`div`,
  `form`, table cells) now dispatches through its component instead of
  falling through to core noderender's unknown-kind comment; the `class`
  strip for legacy journals now applies at every depth.
  (`core-ui/noderender` exports the shallow `RenderKind` seam this uses.)
- **Deleting the page being viewed shows the Kiln fallback.** The host
  fallback carries a `<main>`, so the SSE-triggered SPA refresh swaps in its
  content instead of emptying (and caching) a blank content area.
- **`gofastr dev` removes its temp server binary on shutdown**, instead of
  accumulating one pid-suffixed binary per run in the temp dir; the e2e
  harnesses remove it for the processes they SIGKILL.
- **The Kiln landing follows the theme.** The host fallback page now styles
  itself entirely from the `/__gofastr/app.css` tokens (`--color-*`,
  `--font-*`, `--radius-*`) instead of a hardcoded always-dark palette, so it
  honors `set_theme` overrides and the light/dark scheme like every other
  surface; its styles ride inside `<main>` so an SPA-swapped fallback keeps
  its layout. The landing visual gate now captures both schemes.
- The kiln skill's `add_page` example is valid JSON again (gated by a test),
  and the hooks/routes/seeds tools, action kinds, and expression language it
  references are documented in the skill once more.

### Changed

- **`gofastr dev` runs the server in the project directory.** The rebuilt
  binary's working directory is now `--dir`, the same cwd it gets when run
  by hand, so relative paths (sqlite `db_url`, static dir) resolve against
  the project, and the app's worktree-isolation lookup keys off the
  project's location instead of wherever `gofastr dev` was launched. If you
  relied on launch-cwd-relative paths, your sqlite file now lives in the
  project dir.
- **Kiln defaults its live REST surface to `/api`.** Entity CRUD and HTML
  screens can share a name (`/api/posts` and `/posts`), matching current
  blueprints. Agent-authored page trees reject `class`, `style`, and `on*`
  props and compose the shared design system instead. Previously advertised
  native-agent meta tools (`set_theme`, reject, reset) are now actually
  dispatchable, and the new `set_scaffold` tool authors nav/stubs.

## [0.26.1] - 2026-07-15

Repo-hygiene patch: process ledgers become enforced gates, and the docs
that remain are current. No framework API changes.

### Added

- **`repolint` bans the process-artifact genre.** Two new rules:
  `root-markdown` (root-level `.md` must be one of the GitHub
  community-health files + `ROADMAP.md` + `CLAUDE.md`/`AGENTS.md`) and
  `process-artifact-markdown` (SCREAMING_SNAKE ledger names: AUDIT,
  FINDINGS, NOTES, JOURNAL, HANDOFF, LEDGER; these are rejected anywhere in the
  tree). Rationale for judgment calls lives in commit messages and
  comments beside the tests that enforce them, not in ledger files.
- **The scaffolded host skill covers the v0.26 surface**:
  `uihost.WithAppIcon`, the SEO options + `ScreenSEO`/`ScreenSchema`,
  `gofastr audit a11y` + the enforced build lint, and the
  `gofastr upgrade` flow, with matching trigger phrases.
- The `gofastr-docs` skill's change→doc checklist gained a
  release/BREAKING section (CHANGELOG + `upgrades.yml` `through` bump +
  SECURITY.md supported-versions + host-skill sync).

### Changed

- **`ROADMAP.md` trimmed 57KB → 11KB**: shipped sections deleted per
  the file's own rule; only genuinely-unbuilt work remains. Inbound
  section references across the architecture docs and tests were
  rewired.
- **`perf-results.md` re-measured against v0.26.0**: all 12 hot-path
  benchmarks re-run (Postgres via testcontainers); rewritten as a
  self-contained results doc with a "reading these numbers" section.
- The embedded kiln skill no longer triggers on "GoFastr" alone; it
  requires explicit Kiln signals, so framework-direct users aren't
  mis-routed into HTTP IR mutations.

### Removed

- **~600KB of point-in-time process markdown**, all preserved in git
  history: `references/` research dumps, `docs/` (audit handoff +
  website brief), `SECURITY_FINDINGS.md` + its ledger gate test (every
  row was re-verified fixed 2026-06-10; `SECURITY.md` now points at git
  history), `COVERAGE_NOTES.md` (floors + rationale live in
  `scripts/coverage-floors.sh`), `AI_TEST_AUDIT.md` (the pinning
  `*_security_test.go` files are the record), the embedded
  `agent-notes.md` dev diary and `project-architecture-review.md` risk
  register (its enforceable content already exists as CI gates), and
  `examples/ecommerce/BUILD_JOURNAL.md`.
- Two permanently-skipped contradiction tests in `battery/embed`,
  replaced by a comment beside the tests that actually carry the auth
  contract.

## [0.26.0] - 2026-07-15

Technical SEO and ADA compliance become first-class: static export now
ships the full crawler contract, one image becomes the whole favicon/app
icon surface, and accessibility moves from "tests some examples run" to a
guided `gofastr audit a11y` command with an enforced (escape-hatched)
`gofastr build` gate. Upgrades get the same funnel treatment (#62): a
documented workflow plus `gofastr upgrade`, which reads a migration
registry embedded in the CLI and points at the exact lines in your app a
breaking release touches. The whole batch was hardened by a dual external
review (nine findings, all fixed pre-release; headline: the a11y lint
honors the ARIA escape hatch, so documented icon-only buttons pass).

### Added

- `gofastr upgrade`: guided release upgrades (#62). The CLI embeds a
  migration registry (`cmd/gofastr/upgrades.yml`, one entry per release
  with migration-relevant changes, complete through a `through` marker
  a tripwire test pins to the CHANGELOG's latest release). The command
  reads the project's `go.mod`, resolves the target (`--to vX.Y.Z` or
  the newest tag via the module proxy), prints every note the project
  crosses, with per-note regex detectors pointing at the affected
  `file:line` in the app, and `--apply` runs the mechanical
  `go get` / `go mod tidy` / build / test steps, stopping at the first
  failure. Warns when the target is newer than the binary's registry.
- Upgrade documentation (#62): a version-independent **Updating
  GoFastr** section in the README and a full `upgrading` docs topic
  (`gofastr docs upgrading`) covering the module + CLI split, release
  notes first, MVS version confirmation, and go.mod/go.sum review.
- `gofastr <cmd> --help` now reaches subcommands that implement their
  own help (`audit`, `upgrade`, `docs`); other commands keep the global
  interception so a side-effectful `dev --help` can't start a server.
- `uihost.WithAppIcon(source)`: derives the entire icon surface from one
  image: 32/180/192/512px PNGs under `/__gofastr/icons/`, `/favicon.ico`,
  the `rel="icon"` + `apple-touch-icon` head links, the PWA manifest
  192/512 icons when `PWAConfig.Icons` is empty, and the same files in
  static export. Non-square sources are center-cropped; undecodable
  sources warn and skip.
- `image.NewGradient(w, h, from, to)`: generated placeholder imagery
  (diagonal #RRGGBB gradient) so apps ship an icon without committing
  binary assets; blueprint-generated apps use it for their default icon.
- `uihost.ScreenRobots` per-screen interface and `uihost.WithRobotsMeta`
  sitewide option, `<meta name="robots">` parity with the other
  per-concern SEO interfaces (previously bundle-only).
- Static export writes `sitemap.xml` and `robots.txt` when
  `WithSitemap`/`WithRobots` are configured, same bytes as the live
  handlers (new `UIHost.SitemapXML`/`UIHost.RobotsTXT` single source),
  with `--export-base` folded into `<loc>` entries and the derived
  `Sitemap:` line; user-supplied files in the static dir win.
- `gofastr audit a11y`: guided static accessibility lint: flags missing
  required a11y fields on core-ui/html element configs (Alt, Label,
  Legend, For, …) with a teach-the-rule fix hint per finding; exits 1 on
  findings. `--url <base>` mode runs the vendored axe-core engine via
  headless Chrome against a running app under BOTH color schemes, with
  pages discovered from `/sitemap.xml` (or `--pages`).
- `gofastr build` now enforces the static accessibility lint between
  `go vet` and compilation (guided failure output; `--no-a11y` skips).
  The lint honors the ARIA escape hatch, an `ExtraAttrs` literal with
  `aria-label`/`aria-labelledby`/`role` satisfies the matching typed
  field, and non-literal `ExtraAttrs` fails open (runtime validation
  still backstops it), so the documented icon-only button form passes.
- `check.LintA11yFile`: a11y-only linter entry point that works on any
  .go file (import-alias aware, no false positives on non-core-ui/html
  calls), backing both the audit command and the build gate.
- Blueprint-generated apps ship `uihost.WithAppIcon` (gradient derived
  from the theme's primary color) and a protective default `robots.txt`
  (`Disallow: /__gofastr/`).
- Docs: new `seo` and `accessibility` topics; static-export and PWA docs
  updated for the new surfaces. Meridian and examples/site demonstrate the
  SEO stack (sitewide OG/description, per-screen `ScreenSEO` bundle with
  Product JSON-LD, Organization/WebSite schema, sitemap + robots, icons).

## [0.25.0] - 2026-07-15

The MCP surface gets the funnel treatment (#61): the dev loop implies the
full agent toolkit ("livereload for agents"), generated apps ship the
complete MCP contract, mutating control and log debug tools stay
fail-closed outside dev, custom tools gain first-class auth gating, and
the guidance in skills, agents.md, and the embedded docs is pinned to the code
by tripwire tests.

### Added

- **`gofastr dev` is livereload for agents: the MCP surface auto-enables
  in the dev loop.** Under `GOFASTR_DEV` (set by `gofastr dev`),
  `framework.NewApp` auto-mounts `/mcp` and enables the read-only
  introspection tools AND the new mutating control tools with zero
  options; battery/log auto-enables its `log_recent` / `log_filter` /
  `log_metrics` / `log_set_level` debug tools the same way; and every
  CRUD-enabled entity serves its MCP data tools without per-entity
  `mcp: true` (entities with `crud: false`, like the auth battery's
  user/session configs, are never implied: no routes, no tools). Opt
  out with `GOFASTR_DEV_MCP=0` (mirrors `GOFASTR_DEV_LIVERELOAD=0`); a
  production `GOFASTR_ENV` always wins, and production processes never
  see `GOFASTR_DEV`. A dev-implied mount yields (warn, not panic) to a
  hand-wired `/mcp` route and dev-implied tool registration tolerates
  name collisions, so existing apps can't be broken by running under
  `gofastr dev`.
- **`framework.WithMCPControl()`: runtime control over MCP.** The
  mutating counterpart to `WithMCPIntrospection`: `app_module_enable` /
  `app_module_disable` toggle registered modules on the running app
  through the module store (dependency-checked, fail-closed), for
  `/mcp` endpoints reachable only by trusted callers. Code-level change
  stays the `gofastr dev` rebuild loop's job; MCP control mutates
  runtime state the app already models.
- **Blueprint-generated apps ship the debug loop.** The generated
  `main.go` registers battery/log (canonical zero config: per-app file
  sink, access log, panic recovery, dev console), so under `gofastr
  dev` a generated app answers "recent requests / current errors /
  trace this request_id" and accepts module toggles over `/mcp` out of
  the box, while a production boot exposes none of it. The MCP e2e gate
  now pins both halves (dev boot has entity + introspection + log +
  control tools; prod boot refuses the mutating/debug set).
- **Auth gating for custom MCP tools.** `mcp.Gated(gate, handler)`
  wraps any directly registered tool handler with a per-caller
  precondition, and battery/auth ships the gates: `auth.MCPUser()`
  (any signed-in caller) and `auth.MCPRole("admin", …)`. The `/mcp`
  route runs under the app's global middleware chain, so the session /
  JWT middleware resolves the caller before the tool executes; the
  gate reads the same identity `RequireRole` does. Entity CRUD tools
  never needed this (they re-dispatch through the router and inherit
  HTTP auth + owner scoping + RBAC); `Gated` covers the direct
  registrations that bypass route middleware: `app.MCP.RegisterTool`
  handlers and `Endpoint.MCPHandler` twins.
- **UI-quality eval: MCP funnel signals.** Each candidate now records
  whether the builder touched `/mcp` during the build
  (`builder_used_mcp`), and the served candidate is probed for its MCP
  surface (`candidate_mcp_tools`, `candidate_mcp_introspection`) plus a
  fail-closed check that dev-only log tools did not leak into the prod
  boot (`candidate_mcp_log_tools_prod`).

- **Blueprint-generated apps are MCP-complete out of the box.** The
  generated `main.go` now wires `framework.WithMCP()` +
  `framework.WithMCPIntrospection()` instead of hand-mounting a
  POST-only `/mcp`: generated apps gain the GET SSE half of the
  Streamable HTTP transport, the MCP discovery endpoints
  (`/mcp/server-card`, `/.well-known/mcp/server-card.json`,
  `/.well-known/mcp/catalog.json`), and the nine read-only
  introspection tools (`app_routes`, `app_readiness`,
  `framework_docs_search`, …) alongside the per-entity CRUD tools. A
  new e2e gate (`TestE2E_MCP_BlueprintApp`) generates, builds, boots,
  and drives the whole contract over real JSON-RPC.
- **Introspection guidance is pinned to the live tool set.**
  `TestIntrospectionGuidanceNamesEveryTool` fails whenever a tool
  registered by `WithMCPIntrospection` is missing from
  `framework/agents.md`, the `agent-ready` doc, or the app-introspect /
  mcp-debug skills: the "five tools" drift (four tools had shipped
  undocumented) is fixed and can't silently recur.

### Fixed

- MCP guidance accuracy sweep: the app-introspect skill no longer
  claims `app_readiness` returns per-check `error` text under
  `WithVerboseReadiness()` (the tool always redacts it; `/readyz` and
  `/mcp` can sit on different trust boundaries), documents the
  zero-checks `reason` field, and the skills' `go run ./examples/site`
  instructions now point at the right port (:8083; :8082 is
  dev-watch). The mcp-debug skill's wiring snippet uses `WithMCP()`
  (matching `examples/site`) instead of a manual mount that would panic
  alongside it.
- `examples/site` registers a real readiness check (`docs-embed`), so
  `app_readiness` on the flagship reports `ready:true` instead of the
  unconfirmed `"no readiness checks registered"` state.
- Shipped-guidance relevance sweep: five battery `agents.md` snippets
  had drifted into wouldn't-compile territory:
  `email.SMTPConfig{TLS:…}` (field is `UseTLS`), `cache.Set/Get`
  missing the `ttl`/`dest` arguments, `webhook.Manager.Stop()` without
  its context, `setup.AdminStep` used as a one-return value, and
  `admin` guidance calling a nonexistent `User.HasRole`, plus the
  host skill's two-arg `testkit.NewIsolatedDB` and stale file paths in
  the gofastr-docs skill. The agents.md snippet gate now also
  validates struct-literal field names against the real structs, so
  the `TLS:`-class drift fails CI.

## [0.24.0] - 2026-07-15

Dev-experience overhaul + static site as an app (#59): hot reload reaches
every HTML surface and the guidance funnels to `gofastr dev`; HMR-readiness
is gated deterministically and measured non-deterministically in the
UI-quality eval; a 77-surface documentation-accuracy sweep (including
`gofastr migrate` honoring an exported `DATABASE_URL`); and static exports
with `WithPWA` become fully offline-capable installable apps.

### Added

- **Hot reload now reaches every HTML surface.** `framework.NewApp` mounts
  dev-only middleware (active only under `gofastr dev`'s `GOFASTR_DEV=1`)
  that splices the livereload client into full HTML documents: responses
  declaring `Content-Type: text/html` that contain `</body>` and don't
  already carry the tag. `static.Handler` file serving (the SPA and
  static-site shapes), widget-server pages, and hand-rolled handlers that
  set the type now auto-refresh like uihost screens. Fragments (island
  swaps, SPA-nav partials), compressed bodies, HEAD/Range requests,
  WebSocket upgrades, JSON/SSE/streams, and flushed progressive HTML all
  pass through untouched.
- **HMR-readiness evals.** An examples sweep boots every runnable example
  under `GOFASTR_DEV=1` and asserts the livereload endpoint plus client
  injection; a blueprint dev-loop e2e generates an app, runs it under
  `gofastr dev`, edits a screen, and asserts the rebuilt content serves; a
  docs tripwire pins that the dev-loop docs lead with `gofastr dev`.
- **Static site as an app: full-site offline PWA for static exports.**
  A static export with `uihost.WithPWA` now emits a full-site service
  worker (`PWAStaticServiceWorkerJS`): the exported page set is closed and
  immutable, so the whole site, pages, widget chrome, `llm.md`, component
  stylesheets under their versioned `?v=` URLs, is precached at install
  and navigations are cache-first (trailing-slash tolerant): land once,
  install the app, and the whole site works offline, including
  never-visited pages. User static-dir files precache best-effort so one
  un-servable file cannot brick the install; the cache version
  fingerprints the exported tree (unchanged rebuilds stay byte-identical);
  static caches use their own `gofastr-pwa-static-…` prefix so live and
  static deployments on one origin never delete each other's caches; and
  a user-supplied manifest or worker in the static dir wins over the
  generated one. The live app's conservative network-first worker is
  unchanged and deny rules apply to both. Proven by a Chrome e2e:
  install, kill the server, navigate to a never-visited page, get its
  real content.
- **Non-deterministic dev-loop funnel signal in the UI-quality eval.**
  Builders now get the snapshot's own `gofastr` CLI first on PATH via a
  logging shim (previously `gofastr docs`/`gofastr dev` depended on a
  possibly version-mismatched global install), the builder prompt stays
  neutral about dev commands, and each candidate records whether the blind
  builder discovered `gofastr dev` from the generated guidance alone,
  surfaced in `result.json` and the leaderboard as funnel telemetry.

### Changed

- **`gofastr migrate` honors an exported `DATABASE_URL`.** The docs always
  promised the 12-factor pattern, but only `--db-url=` and a `.env` file
  worked; the process environment now sits between them (flag > env > .env,
  matching the framework's dotenv precedence).
- **Documentation-accuracy sweep** (77 guidance surfaces audited against the
  code): the audit-log example now shows the real chained
  `app.WithAuditLog(cfg)` wiring instead of a NewApp option that never
  compiled (framework agents file + host skill); the ecommerce example's
  README pointed at a `gen/` directory that hasn't existed since it moved to
  `output_dir: app`; the notify battery no longer advertises a nonexistent
  `WebhookChannel` (signed outbound webhooks are `battery/webhook`);
  `pack` is no longer marked *(future)* in the architecture doc; the
  migrations quick-reference shows the required `--from=`; and the harness
  docs (and one error string) now say plainly that `verify-ack`,
  `conformance`, `ack`, `token`, and `--auth-token-file` are specified but
  not yet wired into the CLI.
- **The development loop funnels to `gofastr dev`.** `go run .` never
  hot-reloads (only `gofastr dev` sets `GOFASTR_DEV=1`), yet the
  highest-traffic guidance led with it. `ui-getting-started`,
  `tutorial-blueprint-app`, `project-structure`, the README quickstart, the
  post-`generate` next-steps output, the generated AGENTS.md trigger row,
  and the UI-eval builder prompt now lead with `gofastr dev` for iteration,
  keeping `go run .`/`go build` for one-shot runs and production.

## [0.23.0] - 2026-07-14

AI-native UI composition (#57): framework-owned operational components,
generated apps that ship zero bespoke CSS with an app-owned `DESIGN.md`, an
adaptive-by-default canonical theme (**BREAKING**: see below), and a
framework-snapshot UI-quality evaluation harness.

### Added

- **Framework-owned operational composition.** `ui.RecordSummary` provides one
  compact dominant record/event summary with bounded status, next-decision,
  metrics, support, ownership, and natural-width action slots. Its optional
  `Aside` fills a purposeful wide-screen rail, while `Actions` stays in the
  lead region and ahead of support context on phones; `ui.MetricBand` renders
  one to six related signals as a flat semantic row that becomes two columns
  on phones. `SiteHeaderConfig.MobileBrand` now swaps long desktop identities
  for a concise phone mark/name without app CSS.
- **Natural UI composition guidance.** `gofastr init` now creates an app-owned
  `DESIGN.md`, points generated agent onboarding at it, and embeds a
  `ui-composition-recipes` reference for product-specific desktop/mobile page
  structures composed from `framework/ui`.
- **Framework-snapshot UI evaluation.** The UI-quality harness can use Codex,
  OMP / GLM-5.2, or Claude Code / Opus builders and Codex or Claude visual
  judges, with role-specific provenance retained in run manifests.

### Changed

- **BREAKING: `theme.Default()` is now adaptive.** The canonical framework
  theme carries a complete contrast-safe `DarkColors` palette, so every app
  mounting `theme.Default()`, new or existing, follows the OS dark
  preference and `ThemeToggle` works without host setup. An existing app whose
  own CSS assumes light tokens should either audit its surfaces in dark mode
  or opt out explicitly (`t := theme.Default(); t.DarkColors = nil`).
  `gofastr theme init` emits the same palette (generated from the canonical
  map, not a copy), and `theme.Overrides.DarkColors` lets a small brand reskin
  provide explicit dark values instead of silently reverting to the canonical
  palette. Forced-theme browser gates synchronize both the HTML attribute and
  the native color-scheme meta.
- **BREAKING: `SiteHeader` wraps its Brand slot.** Brand (and the new
  `MobileBrand`) render inside a `.ui-site-header__brand` wrapper div, so host
  CSS or tests selecting the brand as a direct child of the header must adjust
  one level. The framework now ships typography defaults for a linked brand
  (replacing browser-default blue underlines) at **zero specificity** via
  `:where()`, so any consumer rule still wins, preserving the "consumer owns
  visual identity" contract.
- **Responsive and touch-target hardening.** `ui.DocLayout` now shrinks inside
  flex/grid parents instead of preserving a desktop min-content width on
  phones, and `ui.ValidationSummary` field links meet the WCAG 2.2 24px target
  floor. Button wrapping is container-driven: `flex: 0 0 auto` sizes each
  control to its unwrapped label so action rows wrap whole controls first,
  while a label wider than its own container, a sidebar rail or card cell at
  any viewport, not just a phone, wraps inside the bounded button instead of
  clipping. `ui.AvatarGroup` now uses a 10% overlap that keeps initials
  readable, an adaptive overflow chip, and compact corner presence dots.
- **The interactive set reads both theming surfaces.** Counter, Tabs, Toggle,
  Dropdown, and Collapsible chain every legacy `--fui-*` bridge read to its
  canonical token, `var(--fui-border, var(--color-border, …))`, so the
  adaptive theme reaches them with no host aliases while existing `--fui-*`
  host overrides keep winning. Collapsible also gains an opaque
  `--color-surface` background to hold contrast on tinted panels.
- **Balanced phone signal bands.** Odd three- and five-item `ui.MetricBand`
  sets now make their final signal span the phone row instead of rendering an
  accidental empty quadrant, and a single-item band no longer paints a stray
  column divider.
- **BREAKING: `ui.Cluster` zero value now wraps.** Clusters wrap whole
  controls by default; `ClusterConfig.NoWrap` is the explicit opt-out for
  compact chrome. The old boolean `Wrap` field remains source-compatible but
  is now ignored; it is deprecated because its zero value could not represent
  the documented wrapping default. A caller that relied on `ClusterConfig{}`
  or `Wrap: false` rendering nowrap must set `NoWrap: true`.
- **The init scaffold emits zero app-owned CSS.** The generated
  `screens/styles.go` and `WithCustomCSS` wiring are removed; the starter page
  now composes framework UI primitives. Reinit preserves `DESIGN.md` even with
  `--force`.
- **UI evaluation variants now represent framework roots, not injected design
  prompts.** Generated onboarding is fingerprinted and must remain untouched;
  historical prompt-treatment scores are explicitly invalid for framework
  quality claims.
- **UI evaluation runs fail closed on contamination.** Exclusive run creation
  and locks prevent mixed concurrent artifacts; reuse rejects linked run
  directories; candidate and framework fingerprints are rechecked around
  mutating gates; result JSON is atomically replaced; agent and candidate
  environments omit unrelated credentials; Windows jobs and Unix process
  groups own descendants; capture blocks non-candidate network requests and
  broken images; and visual judges treat screenshot text as untrusted output.

### Fixed

- **Eval-runner hardening (post-review).** The OMP stream is drained through a
  line-buffered `Stdout` writer instead of a `StdoutPipe` raced against
  `Wait`, so a successful build can no longer lose its final `message_end`
  event (and an oversized final message no longer fails extraction); switching
  an agent to the codex backend clears the previous backend's model and
  demands an explicit `--*-model` instead of launching `codex --model opus`;
  candidate gates pin the runner's resolved `GOMODCACHE` so the isolated home
  no longer forces a full re-download of the dependency graph `go mod tidy`
  just warmed; judge evidence is always copied rather than hard-linked so one
  judge process cannot corrupt the panel's shared pixels; and a
  machine-specific `NODE_EXTRA_CA_CERTS` fallback path was removed (export the
  variable instead). The five capture tests that drive a real headless Chrome
  are now gated behind `-short` like the other browser suites, and
  `test-all.sh` retries their contention deadline signature serially.
- **Generated home screen keeps prose out of the shell block.** The sample
  entity hint is part of the Section description; the `CodeBlock` contains
  only runnable commands.

## [0.22.0] - 2026-07-14

First-class installable-PWA support and blueprint-generated LLM page
documentation (#54).

### Added

- **Installable PWA (`uihost.WithPWA`)**: one opt-in option turns a UIHost
  app into an installable Progressive Web App: a typed web app manifest at
  `/manifest.webmanifest` (emitted via `encoding/json`, defaults derived at
  serve time: name from the app title, `/` start URL/scope, standalone
  display), a generated service worker at `/service-worker.js`, a CSP-safe
  external registration script, and an offline fallback screen at
  `/__gofastr/pwa/offline` (framework default or `PWAConfig.OfflineScreen`;
  deliberately not wrapped in the app layout; it is precached, so nothing
  personalized may render into it). The worker is conservative by
  construction: document navigations are **network-first and never cached**
  (rendered HTML can be personalized), falling back to the precached offline
  screen; Cache Storage only ever holds the versioned app shell (runtime,
  split modules under their content-addressed `?v=` URLs, `app.css`, the
  offline page + its per-component stylesheets, icons, declared `Precache`
  extras), matched **exactly**: content-addressed URLs are cache-first
  (immutable), everything else is network-first so post-deploy HTML never
  pairs with the previous deployment's runtime/CSS. Sensitive endpoints
  (SSE, session, signal, action, widgets, `/api`, `/auth`, plus
  `PWAConfig.DenyPaths` for custom mounts) can never be precached and are
  never intercepted. Cache names version deterministically: the fingerprint
  includes the bytes of static-served precache entries, so swapping an icon
  in place rotates the cache, and activation deletes only obsolete caches
  owned by the app. Updates never force a reload: no `skipWaiting`; a
  waiting worker dispatches `gofastr:pwa-update` on `window`. Precached
  responses are re-wrapped at install time so a static host's redirects
  can't poison the offline fallback. Verified end-to-end by a serialized
  Chrome test (registration, installability metadata, offline fallback
  against a dead server, v1→v2 cache-version cleanup). See `gofastr docs
  pwa`.
- **Static export emits the PWA surface**: `ExportStatic` writes the
  manifest, service worker, registration script, and offline page; under a
  non-root `BasePath` the manifest URLs, the worker's precache/deny lists,
  and the registration target are all prefixed, so the exported app installs
  and works offline from a subpath (GitHub Pages project sites included).
- **Blueprint `app.pwa`**: `gofastr generate` scaffolds the full surface
  from one block, including replaceable 192px/512px/maskable placeholder
  icons (deterministic in-process PNGs colored from `theme_color`, falling
  back to the theme's `primary` token) so a generated app is installable
  immediately. A custom `api_prefix` or auth `base_path` flows into
  `DenyPaths` automatically. Round-trips through `pack`.
- **Blueprint `app.llm_md`**: emits `uihost.WithPublicLLMMD()` so every
  registered screen serves its `/llm.md` document plus the `/llm-pages.md`
  index; app-level and per-screen `NoLLMMD` opt-outs keep working.
  Independent of `app.pwa` by design; both default off, and existing
  blueprints generate byte-identical output.

## [0.21.0] - 2026-07-13

Multi-replica auth durability + a hardened third-party plugin isolation
boundary. **BREAKING:** production mode refuses the in-memory 2FA store
(see below).

### Added

- **Heavy-JS plugin platform (`framework/pluginhost`)**: the client mirror
  of the process-isolation track (#37). Hosts third-party-grade JS plugins
  in a sandboxed **opaque-origin iframe** (`sandbox="allow-scripts"`, never
  `allow-same-origin`) with a versioned postMessage protocol (ready→init
  handshake, capability grants, save/upload/theme/resize events; source
  check by `event.source`, never origin strings). Isolation is enforced by
  the runtime, not by convention: the sandbox derivation
  (`Manifest.SandboxString` + the broker's `sandboxFor`) is **authoritative**;
  it strips `allow-same-origin` and forces `allow-scripts` regardless of
  manifest input, and the framed-asset CSP carries `sandbox allow-scripts`
  so a **top-level** load of the frame document is opaque too, not just an
  embed. The capability gate is **default-deny**: `pluginhost.Allow(ctx,
  granted, required)` is `grant-set ∩ caller-authority` (reusing the
  battery/auth `resource:verb` matcher via the new `auth.ScopeMatch`), so a
  plugin can't exceed its grants even under a session cookie;
  `pluginhost.Guard` is the fail-closed route chokepoint (`403
  E_CAPABILITY_DENIED`). Framed assets validate the request origin before
  interpolating it into the CSP (no header-injected directives) and carry
  `nosniff`; the mount marker drops unsafe attribute names. Ships plugin
  `Manifest` + validating `NewClientModule`, `MountMarker`
  (`data-fui-plugin*` markers; see core-ui/ARCHITECTURE.md attribute
  table), the same-origin `AssetServer`, and the host broker
  (`host/pluginhost.js`, served at its own route; core `runtime.js`
  budgets untouched). Distilled from the proven `gofastr-plugins` wysiwyg
  build; that repo now aliases this package.

- **Shared login rate limiting (`auth.SQLRateLimitStore`)**. Every auth
  limiter (login per-IP / per-account, register, 2FA challenge,
  magic-link, password-reset, email-verification) accepts
  `RateLimiterConfig.Store`, a database-backed attempt ledger so the
  brute-force budget stays `MaxAttempts` total across N replicas (not
  `× N`) and a block on one replica holds on all. One store instance
  backs every limiter (keys namespaced per `Scope`, defaulted by each
  surface); schema self-creates; a store error fails **closed**. The
  in-process limiter remains the zero-config default.
- **Durable 2FA store (`auth.EntityTwoFAStore`)**. The missing sibling of
  `EntitySessionStore`: TOTP secrets, enrollment state, and bcrypt-hashed
  backup codes persist in a database table (SQLite or PostgreSQL) instead
  of process memory, so a restart or a second replica no longer wipes
  enrollment. `TwoFAPlugin.Init` self-migrates the table (no host DDL);
  backup-code consumption is replica-safe via an optimistic
  compare-and-swap (two replicas racing to burn the same code; exactly
  one wins). `TwoFAEntityFields()` exposes the table to the entity system
  with the secret and code hashes `Hidden`.

### Changed

- **BREAKING: production mode refuses the in-memory 2FA store.**
  Previously `DevMode: false` + `TwoFAPlugin` without a configured store
  logged a WARN and booted: a restart then silently reverted every
  enrolled account to password-only auth. A security control that
  quietly stops applying is not warning-grade: Init now fails closed.
  Fixes: set `TwoFAConfig.Store: auth.NewEntityTwoFAStore(db, "auth_twofa")`,
  or acknowledge a deliberate single-node deployment with
  `AuthConfig.AllowInMemoryStores: true` (downgrades the refusal to a
  WARN). DevMode is unaffected.

## [0.20.0] - 2026-07-12

Issues #39, #40, #45, #47, #52.

### Added

- **Nested predicate filters (`?where=<json>`)** (#52). The list endpoint
  accepts a boolean predicate tree, leaves (`{field, op, value}`) and
  AND/OR groups, for filters the flat `?field_op=` params can't express
  (`status = A OR (priority = high AND assignee = me)`). Compiled to one
  parenthesized WHERE clause: every field is schema-validated (Hidden
  rejected), every value bound as a placeholder, depth/node bounds
  fail-closed, and the invariant holds: a user OR-group can never widen past
  the owner/tenant/soft-delete scopes (they stay outer ANDs).
- **Data export / import registry (`App.ExportData` / `App.ImportData`)**
  (#39). Anti-lock-in: dump every entity's rows plus registered battery
  tables (auth, queue) to a portable NDJSON archive + a checksummed
  manifest, and restore it. Reads and writes RAW (preserving original
  ids, timestamps, owner/tenant, hidden columns, soft-deleted rows) so
  referential integrity survives; identifiers are `SafeIdent`-whitelisted,
  values bound, import staged (validate-then-write in one transaction).
  No new dependency.
- **Cross-container (kanban) sortable + version-aware conflict recovery**
  (#45). `core-ui/patterns/sortablelist` gains `data-fui-sortable-group`
  (lists sharing a group accept cross-container drag) +
  `data-fui-sortable-container` (destination column in the commit
  payload), an optional `data-fui-sortable-version` token with a distinct
  409 conflict callback (refetch/reconcile instead of blanket rollback),
  cross-column keyboard moves, and an aria-live announcer. Single-list
  behavior is unchanged.
- **Runtime-editable RBAC (`access.GrantStore`) + admin management
  screens** (#40). A DB-backed grant store that loads role→permission
  grants into the live `RolePolicy` at boot and persists edits, plus
  `battery/admin` screens (behind the admin default-deny gate, audited)
  to manage grants and user→role assignments at runtime. Adds
  `AuthManager.SetUserRoles` / `UserStore.UpdateRoles`. Role and
  permission strings are always bound parameters.
- **Presence foundation (single-replica)** (#47). Live "who's viewing
  this" rosters: an SSE connection joins `?presence=<topic>`, the island
  manager tracks a per-topic roster with **server-derived** identity
  (never a client param; anonymous synthesized from the session), and
  roster changes push live over the existing SSE lane. In-process
  `Manager.PresenceRoster` + a demo composing `ui.AvatarGroup`. There is
  deliberately no ungated HTTP roster endpoint (it would leak identities);
  cross-replica aggregation is future work (#47 tracks it).

## [0.19.0] - 2026-07-12

Issues #41, #42, #46, #47, #48, #49, #50.

### Added

- **Durable pgvector embedding store (`embed.NewPgVector`)** (#42). A
  Postgres+pgvector-backed `embed.Store` so multiple app replicas share
  one vector index instead of each holding an in-process FlatStore.
  Ranking is server-side cosine distance (`<=>`) and matches FlatStore's
  top-K order for the same vectors; it implements `chunkLister` so hybrid
  search composes, and deliberately omits snapshotting (a Postgres table
  IS the durable copy, so pairing it with `Options.Path` fails closed).
  `EnsureSchema` creates the `vector` extension + table, with an
  actionable error when the DB role can't `CREATE EXTENSION`. No new
  dependency: vectors are encoded in pgvector's text format over the
  existing `lib/pq`.
- **Pane-host / split-pane layout primitive (`ui.PaneHost`)** (#50). A
  master-detail shell: an always-visible primary pane plus one or two
  openable side panes (`Secondary` / `Tertiary`) with a declarative
  open/close/swap lifecycle, focus handoff on open and restore on close,
  and a responsive collapse where, below 768px, an open side pane
  becomes a fixed overlay drawer (backdrop, focus trap, scroll lock,
  ESC-to-close). Driven by the `panehost` runtime module +
  `data-fui-pane-*` attributes; `window.__gofastr.openPane` /
  `closePane` / `swapPane` expose programmatic control. Pane content
  loads via the existing RPC→signal(html) rail; pane state is never a
  route.
- **Avatar presence dot (`AvatarConfig.Status`)** (#47). `ui.Avatar` /
  `ui.AvatarGroup` render an optional presence dot (online / away / busy
  / offline) sized as a fraction of the avatar and colored from the
  status tokens, with a ring in the surface color. This is the roster
  *visual*; presence *transport* (binding a user to their live
  connection and aggregating it across replicas) remains app-owned and
  is tracked in #47.
- **Queue lane reservations (`Job.Lane` + `WithDBLaneWorkers` /
  `WithLaneWorkers`)** (#41). A lane is a capacity-reservation tag on a
  job: dedicated workers claim only their lane, shared workers keep
  claiming any lane by priority, so a bulk backfill can no longer starve
  urgent jobs by saturating every worker. DBQueue adds a `lane` column
  (auto-migrated onto pre-existing tables, both dialects) plus a
  `(lane, status, scheduled_at, priority)` index. `RedisQueue` stays
  instance-per-lane via its `queueName`.
- **MemoryQueue honours `Job.Priority`** (#41). Dispatch moved from a
  FIFO channel to a priority heap (`Priority DESC`, enqueue-order
  tiebreak) for dev parity with DBQueue. The pending store is now
  unbounded (`Enqueue` no longer blocks at 1024 queued jobs); the
  dead-letter set stays bounded.
- **SSE connection state (`window.__gofastr.sseStatus`)** (#46). The SSE
  runtime module now maintains `{connected, lastEventAt, retryCount}`
  (one live object, mutated in place) and dispatches a
  `gofastr:sse-status` event on connect/disconnect.
  `NetworkRetryBanner`'s `SSESilenceMs` trigger, previously dead
  because nothing wrote `lastEventAt`, now works, and the banner
  re-probes its health endpoint on SSE reconnect so it can dismiss.
- **Per-user locale switching (`framework.WithLocaleResolver` +
  `i18n.CookieLocale`)** (#48). Locale negotiation accepts resolvers
  consulted before the `X-Locale`/`Accept-Language` headers, so a
  stored preference (cookie/session) wins. Resolver values are
  length/charset-bounded and only accepted when they match a catalog
  locale; a garbage cookie cannot force an unsupported locale.
- `i18nui.TVars(ctx, key, vars)`: translate + interpolate `{name}`
  placeholders on both the catalog and English-default paths (#48).

### Fixed

- **framework/ui components now actually translate** (#48). The i18n
  middleware attached only the locale, never the translator, so every
  component rendered English even with a catalog wired. `WithI18n` now
  bridges the translator onto the request context, and a translator
  miss on a `ui.*` key falls back to the English default instead of
  rendering the raw key. On top of the four previously-wired
  components, all ~30 framework/ui components with user-facing copy
  (DataTable, FilterToolbar, forms, uploads, navigation, a11y labels,
  …) now resolve their default labels through `i18nui`; explicit config
  values always win, and default English output is byte-identical.
- **Dead `--radius-*` token references** (#49). 18 references across 12
  component files used `var(--radius-*)` while the token pipeline emits
  `--radii-*`; those styles silently used hardcoded fallbacks (and
  `Repeater`, with no fallback, lost its border radius entirely). All
  renamed to the emitted `--radii-*` tokens, so theme radius overrides
  now reach every component.

## [0.18.0] - 2026-07-10

Backlog from the 2026-07-10 dual blind cold-start eval (two agents each
built a multi-surface app from the repo alone; every item below was hit
independently or verified against the running builds).

### Added

- **Role-based cross-owner read (`EntityConfig.CrossOwnerRead`).** An
  owner-scoped entity can name an RBAC permission (e.g.
  `"tickets:read:all"`) that lifts owner scoping for READ operations
  only: list/get/count/cursor/stream/includes, HTTP and in-process.
  Checked via `access.Can` at the single owner-scope chokepoint, so it
  is fail-closed (no policy in context ⇒ scoping stays on) and
  spoof-proof (roles enter context only via server-side middleware).
  Writes never widen: update/delete stay owner-scoped and creates still
  stamp the caller. The admin battery's wildcard grant passes any
  permission, so opted-in entities are fully visible in the back office,
  per-entity opt-in, decided by the entity author.
  `owner.AllowCrossOwner` remains the in-process escape hatch.
  Blueprint key: `cross_owner_read`.
- **Free-text search (`EntityConfig.SearchFields` + `?q=`).** The list
  endpoint's `?q=` parameter now searches the declared columns
  server-side: whitespace-tokenized (deduped, capped at
  `filter.MaxSearchTerms`), each token an OR-group of
  `LOWER(col) LIKE` with metacharacters escaped, tokens AND-composed
  with owner/tenant/soft-delete scoping on every path (count, buffered,
  cursor, streaming). ASCII-case-insensitive on every dialect. Hidden
  or non-text columns are rejected at `Define` (value-disclosure
  oracle). In-process parity via `ListOptions.Search`; OpenAPI and the
  MCP list tool document/forward `q`; blueprint key: `search_fields`.
- **SQLite FTS5 search backend (`search.NewSQLiteFTS`).** Durable
  ranked full-text search without Postgres: FTS5 virtual table (porter
  tokenizer), bm25 ranking, prefix matching, FTS5 operators neutralized
  by quoting, `FieldEquals` via allow-listed `json_extract`. Requires
  building with `-tags sqlite_fts5` (schema creation says so when the
  module is missing).
- **`upload.ServeHandler(storage)`.** The download half that uploads.md
  always claimed existed: GET/HEAD, sniffed content type, `nosniff`
  always, HTML/SVG neutralized to `application/octet-stream` +
  attachment. Traversal stays enforced in the storage backends, now
  classified via `upload.ErrInvalidKey` (400) vs `ErrNotFound` (404).
- **`Router.MethodNotAllowed` + uihost fall-through.** Registering a
  non-GET route at a screen path no longer shadows the screen with a
  bare text 405: the uihost delegates GET/HEAD to the screen render
  when one resolves, and renders a styled 405 page (Allow header
  preserved, gated-method 404 semantics unchanged) otherwise.
- **`crud.ValidationError` (+ `framework.ValidationError`).** Exported
  with `Fields() map[string][]string` and `NewValidationError`, so
  in-process callers can branch on per-field messages with `errors.As`.
  HTTP wire shape unchanged.
- **`crud.WithServerWrites(ctx)`.** Opt-in for trusted server code to
  persist ReadOnly/Hidden fields through
  `CreateOne`/`UpdateOne`/batch/upsert; previously such writes were
  silently dropped with no error. HTTP handlers never set the flag
  (mass-assignment protection unchanged); owner and tenant columns stay
  context-stamped and body-immutable regardless.
- **`AuthConfig.DefaultRoles` + `AuthManager.ListUsers`.** New-account
  roles are configurable (register, magic-link, and OAuth auto-create;
  still strictly server-assigned), and back offices can enumerate users
  through the optional `UserLister` store interface (implemented on
  `EntityUserStore`, paginated, never selects the password hash)
  instead of raw-SQLing `auth_users`.
- **Queue failure logging.** `battery/queue` DBQueue and MemoryQueue
  take a logger (`WithDBLogger` / `WithLogger`, default
  `slog.Default()`): handler failures log at WARN, terminal
  dead-letters at ERROR, swallowed Ack/Nack errors at WARN. A failing
  job is no longer silent.

### Fixed

- **BarChart no longer renders black bars** for unrecognized `Color`
  values: registered status variants resolve to their accent color,
  syntax-valid CSS colors pass through, anything else falls back to the
  theme primary.
- **Docs drift**: `uploads.md` documented a nonexistent
  `upload.NewLocal(dir, urlPrefix)` API; `entity-declarations.md`
  claimed hidden fields are "still stored and API-readable" (they are
  excluded from responses and skipped on client writes); the stale
  "in-memory user store" comment on `AuthConfig.UserStore`.
- **Docs discoverability**: the island cookbook
  (`interactive-patterns.md`) is now cross-linked from the entity,
  admin, UI, and widget docs; `theming.md` gains a self-hosted web
  fonts recipe with the explicit CDN-fonts-are-CSP-blocked callout.

## [0.17.0] - 2026-07-10

### Added

- **Module manifests + runtime enable/disable (#35).** A Module is a
  Battery plus a manifest (`ModuleManifest` with `DependsOn`,
  `MigrationGroup`, `Version`, `Description`). Everything a module
  registers during `Init`, routes, entities, cron jobs, queue
  consumers, MCP tools, is attributed to the module, and a live
  enable/disable gate is enforced at dispatch time: disabled → routes
  404 (before auth, so existence doesn't leak; the gate keys on
  `METHOD + " " + path` so two modules owning different methods on the
  same path are gated independently, and 405 `Allow` headers list only
  non-gated methods), cron jobs skip, queue jobs defer (released to
  pending without consuming retries: they run on re-enable; gated job
  types are filtered before claim so the DB queue never churns
  claim/release cycles), MCP tools refuse with a generic
  `"tool unavailable"` error and are excluded from `tools/list`.
  Toggling is persisted (`gofastr_modules` table when a DB is set,
  in-memory otherwise; if the table cannot be created, `Start` fails
  closed rather than silently re-enabling every disabled module),
  fail-closed on dependencies (disable refuses if an enabled module
  depends on it; enable refuses if a dependency is disabled; both
  check-then-act sequences are serialized by a toggle mutex), and
  propagates across replicas via `WithFanout` on topic
  `gofastr.modules` with node-ID self-dedup. The fanout message is a
  refresh signal only; the receiving replica re-reads authoritative
  state from its store rather than trusting the payload. Attribution
  hooks are string callbacks on `core/router` (`SetRegisterHook` /
  `SetRouteGate`), `core/mcp` (`SetRegisterHook` / `SetCallGate`),
  `framework/cron` (`Scheduler.SetGate`), and `battery/queue`
  (`DBQueue.SetGate` / `MemoryQueue.SetGate`); neither core package
  imports framework. New `app_modules` introspection tool under
  `WithMCPIntrospection`. (#35)

- **First-run setup (`battery/setup` + `framework.WithSetup`).** A
  deployed binary against an empty database now has a guided first
  boot: while the `Complete` predicate reports false, `Start` serves
  an SSR setup
  wizard (composed from the design system) instead of the app router;
  everything else 503s, `/healthz`+`/readyz` stay up, and background
  consumers (cron/queue/outbox relay) wait until bootstrap finishes,
  then the handler swaps atomically with no restart. Access requires a
  **single-use setup token** printed to the boot banner (first visit
  exchanges it for an HttpOnly cookie and invalidates the URL form;
  restart mints a fresh one); wizard POSTs are origin-guarded
  regardless. The same steps run **headless** for IaC installs when
  their env vars (`GOFASTR_ADMIN_EMAIL`/`GOFASTR_ADMIN_PASSWORD`, or
  per-field `EnvVar`) fully resolve, before the port binds, failing
  loud. Steps are pluggable (`setup.Step` with fields, validation, and
  a `Run`); shipped: `setup.AdminStep(auth, db, usersTable)` (initial
  admin via the auth battery's hasher + password policy) and
  `setup.HealthStep(app)` (readiness checks with actionable errors).
  Completion is derived state, never a marker file; a crash mid-setup
  re-enters setup. `GOFASTR_SETUP=off|force` overrides; worker-role
  processes refuse to start while setup is incomplete. See the
  first-run doc. (#34)

- **Migration groups (`Migration.Group` / `-- +migrate Group <name>`).**
  Migrations can now be scoped to the feature or module that owns them:
  versions are unique per group, `Up`/`Down`/`Status` take an optional
  group selection (`m.Up(ctx, "knowledge")`, CLI `--group=<name>`,
  repeatable), and enabling a feature later applies only its pending
  group under the same advisory lock. Within a group ordering is
  strictly by version; across groups a run interleaves in
  `(version, group)` order, a deterministic tiebreak, not a dependency
  mechanism (groups must be self-contained). Group-less usage is
  untouched: the runner emits byte-identical SQL and never alters the
  tracking table until a non-default group is actually in play, at
  which point `group_name` is added and the primary key upgrades in
  place to `(group_name, version)` (atomic ALTER on Postgres, a
  transactional rebuild on SQLite). Checksums, dirty state, and
  `force` key on `(group, version)`; `migrate generate --group=<name>`
  stamps the directive. A named group with no registered migrations is
  a *disabled module*: its applied rows are shown by `status` but never
  compared, blocked on, rolled back, or dropped (`force --group` is the
  reconciliation escape hatch). The default group is never treated as
  a module, so drift there still errors. `--group=default` (reserved
  name) addresses the default group in selections. `Migrator.Register`
  now returns an error (duplicate `(group, version)` or invalid group
  name). (#33)

## [0.16.0] - 2026-07-09

### Added

- **Cross-replica real-time push (`framework.WithFanout`).** The real-time
  lane, entity `_events` SSE streams, `On`/`Subscribe` handlers, and UI-host
  island push, previously stopped at the process boundary: an event emitted
  on replica A never reached a browser connected to replica B (the docs'
  answer was sticky sessions). A new `core/fanout.Fanout` seam bridges it:
  `framework.WithFanout(f)` mirrors every bus emit to the other replicas and
  re-emits it there, and wires any mounted UI host's island manager so island
  updates reach sessions connected elsewhere (`SSEBrokerConfig.Fanout` covers
  hand-built brokers). Backends: `framework/fanout.NewPostgres(dsn, db)`
  (LISTEN/NOTIFY on the database you already run; payloads past the NOTIFY
  size limit spill to a self-purging fallback table) and
  `core/fanout.NewRedis` (bring-your-own client, mirroring
  `cache.RedisClient`); `core/fanout.NewInProcess` simulates replicas in
  tests. **Semantics under fanout: the bus becomes a broadcast**: every
  handler fires on every replica, so side-effect work belongs on outbox
  consumers, and handlers that derive new events must gate on the new
  `event.IsRemote(ctx)`. Delivery stays lossy best-effort (the durable lane
  is the outbox's); publishes never block emitters or request handlers (per
  publish/subscriber bounded drop-oldest queues). Closes the sticky-session
  requirement in the scaling doc. (#28)

- **Worker-process mode (`framework.WithRole` / `GOFASTR_ROLE`).** The same
  binary can now run as a dedicated web or worker process instead of always
  doing both: `RoleServe` serves the full router but never starts
  `AddCron`/`AddQueue` workers or the outbox relay; `RoleWorker` runs those
  consumers and serves only `/healthz` + `/readyz` (same handlers as the full
  router, so orchestrator probes work unchanged); `RoleAll` (default) is
  today's combined behavior. Explicit `WithRole` beats the `GOFASTR_ROLE`
  env var; invalid values fail loudly at construction. Worker-scoped
  drainers are only registered when their workers actually start, so a
  serve-only shutdown never drains a scheduler that never ran. Plain
  `OnStart` hooks stay role-agnostic; gate custom background work on
  `App.Role()`. (#32)

### Changed

- **BREAKING: transactional outbox now delivers per-consumer, not
  whole-row.** The `framework/outbox` relay previously published each row
  to the event bus all-or-nothing across co-subscribers: one failing
  subscriber failed the whole row and blocked its siblings until `Replay`.
  Delivery is now split into two disjoint lanes: a best-effort **real-time
  lane** (the live bus / SSE `EventStream` / ephemeral `On`/`Subscribe`,
  fed post-commit by `EmitEvent`) and a durable **per-consumer lane**.
  Durable consumers are now declared and named via
  `framework.WithOutboxConsumer(name, eventType, handler)` (or
  `Outbox.Consume`); each `(row, consumer)` pair is tracked in a new
  `event_outbox_delivery` child table and retried / backed-off /
  dead-lettered **independently** (sibling isolation). Consumer-set changes
  are handled by **time**, not by a per-replica snapshot, so rolling
  deploys can't lose events: a delivery whose consumer has no handler
  anywhere is `abandoned`, and a parent is completed or an orphan type
  dropped, only once older than the handler grace (`WithHandlerGrace`,
  default 15m), so a lagging replica never destroys, or prematurely
  completes, a freshly-added consumer's work. (Consequence: a
  fully-delivered parent's `dispatched` bookkeeping lags by the grace;
  delivery to consumers stays prompt.) Adds `Outbox.ListDeliveries`,
  `Outbox.ReplayConsumer`
  (resurrects dead *or* abandoned deliveries), `WithHandlerGrace`, and
  `WithRetention` (optional purge of settled rows); `StartRelay` drops its
  `bus` argument and no longer publishes to the event bus. **Migration:**
  drain in-flight outbox rows before upgrading: the new relay ignores the
  old single-delivery row state and there is no automatic backfill.
  Declaring `WithOutboxConsumer` without `WithOutbox` now panics at
  construction rather than silently dropping the consumer.

### Removed

- **OIDC PKCE `code_challenge`.** The confidential OIDC provider no longer
  sends a PKCE `S256` `code_challenge`/`code_verifier`. It was only an
  IdP-compatibility shim: the verifier was derived from the same client
  secret (and public state) that already protects the server-to-server
  code→token exchange, so it added no defense a secret-holder didn't
  already have. Genuine PKCE, a random per-request verifier bound via a
  cookie or store, remains the path for *public* (SPA/mobile) clients and
  is out of scope for the confidential provider. No API change: `AuthURL`
  and `ExchangeCode` are unchanged; the internal `ExchangeCodeWithState`
  seam and the `stateExchanger` interface are gone.

## [0.15.0] - 2026-07-08

Nexus-gap wiring round two: six issues surfaced by building on GoFastr
(tracking #35), each built at the existing seam and then hardened by two
adversarial review rounds. No breaking changes.

### Added

- **Inbound webhook battery** (#26). `battery/webhook` gains an HTTP
  ingestion endpoint beside the outbound one: constant-time, fail-closed,
  body-size-capped signature verification; envelope persistence (memory +
  SQL stores); dedupe by provider delivery id; and enqueue for async
  processing via `battery/queue`. Delivery is durable-before-ack: with a
  queue wired, the dedupe key is registered only *after* a successful
  enqueue, so an envelope that never reached the queue can never
  dedupe-ack the sender's retry (no lost events); without a queue,
  persistence itself is durable acceptance. Best-effort store-update
  failures surface through `IngestConfig.Logger`.
- **Generic OIDC login provider** (#29). `battery/auth` gains an
  authorization-code OIDC provider that verifies the id_token locally:
  RS256/ES256 alg allowlist enforced before key lookup, kty/crv-vs-alg
  cross-check, `iss`/`aud`/`azp`/`exp`/`nbf`/`iat` validation, JWKS fetch
  with per-kid rotation-refetch rate-limiting, and RSA/EC key sanity
  (≥2048-bit moduli, non-degenerate exponent, P-256). A PKCE `S256`
  `code_challenge` is sent for compatibility with IdPs that mandate it
  (the confidential client secret remains the actual exchange protection).
- **Scoped API tokens and service accounts** (#30). `gfsk_`-prefixed
  personal-access tokens and non-human service-account credentials,
  sha256-hashed at rest, scoped `resource:verb` with wildcards.
  `TokenMiddleware` fails closed through a single funnel that clears any
  outer identity on a bad token; `RequireScope` gates machine routes while
  sessions/JWTs pass unscoped. The token-management endpoints are
  session-only, so a leaked scoped token can't mint an unscoped one for
  its owner.
- **Transactional event outbox** (#25). `framework/outbox` delivers events
  at-least-once: `Append` stages a row inside the caller's transaction and
  a leased relay (Postgres `FOR UPDATE SKIP LOCKED` / SQLite tx) delivers
  to the event bus with exponential backoff and a dead-letter state. A
  panicking consumer is retried and eventually dead-lettered, never
  silently marked dispatched (new `EventBus.EmitStrict`). Enable per-App
  with `framework.WithOutbox(...)`; CRUD mutations stage their lifecycle
  events into the caller's transaction automatically. `WithoutEnsureTable`
  opts out of the boot-time table create.
- **`framework.WithoutAutoMigrate()`** (#24). Suppresses the boot-time
  entity DDL for deployments that require every schema change to come from
  a reviewed migration; documented alongside the two-layer migration model.
- **Per-file and additive blueprint generation + scaffolds** (#20).
  `gofastr generate` now emits one file per entity and per screen behind a
  fixed, name-free registration seam. `gofastr generate --from=<partial>
  --add` additively emits only the new pieces into an existing project
  (never overwriting, continuing declaration order, refusing colliding
  routes and pre-0.15 layouts); `gofastr generate entity|screen <name>`
  scaffolds a stub through the same path. `gofastr pack` reverses the new
  layout, so generate/pack still round-trips.

## [0.14.0] - 2026-07-07

Nexus-gap wiring round one: three small framework gaps surfaced by
building Nexus on GoFastr (tracking issue #35), each closed at the
existing seam rather than with new machinery.

### Added

- **OpenAPI specs now say how to authenticate** (#21). When any entity is
  auth-gated (owner-scoped, multi-tenant, or RBAC), the generated spec
  declares `components.securitySchemes`: `bearerAuth` (HTTP bearer, JWT)
  and `cookieAuth` (the auth battery's session cookie, production default
  `__Host-session`), and every gated operation carries a per-operation
  `security` block accepting either. Ungated entities stay unmarked, and
  there is no global `security` requirement. Deployments overriding
  `AuthConfig.SessionCookie` can replace the scheme via
  `Spec.SetSecurityScheme("cookieAuth", …)`. `core/openapi.Operation`
  gained the underlying `Security` field + `AddSecurity`.
- **Rate-limit budget headers** (#22). `core/middleware.RateLimit` emits
  the IETF-draft `RateLimit-Limit` / `RateLimit-Remaining` /
  `RateLimit-Reset` headers on every response (allowed and 429) so API
  clients can self-pace; `RateLimitConfig.OmitBudgetHeaders` suppresses
  them. `Retry-After` on 429 is unchanged. The auth battery's limiter
  deliberately emits only `Retry-After`: a live remaining-attempt count
  on login/reset endpoints would hand attackers brute-force pacing.
- **Storage content checksums** (#23). `battery/storage.SaveWithChecksum`
  tees any `Storage` save through SHA-256 in a single pass and returns
  `SaveResult{Size, SHA256}`; `VerifyChecksum` re-reads and compares,
  wrapping `ErrChecksumMismatch` on mismatch. The `Storage` interface is
  unchanged; the helpers wrap any backend, including user-implemented
  ones.

- **Auth security events reach the audit log** (#31). `battery/auth` now
  emits a fixed-vocabulary `SecurityEvent` at every security decision point:
  login succeeded/pending-2FA/failed, register, logout, the full 2FA
  lifecycle (enroll, challenge pass/fail, disable, backup-code regen),
  password reset requested (known *and* unknown emails, so probing is
  visible) and completed, session revocation with counts, OAuth
  linked/login/refused, and magic-link request/consume. Wire it with one
  line: `AuthConfig.AuditSink = sink` where
  `sink, _ := auth.NewSQLAuditSink(db, "")` writes into the same
  `audit_log` table as the CRUD hooks (entity `"auth"`). Events never
  carry credentials; the only user-controlled string is the email, and a
  leak-guard test greps every event for planted secrets. A panicking or
  failing sink never breaks the auth flow it was recording.
  `framework.AppendAuditEvent` is the new exported append primitive for
  custom sinks, sharing the CRUD trail's sanitization.
- **Postgres full-text search backend** (#27). `search.NewPostgres(db, cfg)`
  implements the existing `Backend` interface over a single table with an
  in-SQL `tsvector` (GIN-indexed, idempotent `EnsureSchema`), ranked
  `ts_rank` results, weighted fields (`Document.Fields` keys promoted to
  weights `'B'..'D'`; `Text` is always `'A'`), configurable language, and
  built-in prefix matching on the final query term (search-as-you-type).
  Query text is sanitized through a single unit-tested chokepoint and always
  parameterized. `Query` gained `FieldEquals`, an exact-match filter on
  `Document.Fields` implemented identically in both backends (JSONB
  containment in Postgres) so tenant/owner scoping happens in-query instead
  of post-filtering. pg_trgm is deliberately omitted (needs
  `CREATE EXTENSION`/superuser).

### Fixed

- Docs drift: `uploads.md` showed a two-method `Storage` interface that
  no longer exists (real interface has `Save`/`Delete`/`Get`/`Exists`,
  with `Save` returning `error`), and `security.md`'s rate-limit example
  used `Requests`/`Window` fields that were renamed to
  `Capacity`/`RefillEvery`/`RefillBy` long ago.

## [0.13.0] - 2026-07-07

The UI-library hardening release: a five-dimension evaluation of
`framework/ui` + `core-ui` (API, correctness, extensibility, docs,
discoverability) followed by fixes for everything it found, an adversarial
review pass over those fixes, and fixes for what *that* found.

### BREAKING

- **Unknown component variants panic at build time.** `ui.Card` now rejects
  an unregistered `CardVariant` the way `ui.Button` always rejected bad
  `ButtonVariant`s, and `ui.ToggleAction` validates `Variant`/`Size` the same
  way. A typo like `Variant: "primry"` that used to silently render unstyled
  markup now fails loudly at screen construction. Register custom variants
  first (see below).
- **Plain modals paint a real panel.** A bare `preset.Modal` used to render
  its slot content floating invisibly on the dimmed backdrop; the centered
  widget skeleton now wraps its slots in one `.fui-panel` that paints surface,
  border, radius, padding, and shadow from theme tokens. Bodies that own their
  chrome opt out with `.fui-slot-bare` on the body's root element (Lightbox
  and CommandPalette are excluded automatically). Anything targeting the old
  `.fui-pos-center > .fui-slot` selector should target `.fui-panel`. Bottom
  sheets similarly gained a default surface (75vh cap, scroll inside).
- **`html.Button` requires a label.** An empty `Label` with no
  `ExtraAttrs["aria-label"]` now panics at build time instead of rendering an
  unlabeled `<button>`. Icon-only buttons pass an `aria-label`. (Data-driven
  renderers degrade instead: a labelless Kiln button node renders with
  `aria-label="button"` rather than panicking at request time.)
- **`ui.Sticky` stacks at the theme's `--z-sticky` (200).** The old CSS read
  a `--z-index-sticky` token that never existed, so sticky elements silently
  stacked at the 100 fallback. Anything relying on that broken value moves up
  to the designed `ZIndexSet` layer. `StickyConfig.ZIndexTier` now actually
  works (`dropdown`/`modal`/`popover`/`toast`) and panics on unknown tiers.
- **`patterns/tabs` panics past 16 tabs** instead of silently breaking (the
  registered stylesheet generates 16 nth-child slots; the panic names the
  ceiling and the escape hatch).

### Added

- **Accessibility enforcement grew three gates.** A shared axe-core harness
  (`internal/axetest`) now drives two app gates: the existing site gate
  (every component demo page, both color schemes) and a new Meridian gate:
  marketing, auth, app, and admin pages scanned logged-in with an **empty
  allowlist**, plus the first open-widget scan (the quick-add modal's open
  DOM state). A keyboard-only traversal gate walks Tab through key pages of
  both apps asserting no focus traps, visible focus indication on every
  stop, complete reachability of interactive elements, and the modal's
  trap-then-release cycle. What the gates flushed out was fixed at the
  source: `battery/admin` pages now render a `<main>` landmark; DataTable's
  empty actions header is hidden from the a11y tree; `PricingCard`, `Card`,
  and `PageHeader` gained a `HeadingLevel` config so composed pages keep a
  sane heading outline; the pricing badge's default text color adapts
  per-scheme via `color-mix` toward the text token; the sidebar's active
  nav link has a visible keyboard focus ring (it previously vanished under
  the `aria-current` background); Sidebar renders a `<div>` shell instead
  of nesting an `<aside>` landmark inside the layout's `<nav>`; and
  Meridian's status/text-subtle palette was retoned to clear WCAG AA on
  the components' tinted chips in both schemes. The site gate also
  enables axe's WCAG 2.2 `target-size` rule (24px minimum) and scans a
  curated page subset at a 390px viewport: carousel dots grew invisible
  24px hit areas (`::after` pip), tree row links meet the floor, and
  horizontally-scrolling command lines are keyboard-focusable.

  An adversarial review of the gates then hardened them further: the
  harness rejects blank pages instead of scanning them as vacuously
  clean; the site's three allowlisted rules apply only to `/components/*`
  demos (content pages scan with an empty allowlist, which surfaced and
  fixed nine real heading/landmark defects across the home, get-started,
  philosophy, and docs pages); every `/docs/<slug>` page, `/kiln`, and
  every Meridian admin CRUD screen (list + create per entity) is now
  scanned. New knobs from those fixes: `CalloutConfig.Landmark` opts an
  in-flow tip out of the complementary-landmark role, and
  `EmptyStateConfig.HeadingLevel` keeps empty states in the page outline
  (AnchoredRail's rail label is no longer a stray `<h6>`).
- **Variant registration.** `ui.RegisterButtonVariant`, `RegisterButtonSize`,
  `RegisterCardVariant`, and `RegisterStatusVariant` open the variant system
  at init time: pass `VariantCSS{Props, Hover, Focus}` (or
  `StatusVariantCSS{Color, Icon}`) with `{colors.x}` token references and get
  a typed variant value back. Status variants fan out to StatusBadge, Tag,
  Callout, and Notification. Registration is init-only (sheets seal on first
  materialization; late registration panics). Custom button variants style
  `ui.ToggleAction` markup too.
- **`ui.ToggleAction`**: an optimistic press-and-commit button
  (follow/subscribe/pin): idle/committed labels + icons, endpoint POST with
  rollback on failure, optional untoggle endpoint, and `data-fui-toggle-group`
  mutex semantics. Gallery demo + e2e.
- **`__gofastr.doc`**: the runtime's single owner of global document state.
  A frozen manifest of every `<html>` attribute, `<body>` class, and DOM
  singleton the runtime may touch (guard warns on undeclared writes),
  refcounted scroll-lock (two closing widgets can't unlock each other's
  scroll), SSR-adopting `singleton()`, and a `reattach()` hook for cross-layout
  shell swaps. Documented in `core-ui/ARCHITECTURE.md`; a test parses the doc
  table against the manifest.
- **Theme slots for the code palette.** `Theme.Code` / `Theme.DarkCode` emit
  the `--tk-*` syntax-highlighting tokens (optional group; zero slots emit
  nothing), so dark mode can restyle code blocks through the theme instead of
  raw CSS.
- **New embedded docs**: `ui-wiring.md` (annotated main.go wiring
  `framework.App` + core-ui app + uihost, compile-verified), `theming.md`
  (token catalog, `DarkColors`, `ui.Themed`, `--ui-*` knobs, and why theming
  never relies on CSS source order), and `runtime-contract.md` (the full
  `data-fui-*` attribute reference + SSR/island/SSE model, sync-tested against
  `core-ui/ARCHITECTURE.md`; fixes five dead links in the embedded docs).
  Plus a form-in-a-modal recipe in `widgets.md` documenting
  `data-fui-rpc-close` / `data-fui-rpc-reset`, and pagination-package
  disambiguation on `DataTableConfig`.
- **Routing users to the UI system.** `framework/ui` registers with
  `agentsinv` (generated apps' AGENTS.md now lists the `ui` package), the
  gofastr-host skill's don't-reinvent table gained a UI row, the docs index
  gained a "Building UI" group, and the README points at the catalog.
- **Meridian exercises the full surface** (design-system canary): a plain
  quick-add modal on the default panel surface, an ink-band
  `ui.Themed`/`RegisterThemeOverride` CTA, an island-mode DataTable (RPC
  sort/pagination sharing one config between screen and endpoint), and
  `--ui-layout-container-width`. Its `app.css` now contains only published
  `--ui-*` variable declarations; the page-header/section internals
  overrides became upstream knobs (`--ui-page-header-title-*`,
  `--ui-section-eyebrow-*`).
- **`gofastr pack` follows extracted helpers.** A resource chain moved into a
  package-local zero-arg helper (so a screen and its island endpoint share one
  config) still reverses to the blueprint block.

### Fixed

- **Security.** Escaping/sanitization holes closed across the component
  catalog: `SignalToggle` label/name/class, chart `Class` (SVG context),
  href sinks in Card, Tag, NotificationBell, ProgressSteps, Sidebar, and
  DocLayout (safeURL with `#` fallback), the CommandPalette→combobox
  `data-fui-push-state` href (scheme allow-list; unsafe values omit the
  attribute), and Menu `ExtraAttrs` keys (routed through the `render.Attr`
  allow-list so a smuggled key can't become a live event handler).
- **Correctness.** Multiselect chips show the option Label (the `label[for]`
  never matched) and option IDs no longer collide ("C++"/"C#", or two
  instances on a page); static-options Combobox SSRs `aria-expanded="true"`
  so Escape/outside-click dismissal works before any keystroke; `ui.Tabs`
  mirrors `aria-selected` on click; SiteHeader styles the active link on
  `aria-current="page"` (the dead `data-fui-active` attribute is gone);
  infinitescroll's noscript fallback stays GET (a noscript form cannot carry
  the CSRF token a POST would need); tree items only show a focus ring when
  focused; heading auto-IDs are documented as deterministic with `ID` as the
  collision escape hatch.
- **Runtime.** `data-fui-rpc-navigate` to the current page re-renders instead
  of silently no-opping, and post-mutation navigation bypasses the stale
  screen cache (including across `X-Gofastr-Location` redirects); demand-loaded
  modules now load when the marker sits on a lazily-mounted root node (drag-to-
  dismiss on late-mounted sheets was silently dead); the minifier never drops a
  semicolon after `)` (an empty `if(x);` body was corrupted into a
  SyntaxError). Core runtime stays within its 12KB gzip budget.
- **Theming/tokens.** Component CSS reads the theme's typography and spacing
  scales: 194 literal `font-size` declarations became `var(--text-*, …)` and
  76 spacing literals became `var(--spacing-*, …)` (a budget test blocks new
  literals); large buttons keep their own step (`--text-lg`); the danger
  button and notification badge read `--color-danger`/`--color-primary-fg`
  instead of hardcoded hex; the pricing-card badge exposes
  `--ui-pricing-card-badge-fg` for themes whose primary isn't text-safe on
  tinted chips.
- **`style.Contribute` works as documented**: the uihost's `app.css` now fans
  in contributed styles automatically as the last layer; no `style.Apply`
  hand-wiring in the host.

### Changed

- The drag-dismiss handler moved out of the core runtime into a demand-loaded
  module (`src/dragdismiss.js`); pages without bottom sheets don't ship it.

## [0.12.1] - 2026-07-06

### Fixed

- **Opening a modal no longer dislodges sticky elements.** The overlay
  scroll-lock set `overflow: hidden` on `<body>`, which turns the body into a
  clipped scroll container and breaks any `position: sticky` descendant: on a
  scrolled docs page, opening the ⌘K command palette (or any modal/drawer) sent
  the sticky nav rail off-screen. The lock now applies to `<html>`, which locks
  the viewport just as effectively while leaving sticky elements pinned and
  preserving scroll position.

### Added

- **The components gallery now covers the full `framework/ui` catalog.** Added
  showcase pages for `Hero`, `HeroSplit`, `PricingCard`, `AuthCard`,
  `FilterToolbar`, `DetailList`, `FactBox`, `TerminalBlock`, `StepRail`, and
  `StatusPill`, which shipped in the design system but had no `/components/<slug>`
  demo. A new coverage test (`TestComponentGalleryCoversUI`) parses
  `framework/ui` and fails when a component constructor has neither a gallery
  entry nor an explicit allow-list line, so the gallery and
  `docs/content/ui-new-components.md` can't silently fall behind again.

### Changed

- **Blueprint tutorial teaches "generate once, then own the Go."** The
  getting-started tutorial no longer edits `gofastr.yml` and re-runs
  `gofastr generate` (which is one-shot and refuses to overwrite without
  `--force`) to add security; it generates once with auth enabled, then adds
  `owner_field` + `access` + the RBAC policy by editing the owned
  `entities/register.go` and `app.go` directly. Also corrects the REST paths to
  the `/api` prefix. The dev-mode→production note in `blueprints.md` likewise
  points at the owned `app.go` rather than "regenerate."

## [0.12.0] - 2026-07-04

### Added

- **Generated `entity_list` screens get facet filters (`filters:`).** A
  top-level `entity_list` block can now declare `filters: [status, assignee_id,
  …]` naming enum, bool, and relation columns. The generated list screen renders
  a responsive `ui.FilterToolbar` above the table: enums as pills (≤4 short
  values) or a `<select>`, bools as Yes/No pills, relations as a `<select>` of
  the related records' display names, folded into the **same** URL-driven GET
  form as the existing search box (never two competing forms). The owned
  `resource.go` engine applies each active facet as a server-side equality
  filter that composes with search, sort, and pagination (sort-header and page
  links preserve the active facets; applying a facet resets to page 1).
  Filtering is **explicit**: omit `filters:` and the list renders exactly as
  before. `validate` rejects a filter column that is unknown or of an
  unsupported type. `pack` round-trips `filters:` back to YAML. The Meridian
  flagship's customers list (enum pills) and invoices list (enum + relation
  selects) now exercise it. This closes the last gap that made both cold-start
  evaluations hand-write their main list screen.
- **Blueprint fonts are self-hosted and actually work.** A theme naming
  `font_heading` / `font_body` used to emit `@font-face` rules pointing at
  `/fonts/<slug>.woff2` while shipping no font files; every fresh app 404'd
  and silently fell back to system fonts (the strict CSP blocks the Google CDN,
  so a `<link>` never worked either). `gofastr generate` now **fetches** each
  family's latin `woff2` subset at generate time and writes it to
  `static/fonts/<slug>.woff2` (defaulting `static_dir` to `static/` when only a
  font is declared), so a named font renders with zero manual steps. Offline
  generation still emits the app but prints a loud warning naming the exact
  files to supply, and the generated `main.go` boot-checks for them; no silent
  404 path remains.
- **Blueprint seed `count:` and `weights:` for realistic demo data.** A seed
  entry can now declare `count: N` to auto-generate N demo rows (filling scalar
  + enum columns with deterministic, reproducible values) and an optional
  per-column `weights:` map to skew enum distributions. The unweighted default
  is a deterministic, *non-uniform* skew seeded from the entity name, replacing
  the flat `open/in_progress/resolved/closed` round-robin that read as obviously
  fake.

- **`owner.AllowCrossOwner(ctx)`: sanctioned cross-owner read escape.**
  Entities with `OwnerField` auto-scope every read to the signed-in user,
  with no way to express an app-legitimate cross-owner aggregate (e.g.
  "spots remaining = capacity − COUNT(bookings across ALL members)" or
  reading a whole waitlist to promote the oldest entry) short of dropping
  to raw SQL against framework-managed tables. `owner.AllowCrossOwner`
  returns a context that lifts owner scoping for the **in-process Go**
  `CrudHandler` methods (`ListAll`, `CountAll`, `GetOne`, and the
  mutate-by-id methods, which share the scope helpers), the owner-side
  twin of `tenant.AllowCrossTenant`. Secure by default is untouched: the
  context key is unexported, so the auto-generated **HTTP CRUD endpoints
  have no path to it** and stay owner-scoped, always (regression-tested).
  It lifts the owner *requirement* only: it authorizes nothing; gate the
  caller yourself. See `docs → entity-declarations` → "Reading across
  owners".

- **`interactive.Action.WithConfirm(message)`: pre-flight confirm as a
  first-class builder method.** The only way to gate a destructive RPC behind
  a confirmation was `interactive.Confirm(msg)` passed to `OnSuccess(...)`,
  but the gate fires *before* the request, not on success, so its placement
  actively misled readers. `WithConfirm` reads in the order it executes and
  emits the identical `data-fui-confirm` attribute. `interactive.Confirm` is
  now deprecated (still works). See `docs → interactive-patterns` → "Confirm".

- **`ui.FilterToolbar`: the filter/sort control strip for list screens.**
  The framework had every filter *primitive* (`Select`, `SegmentedControl`,
  `SearchInput`, `FilterChipBar`) but no composed strip for the ubiquitous
  "row of facet controls + search + sort + Apply/Reset above a table"
  surface, so callers hand-rolled it, and repeatedly shipped the same
  mobile defect: the row overflowed a narrow container and pushed the sort
  control and Apply button off-screen (unreachable at 375px), while pill
  labels wrapped mid-label to three lines. `FilterToolbar` renders one
  URL-driven `<form method="GET">` (facets as `<select>` or `Kind:
  FacetPills` radio-pill groups, optional search + sort, Apply submit +
  Reset link) whose submitted params are the source of truth for the
  screen's `Load(ctx)`. It is responsive by construction: declares itself a
  container and degrades row → wrapped rows → single-column stack as its own
  width shrinks, keeping every control (Apply/Reset included) on-screen and
  tappable, with pills that wrap between themselves but never mid-label.
  Zero new `data-fui-*` attributes (native GET form + radio semantics);
  styling is theme-token CSS, light and dark. See `docs → ui-new-components`
  → "Filter toolbars: the URL-driven pattern".

### Fixed

- **`interactive-patterns` docs are now a complete hand-written-island
  cookbook.** Added an end-to-end recipe covering the four traps cold-start
  authors hit: a raw `data-fui-rpc` route needs its own `app.Router().Post`
  (only `widget.Mount` auto-wires); the RPC JSON key is the input's `name`,
  not its `id` (curl hides the mismatch); a `<select>` rides the existing
  `data-fui-rpc-trigger="input"` (no `change` trigger exists or is needed);
  and the two placeholder syntaxes, `{id}` on the HTTP router vs `:id` on the
  screen router, are a silent 404 when crossed. Also corrected stale
  `interactive.Action{…}` struct-literal examples (fields are unexported; use
  `interactive.Post(...)`) and documented `ui.ConfirmAction` as the themed,
  test-drivable alternative to native `window.confirm`. Behind the select
  recipe: new `TestInputTrigger_SelectFiresRPC` e2e in `core-ui/runtime`.
  README and `blueprints.md` now cross-link the cookbook so blueprint users
  find it before reverse-engineering `runtime.js`.

- **Chart / stat `source:` no longer silently renders "—".** A `stat_card` or
  `*_chart` bound to `source: {entity: X}` rendered an empty dash whenever `X`
  had no generated list/detail screen, because `RegisterGenerated` only
  populated `appResources` for entities with screens. Every entity referenced by
  a data source is now registered (pure lookup-map population; no extra routes),
  so charts sourced from screen-less entities show real numbers. `gofastr
  validate` / `generate` additionally reject a chart/stat source pointing at an
  unknown or crud-disabled entity up front.

### Changed

- **BREAKING (generator output): `gofastr generate` emits a flat `package
  main`, and is now a one-shot generator.** The blueprint used to scaffold a
  `blueprint/` subpackage (`package blueprint`: `app.go`, `screens.go`,
  `resource.go`, `stubs.go`, `resource_test.go`) alongside `main.go`. That
  folder read as "the generator's", forced every custom screen into it, and
  the unexported `blueprintAuthPolicy` etc. leaked generator branding into
  your code. Those files now land at the project root as ordinary
  `package main` (`entities/` is unchanged), and `main.go` calls
  `RegisterGenerated(...)` directly instead of importing a `blueprint`
  package. Emitted identifiers dropped the `Blueprint`/`blueprint` prefix
  (`BlueprintAppName`→`appName`, `blueprintAuthPolicy`→`authPolicy`,
  `BlueprintBaseCSS`→`appBaseCSS`, `BlueprintFontCSS`→`fontFaceCSS`,
  `blueprintResources`→`appResources`, `BlueprintSeedData`→`seedData`, …).
  Generation now **refuses** to overwrite an existing project: if any target
  file is present it lists the conflicts and stops; pass `--force` to
  overwrite. There is no merge/regen workflow: the emitted code is yours to
  own and edit. **Existing generated apps are unaffected at runtime**: only
  the output of a fresh `generate` changes; re-run `generate --force` into a
  scratch dir if you want the new layout. `gofastr pack` reads the new flat
  layout.

- **`ui.BarChart` is legible by default.** Dashboard bar charts previously
  rendered as flat, unlabeled slabs: no way to read magnitudes, near-equal
  or uniform data (8/8/8/8) all looked like identical full-height
  rectangles, and 4+ category labels collided/truncated mid-word. The chart
  now (1) prints each bar's value above its cap by default (opt out with
  `BarChartConfig.HideValues`); (2) rounds the y-scale up to a clean maximum
  so the tallest bar keeps ~15% headroom: uniform and near-equal data read
  as intentionally-equal or clearly-ranked bars, never a wall of slabs;
  (3) always draws a hairline baseline; (4) wraps long `ShowLabels` category
  labels onto two lines (a single over-long word ellipsizes; the full text
  stays in the bar's `<title>`); and (5) caps bar thickness so 1–2 category
  charts don't render giant blocks. `ShowAxis` now renders proper gridlines
  at clean tick values with left-gutter numeric labels. All styling stays in
  `registry.RegisterStyle` + theme tokens (verified light + dark + 375px).
  Default `Height` is now 200 (was 180) to fit the value + label bands.
- **`ui.LineChart` edge x-axis labels no longer clip.** The first and last
  tick labels sit exactly on the SVG's left/right boundary; they now anchor
  inward (`start` / `end`) instead of centering and clipping.

## [0.11.0] - 2026-07-03

### Documentation

- **Doc code samples that cited non-existent APIs are corrected**:
  `app.Router` as a field → `app.Router()`, `Router.With(...)` → wrap
  the handler with `RequirePermission(...)`, `u.HasRole` →
  `slices.Contains(u.GetRoles(), …)`, `Registry.Names()` →
  `range Registry.All()`, `cron.Scheduler.Every(string, fn)` →
  `Register(CronJob{…})`, and the phantom webhook `Sign`/`Verify`/
  `sha256=` paragraph removed. A new `framework/docs` regression test
  (`TestDocsAvoidKnownWrongAPIs`) fails if any of these forms, or a
  `migrate diff` command reference, reappears in an embedded doc.
- **Doc drift cleaned up**: README `migrate diff` → `migrate generate`
  and `force`; CLI `--help` lists `audit lint` and the full `migrate`
  subcommand set; blueprint field keys (`auto_generate`, `read_only`,
  `hidden`, `pattern`) and `app.theme` font/dark tokens documented;
  flagship renamed ecommerce→meridian in `comparison.md`; the "~10 UI
  primitives" count corrected to 90+; stale "@main install" tutorial
  note removed; new `scaling.md`/`deploy.md`/`observability.md`/
  `agent-ready.md` added to the README index.

### Fixed

- **Reliability footguns** (Tier 3):
  - `battery/queue`: the DBQueue-cron `Scheduler` re-reads its schedule
    set each tick and re-arms on `Register`, so jobs registered after
    `Start` fire (it used to snapshot once and drop late registrations).
    New `WithDBHandlerTimeout` cancels a stuck handler's context so a
    black-holed dependency can't wedge the (single default) worker. The
    in-memory queue re-enqueues timed-out retries on a fresh context
    instead of the already-cancelled one (retries were silently lost).
  - `battery/email`: the SMTP sender bounds its dial (`SMTPConfig.
    DialTimeout`, default 10s) and sets the same budget as the
    connection's I/O deadline (the caller's ctx deadline wins when
    sooner, including on the implicit-TLS path), so neither a
    black-holed host nor one that accepts and then stalls mid-exchange
    can hang the caller forever.
  - `battery/cache`: `NewMemoryCache` warns when built both unbounded
    and never-expiring (the OOM shape).
  - `framework/event`: a panicking subscriber is still recovered (no
    write rollback) but now logged at Error with its stack, instead of
    silently no-op'ing.
- **Generator design-system regression (`.mrd-*`) removed** (`cmd/gofastr`).
  The blueprint generator emitted dead `mrd-chart`/`mrd-chart__title`/
  `mrd-muted` classes into every generated app, the exact one-styling-
  surface tripwire documented as fixed in June. Titled charts now compose
  `ui.Card(Heading: …)`, and empty values render the new `ui.EmptyValue()`
  (a `ui.Muted` em dash, colored by `--color-text-muted`).
  `TestGeneratorEmitsNoBespokeClasses` pins the contract; meridian and
  ecommerce were regenerated.
- **`line_chart` blocks render** (`cmd/gofastr`). `line_chart` validated
  and compiled but fell through to an HTML comment. It now renders a
  `ui.LineChart` over the grouped counts, and all three chart kinds
  **require** a valid `source: {entity, group_by}` at validation time
  instead of silently disappearing.
- **`examples/backoffice` ships zero bespoke CSS.** The `.bo-*`
  stylesheet and hand-rolled form markup are gone; the public pages
  compose `ui.Hero`, `ui.AuthCard`, `ui.Form`/`ui.FormField`, and the
  centered-container layout shell.

### Added (design system)

- **`ui.Muted` / `ui.EmptyValue`** (`framework/ui`): subdued inline
  text and the canonical muted em-dash "no value" placeholder, colored
  via `--color-text-muted`.

- **`WithConfig` merges instead of clobbering** (`framework`). Granular
  options (`WithAPIPrefix`, `WithPublicOpenAPI`, …) set before or after
  `WithConfig` now survive: each non-zero `AppConfig` field wins, zero
  fields preserve what's already set, the same contract the
  `WithAgentReady` fix established. A reflection test pins the field
  list so new `AppConfig` fields can't silently drop out of the merge.
- **Graceful shutdown is now the default** (`framework`). `App.Start`
  installs a SIGINT/SIGTERM handler that runs the full `App.Shutdown`
  drain: HTTP server, batteries, OnStop hooks, before the process
  exits, matching what `deploy.md` always claimed. The drain is bounded
  by the new `AppConfig.ShutdownTimeout` (default 15s): connections
  still open at the deadline (e.g. SSE streams, which never go idle)
  are force-closed instead of hanging the drain. In-flight cron job
  goroutines are joined under the same deadline via the new
  `cron.Scheduler.StopContext`; `AppConfig.DisableSignalHandling` opts
  out for hosts that own signal handling themselves.

### Added

- **Horizontal-scaling doc + multi-replica boot warnings.** New
  `scaling.md` doc page consolidates every process-local default
  (sessions, 2FA state, rate limits, cron, in-memory queue, SSE push,
  cache) with its replica-safe alternative. `battery/auth` production
  mode now logs a WARN at Init when running on the default in-memory
  session or 2FA store; `AuthConfig.AllowInMemoryStores: true` is the
  explicit single-node opt-in that silences it.

### Security

- **HSTS emitted by default** (`core/middleware`). The default
  `SecurityHeadersConfig` now sends `Strict-Transport-Security:
  max-age=31536000` on HTTPS responses, direct TLS or an
  `X-Forwarded-Proto: https` proxy. Previously the zero-value config
  meant no HSTS at all. `HSTSMaxAge: -1` opts out.
- **CSRF cookie is `Secure`/`__Host-` behind a TLS proxy**
  (`core/middleware`). The CSRF middleware now treats
  `X-Forwarded-Proto: https` as HTTPS, so a proxy-terminated deployment
  gets the secure cookie instead of a plain one. Both this check and
  the HSTS one compare the header case-insensitively (matching uihost),
  so proxies that send `HTTPS` count too.
- **Login/register/logout reject cross-site form posts** (`battery/auth`).
  These cookieless-CSRF-prone endpoints now refuse a form POST whose
  `Origin` (or `Sec-Fetch-Site: cross-site`) is another site, closing
  the login-CSRF vector SameSite cookies don't cover. JSON posts and
  no-Origin clients (curl, native apps) are unaffected.
- **`/auth/register` is rate-limited by default** (`battery/auth`). A new
  `AuthConfig.RegisterRateLimit` defaults to 10/min/IP (15-min block),
  matching login's always-on throttle, to blunt account-table flooding
  and email bombing. Form submissions that hit the limit get the
  form-aware 303 error redirect (not a raw JSON 429 page), and
  cross-site posts are rejected before they count against the budget:
  an attacker page can't burn a victim's own login/register allowance.
- **Blueprint refuses `multi_tenant` with no resolver** (`cmd/gofastr`).
  A generated multi-tenant app has no tenant resolver (the strategy is
  host-specific) and `ApplyTenantScope` is fail-closed, so it read empty
  and stamped empty tenants, broken while looking secure. `validate`/
  `generate` now reject it with the manual-wiring remedy.
- **`golang.org/x/image` bumped to v0.43.0**: clears the four
  GO-2026-506x/4961 decode-DoS advisories reachable from
  `framework/image`.
- **BREAKING: `battery/admin` exposure is opt-in.** An empty
  `admin.Config.Entities` now exposes **nothing** instead of every
  CRUD-enabled table as an editable back-office. Set the new
  `AllEntities: true` for the previous whole-back-office behavior
  (still skips `CRUD=false` credential tables), or name entities
  explicitly. The blueprint generator and examples set `AllEntities`.
- **Generator warns on ANY unscoped auto-exposed entity**
  (`cmd/gofastr`). The PII lint only matched a fixed token list, so
  `notes`/`journal_entries`/`balances` generated fully public with no
  signal. `gofastr generate` now warns for every auto-exposed entity
  with no `owner_field`/`access`/`multi_tenant`, spelling out the
  anonymous read/write exposure. `examples/api-tour`'s `profiles`
  (per-user bio) now gates its write operations via `Access`; reads
  stay public for the `?include=` tour flows, anonymous writes 403.
- **Webhook SSRF guard survives a custom `HTTPClient`**
  (`battery/webhook`). Supplying `Options.HTTPClient` (proxy, tracing,
  timeout) previously dropped the dial-time SSRF guard entirely,
  reopening loopback/RFC1918/169.254.169.254 via DNS rebinding. `New`
  now wraps the caller's client with a per-request check that resolves
  the delivery target and refuses internal IPs before the caller's
  transport runs; the transport itself (proxy, tunnel, custom dialer)
  is used verbatim. `AllowPrivateNetworks: true` remains the only
  opt-out.
- **Blueprint-generated apps no longer commit secrets** (`cmd/gofastr`).
  Generated Go reads `JWT_SECRET`, `DATABASE_URL`, and
  `ADMIN_SEED_PASSWORD` from the environment instead of inlining the
  blueprint's values as string literals (the emitted `BlueprintDBURL`
  constant is password-stripped, and the generated e2e test reads the
  admin password from env too). When the blueprint holds secrets the
  generator emits a `.env` (so the app still runs out of the box) plus
  a `.gitignore` excluding it, and `main.go` loads `.env` before the DB
  opens. A deploy without `ADMIN_SEED_PASSWORD` logs a WARN instead of
  silently skipping the admin seed; the generated e2e test falls back
  to a test-local password so a fresh checkout stays green; DSN
  redaction fails closed on unparseable URLs and handles libpq-quoted
  passwords. `.env` values are quoted when their shape requires it, and
  `gofastr pack` reads the file back with the same `core/dotenv` parser
  the generated app boots with, so awkward secrets round-trip exactly.
  `TestBlueprintNeverInlinesSecrets` pins the contract.
- **2FA now fails closed at login** (`battery/auth`). If a registered
  `TwoFactorChecker` reports a user enrolled but the pending-2FA state
  can't be established; the session store doesn't implement
  `SessionPendingMarker`, the mark call errors, or the 2FA state lookup
  itself fails; login is rejected (500), the just-minted session is
  destroyed, and a WARN is logged. Previously a custom session store
  silently downgraded every 2FA-enrolled account to password-only auth.
- **Pending-2FA logins no longer receive a JWT.** The JSON login
  response for a 2FA-enrolled user omits `token` until the challenge
  succeeds (a stateless JWT issued at password time bypassed the second
  factor on JWT-authenticated routes) and now carries
  `"two_factor_required": true` so clients can branch without probing.

## [0.10.0] - 2026-06-27

### Added: agent-readiness surface (isitagentready.com)

GoFastr apps can now advertise the discovery artifacts AI web agents
(and scanners like isitagentready.com) look for, in one opt-in call.
The framework already shipped the plumbing: MCP tools, an OpenAPI spec,
per-screen markdown, sitemap, robots, so this is the *discovery* layer
that makes those capabilities findable. Everything is additive and opt-in;
existing robots/sitemap/openapi/llm.md behavior is unchanged.

- **`uihost.WithAgentReady`** (`framework/uihost`): one-call bundle: serves
  `/llms.txt` + the A2A agent card + AI-bot-aware robots rules + `Link`
  response headers on every HTML page. Granular options
  (`WithLLMsTxt`, `WithAgentCard`, `WithAgentLinkHeaders`,
  `WithMarkdownNegotiation`) expose each piece.
- **`/llms.txt`** (llmstxt.org): curated markdown index (H1 title,
  blockquote summary, `## Section` file-lists); a default Docs section
  links the app's `/llm-pages.md` index when `WithPublicLLMMD` is on.
- **A2A agent card**: `/.well-known/agent-card.json` (+ legacy
  `/.well-known/agent.json`) describing identity, service URL,
  capabilities, and skills (Agent2Agent v1.0, camelCase keys; `supportedInterfaces`
  + `skills` always present). The service endpoint lives in
  `supportedInterfaces[].url`: when `MCPEndpoint` is set, `/mcp` is
  advertised as the JSON-RPC interface (it genuinely speaks JSON-RPC)
  and a derived `mcp` skill points agents at it.
- **AI-bot-aware robots**: `AllowAIBots` augments `/robots.txt` with
  explicit per-crawler rules (GPTBot, ClaudeBot, Google-Extended, …),
  merged into the existing `WithRobots` config regardless of option order.
- **`Link:` response headers**: every HTML page advertises
  `rel="sitemap"`, `rel="llms-txt"`, `rel="agent-card"`,
  `rel="service"` (the MCP endpoint), `rel="service-desc"` (OpenAPI, when
  `OpenAPIEndpoint` is set), and `rel="alternate"` markdown.
  Absolute URLs resolve one canonical origin (`WithAgentReady`/`WithSitemap`
  `BaseURL`, else the forwarded request scheme+host).
- **Markdown content negotiation**: `WithMarkdownNegotiation()` serves a
  page's markdown when the request `Accept`s `text/markdown`.
- **`framework.WithMCP`**: auto-mounts `/mcp` (Streamable HTTP: POST
  JSON-RPC + GET SSE), replacing the hand-wired
  `Router().Handle("POST","/mcp", MCP)`.
- **`framework.WithOAuthProtectedResource`**: serves
  `/.well-known/oauth-protected-resource` (RFC 9728) for OAuth-token-
  protected APIs.
- **MCP handshake**: `core/mcp` now handles `initialize` (returns
  protocolVersion + capabilities + serverInfo, name wired from the app's
  `Config.Name`) and `ping`, so the advertised `/mcp` is functional for
  spec-compliant MCP clients (Claude, Cursor, …), not just `tools/list`.
- **Scanner well-known endpoints**: the isitagentready.com checks the
  framework now auto-serves: `/.well-known/api-catalog` (RFC 9727
  linkset+json, when the app has an API), the MCP Server Card at both
  `/.well-known/mcp/server-card.json` (scanner path) and the spec-reserved
  `/mcp/server-card` + `/.well-known/mcp/catalog.json` (SEP-2127 shape:
  $schema/name/version/description/remotes), when `WithMCP` is on,
  `/.well-known/agent-skills/index.json` (always; opt-in entries via
  `WithAgentSkills`), and opt-in `/.well-known/oauth-authorization-server`
  (RFC 8414, via `WithOAuthAuthorizationServer`).
- **Content Signals**: `AgentReadyConfig.ContentSignals` emits a
  `Content-Signal:` directive in robots.txt (contentsignals.org), e.g.
  `ai-train=no, search=yes, ai-input=yes`.
- **Auth.md** (WorkOS agentic-registration profile): `WithAuthMD` serves
  `/auth.md` (a markdown manifest) and merges an `agent_auth` block
  (skill + identity/claim/events endpoints) into the
  `/.well-known/oauth-authorization-server` metadata.
- **Web Bot Auth / UCP / ACP** (remaining production-scanner checks):
  `WithWebBotAuth` serves `/.well-known/http-message-signatures-directory`
  (the site's signing JWKS), `WithUCP` serves `/.well-known/ucp`, `WithACP`
  serves `/.well-known/acp.json`. DNS-AID / x402 / MPP / WebMCP / ap2 remain
  documented-only (DNS / payment-middleware / client-side / server-only).
- **Docs**: new `framework/docs/content/agent-ready.md` reference;
  `examples/site` dogfoods the full bundle (`WithAgentReady` +
  `WithMCP` + `WithMCPIntrospection`) so gofastr.dev is agent-ready, and
  now also serves `Accept: text/markdown` content negotiation
  (`WithPublicLLMMD` + `ContentNegotiation`).

### Fixed

- **`uihost.WithAgentReady` merges instead of clobbering.** It replaced the
  agent-ready config wholesale, silently dropping any granular option
  (`WithMarkdownNegotiation`, `WithLLMsTxt`, `WithAgentCard`,
  `WithAgentLinkHeaders`) set before it. It now merges field-by-field, so the
  bundle and granular options compose regardless of option order.
- **`examples/site` docs catalogue**: the `/docs/` "Every doc · A–Z" index
  linked 10 embedded docs (incl. the new agent-ready reference) that had no
  registered route, rendering live links to 404s. All are now catalogued
  (51 → 61 doc pages), guarded by a new embedded-doc → catalogue parity test.

What this deliberately does not do: no A2A task server (the A2A card is
discovery-only; A2A/Auth.md/WebMCP/commerce aren't among the scanner's
scored checks); DNS-AID (infra/DNS, documented); Web Bot Auth (client-side
RFC 9421, documented); commerce (x402/MPP/UCP/ACP; no core primitives).
The 11 scored isitagentready checks are all covered (6 always-on, the rest
opt-in/conditional).

## [0.9.0] - 2026-06-25

### Added: `log.ConsoleSink` (zero-config colorized dev feed)

The log battery now ships a human-readable console sink alongside the
JSON file sink, so `log.New(Config{})` gives every local developer a
colorized stderr feed with no configuration, without leaking ANSI into
prod where stderr is captured (journald, containers) rather than shown.

- **`log.ConsoleSink(ConsoleOpts)`** (`battery/log`): a Sink that renders
  each JSON entry as a single human-readable, optionally colorized line
  (`14:32:07.412 INFO  app.start app="myapp" go="go1.24.1"`). Level
  colors, a bolded message, and a dimmed timestamp; attr order preserved
  via token decoding so operators see fields in emit order, not
  `json.Unmarshal`'s randomized map order. Multi-line values (e.g. panic
  stacks) keep newlines escaped so each entry stays one line; non-object
  bytes are written verbatim so a malformed entry is visible, not
  silently dropped. Honors the `NO_COLOR` convention; serializes on a
  mutex so concurrent entries don't interleave.
- **`log.Config.Console` (`ConsoleMode`)**: `ConsoleAuto` (the zero
  value) attaches the sink only when stderr is a terminal and `NO_COLOR`
  is unset; `ConsoleOn` forces it on regardless of TTY (coloring still
  follows TTY + `NO_COLOR`, so piped output drops ANSI and stays
  greppable); `ConsoleOff` disables it. The console sink is appended last
  so it closes last on shutdown.
- **Purely additive**: the file and webhook sinks run unchanged
  alongside it; nothing about the existing JSON log surface moves.

## [0.8.1] - 2026-06-17

### Changed: README rework

- **Added a "Why this exists" section** stating plainly that GoFastr is a
  personal project first: solidifying web-tech foundations, attacking UI
  generation from a compiled-language angle (the author's background is
  Node), working in a compiled language, skipping the convention-vs-
  configuration choice, building something large with AI, and making a
  framework that's AI-first on both the authoring and the consuming side.
- **Removed the framework comparison** from the README: the
  PocketBase / Encore / Wasp / Supabase / FastAPI name-drops and the
  `comparison.md` link. (The `comparison.md` file itself is left on disk
  for now.)
- **Demoted Kiln to a brief experimental mention.** The README no longer
  leads with it: the ~60-line Kiln section became one paragraph linking
  to `kiln.md`, the "Built with GoFastr" Kiln bullet and the `cmd/kiln`
  install line were dropped, and the repo-layout line is marked
  `(experimental)`.
- **Migrated the README-only Kiln detail into `kiln.md`** so nothing was
  lost: plan-gated destructive ops, the full tool surface, the Claude Code
  MCP wiring, and a concrete HTTP tool-call example.
- **Rephrased "dogfooded" → "runs on itself";** fixed the opaque
  "Walkthrough: the v2 read/write surface" heading → "Walkthrough: the
  read/write API".

## [0.8.0] - 2026-06-16

### Added: `gofastr export` (native static-site generation)

The framework now exports a deploy-ready static site itself, replacing the
broken `wget --mirror` crawl used for the GitHub Pages deploy. The crawl baked
cache-bust `?v=<hash>` queries into on-disk module filenames; the static host
strips the query, every split runtime module 404'd, and all client
interactivity (theme toggle, command palette, copy, widgets) silently died.

- **`App.ExportStatic(ctx, dir, basePath)`** (`framework`): drives the app
  in-process (no port, no crawl), enumerates every declared route, renders each
  through the SSG-aware path, and dumps all `/__gofastr` assets (split runtime
  modules, `color-scheme.js`, `app.css`, per-component CSS) with **query-free
  filenames**. Finds the `*uihost.UIHost` via `Mountables()`.
- **`static.Builder`** (`framework/static`): the already-tested builder, now
  wired as the export engine. Emits query-free assets, the `color-scheme.js`
  bootstrap, and split runtime modules via `runtime.ModuleNames()`/`Module()`.
- **Runtime static-mode** (`core-ui/runtime`): a `data-fui-static` marker on
  `<html>` (stamped only at export time) no-ops every server-backed dispatch
  (RPC, widget-catalog fetch, `data-fui-open`) so disabled actions read as
  intentionally inactive, not broken. Client-only features (theme, copy,
  signals) are unaffected; live pages carry no marker so the guards are no-ops
  in the normal server-backed app. Detected via `hasAttribute` to stay within
  the runtime's 12 KB gzip budget.
- **Subpath base path** (`--export-base /<repo>`): a GitHub Pages *project*
  site serves the artifact under a subpath, so the builder prefixes every
  root-absolute `src`/`href`, the inline component-catalog `stylePath` JSON
  values, and bakes the prefix into the emitted `runtime.js` (it constructs
  split-module URLs in JS). External links, fragments, and code samples
  (`core/markdown` escapes quotes in `<code>`) are left untouched. Omit for an
  apex/custom-domain deploy.
- **`ui.Banner` "static preview" notice**: injected at export time only,
  dismissible (`DismissID: gofastr-static-preview`, persists in `localStorage`),
  explaining that server-backed actions are disabled and how to run locally.
- **`examples/site` `--export` / `--export-base` flags**: the same binary
  serves live *or* exports; the live `Start` path is byte-identical when the
  flag is absent.
- **Docs**: new `framework/docs/content/static-export.md` guide (what it is,
  how to export, what gets emitted, static-mode behavior, subpath vs apex,
  GitHub Pages workflow, common mistakes). `.github/workflows/pages.yml` now
  runs `site --export _site --export-base /gofastr`.

### Added: `ui.CodeBlock.Scroll` + `ui.HighlightLines`

- **`CodeBlockConfig.Scroll`** (`framework/ui`): caps the body height
  (`var(--ui-code-block-scroll-max, 26rem)`) and makes it scroll vertically, for
  showing a long file in full without it dominating the page. Forces the framed
  container; horizontal panning still works.
- **`ui.HighlightLines`** (`framework/ui`): the fenced-block tokenizer is now
  exported, so callers can render raw source through `CodeBlockConfig.Lines`
  with the same comment/string/number highlighting the markdown renderer uses.
- **`/examples` Meridian row shows the real blueprint**: the synthetic,
  malformed pseudo-YAML snippet is replaced with the **exact, full
  `examples/meridian/gofastr.yml`** (embedded at build time, drift-guarded by
  `TestEmbeddedBlueprintsMatchSource`), shown in a scrolling, copyable block.
  The copy button copies valid YAML with newlines (via `innerText`).

### Fixed

- **Sticky sidebars were silently broken site-wide.** `body { overflow-x:
  hidden }` (latent since the site-chrome commit) forced `overflow-y` to
  compute as `auto`, turning `<body>` into a scroll container; every
  `position: sticky` descendant (docs + components sidebars, in-page TOC,
  get-started step-rail) then anchored to the non-scrolling `body` and
  scrolled away with the page. The horizontal-scroll guard now lives on
  `html` (the real viewport scroller); `body` is `overflow: visible`.
- **Components-page sidebar wouldn't pin even after the above.** The
  framework's sidebar `<nav>` wrapper is the grid column but didn't pass its
  height down to the `SectionMenu` widget inside it, leaving the sticky rail's
  containing block too short to travel. The wrapper is now a flex column so
  the widget fills the column (no-op on short pages; pins on tall ones).
- **Header brand read as a doubled version** (`λ gofastr v0.x dev`). Dropped
  the static `v0.x` stability tag sitting beside the version; the badge now
  shows one version (`dev` locally, `v0.8.0` tagged), with the "v0.x — pin a
  version" warning kept as the status tooltip.

### Changed

- **Homepage + getting-started repositioned to lead with screens + blueprints.**
  A new "One file, a real app" section pairs a generated screen mock with the
  `gofastr.yml` blueprint + `gofastr generate` output, and the hero lede
  foregrounds screens/endpoints/MCP/migrations instead of centering the data
  layer.
- **Kiln marked experimental** across the site + `framework/docs/content/kiln.md`
  (hero pill, get-started card, footer, palette, docs catalog) so its maturity
  is honest at first glance.

## [0.7.0] - 2026-06-15

### Added: marketing pricing + long-form content blocks

- **`ui.PricingCard`** (`framework/ui`): a real marketing pricing card (plan
  name, headline price + period, checked feature list, CTA, featured variant) so
  pricing pages read like marketing instead of a CRUD table. Composed via a new
  `pricing` blueprint block (`props.plans[]`).
- **`markdown` blueprint block** renders rich long-form prose via `ui.Markdown`
  from a `text:` string; plain `heading`/`paragraph` content is now typeset to a
  readable measure on marketing pages instead of running full-bleed. The Meridian
  flagship's `/pricing` is now pricing cards and `/terms` + `/privacy` are
  markdown, demonstrating all three content treatments.

### Added: `gofastr pack` (the inverse of generate)

- **`gofastr pack [app-dir]`** reconstructs a `gofastr.yml` from a generated app's
  Go source, the inverse of `gofastr generate`. It reads the real artifacts
  (`entities/register.go`, `blueprint/app.go`, `blueprint/stubs.go`,
  `blueprint/screens.go`) via the Go AST and re-serializes the authored blueprint:
  app config + theme/dark + auth + admin, every entity (fields, types, access,
  indices, relations), the screens (reversing the emitted `framework/ui` grammar:
  hero, sections, cards, charts, stat cards, entity list/detail, auth forms,
  headings), nav, and seed. Synthesized `/new` + `/{id}/edit` form screens are
  dropped (they weren't authored). A round-trip test gates the invariant
  `parse(meridian.yml)` ≡ `parse(pack(generate(meridian.yml)))`, so generator↔pack
  divergence is caught as features are added.
- Two supporting fixes the round-trip surfaced: generated entity order now follows
  the blueprint's authored order (was alphabetised); and `entity_list`'s `text:` /
  `empty_text:` are now wired (custom list heading + empty-state copy) instead of
  silently ignored.

### Added: blueprint generates real, full web apps

The blueprint generator now emits **owned Go that composes the full `framework/ui`
catalog** instead of raw HTML, turning a blueprint into a credible product (see
the new `examples/meridian` flagship, a SaaS billing console + marketing site).

- **Server-rendered entity screens.** `entity_list` / `entity_detail` are emitted
  as request-time SSR screens (`RenderCtx`) backed by the entity's `CrudHandler`,
  composing `ui.DataTable` / `ui.PageHeader` with an owned `blueprint/resource.go`
  engine: humanized headers, formatted cells (bool→Yes/No + status badges, enums,
  `$` money, dates), foreign keys resolved to the related record's name, and
  server-side search / sort / pagination + empty states. Replaces the old
  client-fetch raw-`<table>` islands.
- **Full UI catalog as blueprint blocks**: `page_header`, `hero`, `section`,
  `card`, `stat_row`, `stat_card`, `bar_chart`/`pie_chart`, `link_button`,
  `callout`, plus data-bound dashboard widgets: `stat_card`/charts with
  `source: {entity, agg: count|sum, field, group_by, filter}` compute live
  metrics server-side.
- **Marketing + app layouts**: `screen.layout: marketing` uses
  `ui.SiteHeader`/`ui.SiteFooter`; `layout: app` uses the sidebar shell.
- **Auth screens + RBAC gating**: `signup_form` block; `screen.access:
  {auth: true, role: …}` emits an `appui.Policy` that redirects anonymous GETs to
  the login page (with `?next=`) and 403s a signed-in user missing the role.
- **Writable app screens (create / edit / delete).** `entity_list` gains a
  `create: true` flag → a "New <Singular>" button + a synthesized `<route>/new`
  create form; every `entity_detail` gets **Edit** + **Delete** header actions and
  a synthesized `<detail>/edit` form prefilled from the record (enum/relation
  `<select>`s render their options + selection server-side). Forms submit as
  `data-fui-rpc` islands and SPA-navigate back on success. The resource engine
  gained a `Form(ctx, id)` method; the generator installs an `access.RolePolicy`
  (admin role → wildcard) + `access.Middleware` so the gated CRUD API actually
  accepts the signed-in operator's writes instead of 403ing.
- **The generator emits a test for every generated surface.** Each app gets an
  owned `blueprint/resource_test.go` (formatting + input-type helpers) and a
  complete `e2e_test.go` that builds + boots the binary and asserts: the
  home brand; **every** static public screen renders; **every** gated screen
  redirects anonymous callers and renders once signed in; a full
  **create → read (detail + edit) → update → delete** lifecycle against a
  writable entity through its CRUD API + form screens; and that an anonymous
  write to an access-gated entity is refused (RBAC). The create payload is
  synthesized from the entity's required fields, so the suite stays valid across
  schemas.
- A standard `box-sizing: border-box` reset + body theme surface ship in the
  generated base CSS so padded full-width bars don't overflow on mobile.

### Added

- **`core-ui/node` + `core-ui/noderender`**: the JSON-clean UI node tree
  (`Node`, `Action`, tree helpers) and its HTML renderer, extracted from
  `kiln/world` + `kiln/noderender` into first-party `core-ui` packages. The
  blueprint codegen no longer emits any import of the `kiln/*` namespace;
  Kiln consumes the node packages like any other caller (`kiln/world`
  type-aliases `node.Node`/`node.Action`, so kiln-internal code is unchanged).
- **`data-action-mount` runtime primitive**: fires a compiled component
  action once on hydration (and after each SPA nav), so a server-rendered
  island can populate itself on load without a user event.
- **`ScreenGroup.Standalone()`** (`core-ui/app`): marks a screen group as a
  self-contained shell so the host App's default layout does NOT also wrap it.
  `battery/admin` uses it: the back-office mounts on the host's App but renders
  its own sidebar, which previously nested inside the app's sidebar (a
  double-sidebar). Now the admin renders a single, correct shell.
- **Blueprint `app.api_prefix`** (default `"api"`): entity JSON CRUD mounts
  under `/api/<table>`, freeing the bare `/<table>` path for HTML screens.
- **Blueprint login + admin back-office**: a `login_form` screen block renders
  a no-JS HTML sign-in form (posts to the auth battery, redirects on success),
  and `app.admin` wires the admin battery: an editable CRUD back-office over
  every entity at `/admin`, gated by a role, with `seed_email`/`seed_password`
  bootstrapping an admin account on first boot. `battery/admin` gained a
  `LoginPath` config: an unauthenticated GET redirects to the login page
  instead of returning a bare 401.

### Changed

- **Meridian is the flagship example** across the README and the website
  (`/examples`, the home grid). It supersedes ecommerce as the headline blueprint
  demo (ecommerce stays as a second, owner-scoped pipeline example). Acronym field
  labels now read correctly (MRR, ID, URL, …) instead of "Mrr"/"Id".
- **Seed rows generate with sorted keys**, so re-running `gofastr generate` no
  longer churns `blueprint/stubs.go` with random map-iteration order.
- **Blueprint-generated apps are now usable websites end-to-end.** Screens
  routed at an entity's path are no longer shadowed by the CRUD JSON handler
  (the API moved under `/api`); `entity_list` / `entity_form` / `entity_detail`
  blocks render and auto-populate on load (including when nested inside a
  `section`/`div`, which previously degraded to an HTML comment); `enum` fields
  render populated `<select>`s and `relation` fields render selects populated
  from the related entity; forms submit via `data-fui-rpc`; dynamic detail
  routes (`/x/{id}`) resolve; and declared `seed:` data is applied on first
  boot (ordered, idempotent, decimal-coerced, non-fatal on a bad row).
  Generated apps ship a responsive base stylesheet (`BlueprintBaseCSS`) and a
  body font floor so they render in a system/Inter stack instead of the
  browser serif default.

  **BREAKING (generated apps):** regenerated blueprint apps serve entity JSON
  at `/api/<table>` instead of `/<table>`. Set `app.api_prefix: ""` to keep
  the old bare paths. MCP tools and the OpenAPI spec follow the prefix
  automatically.

### Added: per-user data isolation + complete auth UX

- **`owner_field` auto-creates the owner column.** Declaring `owner_field` now
  synthesizes a hidden owner column (AutoMigrate creates it; it never appears in
  generated forms/tables). You no longer hand-declare the field. The generated
  seed runs *as* the bootstrap admin, so demo data is owned and a freshly
  registered user starts with an empty, owner-scoped workspace. Meridian's
  customers/subscriptions/invoices/payments are now per-user (`owner_field:
  user_id`); plans stays a shared catalog.
- **Auth is a full UX, not just a gate.** `ui.SignOut` (a POST logout control)
  in the app sidebar footer and the marketing header; an **auth-aware marketing
  header** (`app.NewContextComponent`) that shows a Dashboard link + Sign out
  when signed in, Sign in when not; a **guest policy** that redirects
  already-signed-in visitors off the login/signup screens.
- **Inline auth errors.** `ui.AuthCard` gained an `Alert` slot, and a failed
  form login now redirects back to the login page with `?error=` (via
  `auth.SetDefaultLoginErrorPath`) and renders "Invalid email or password."
  inline, instead of dumping a raw JSON error body.
- **Role-aware navigation.** A nav item `role:` (→ `ui.SidebarItem.Roles` +
  `ui.SetRolesExtractor`) filters role-gated entries by the signed-in user's
  roles, on both the desktop sidebar and the mobile drawer: a link a user can't
  use never appears (and is never a dead end into a 403).
- **The auth battery self-migrates** its `auth_users` / `auth_sessions` tables
  (`EntityUserStore`/`EntitySessionStore.EnsureSchema`, dialect-aware), so the
  generated app ships **zero** hand-rolled DDL.

### Added: SPA cross-layout navigation + resilient chrome

- **Cross-layout SPA navigation.** The outermost shell carries
  `data-fui-layout`; the route manifest carries each route's layout; the runtime
  detects a layout change (e.g. marketing → app) and swaps the *whole* shell
  instead of just the content, so the new screen renders in the right chrome,
  with no hard reload.
- **Charts render an empty state instead of panicking.**
  `ui.BarChart`/`PieChart`/`LineChart`/`Sparkline` no longer crash the page on
  empty data (a zero-data user's dashboard previously 404'd behind the SSR host's
  panic recovery); they show a calm "No data yet" placeholder.
- **`ui.SiteHeader` collapses its actions into the mobile drawer**, so an auth
  header no longer overflows a phone bar; the bar becomes brand + hamburger.
- **`app.NewContextComponent`** + ctx-aware widget chrome
  (`component.RenderComponentCtx`, `serveChrome` renders with the request
  context), so per-request chrome (role-aware nav drawer, auth-aware header) sees
  the signed-in user.
- The app shell drops its empty top bar; the theme toggle lives in the sidebar
  footer (visible on desktop, in the drawer on mobile).

### Fixed

- **The framework-managed owner column is persisted on create/upsert.**
  `doCreate`/`doUpsert` skipped Hidden/ReadOnly fields when building the INSERT,
  silently dropping the owner id `InjectOwner` stamps, so owner-scoping matched
  nothing and a seeded admin's rows were invisible. The owner column is now
  exempt from the skip.

## [0.6.1] - 2026-06-12

Patch release fixing two docs-site (`examples/site`) bugs. No breaking changes,
no framework API changes.

### Fixed

- **The site version is stamped from the deployment's git tag instead of a
  hand-bumped constant.** `examples/site` displayed a hardcoded `siteVersion`
  that had drifted to `0.4.0` while releases moved on. It is now injected at
  build time via `-ldflags "-X main.siteVersion=$(git describe --tags
  --abbrev=0)"` (wired into `scripts/dev-watch.sh`, `make build-examples`, and
  the Pages workflow, which now checks out with tags), so the deployed site
  always matches the tag it was built from. An un-stamped local `go build`
  shows `dev`.
- **The Sidebar showcase's mobile nav drawer now opens.** At < 900px the
  `/components/sidebar` demo rendered a hamburger wired to `ui-sidebar-drawer`,
  but that drawer widget was never mounted, so the button silently did nothing.
  The drawer is now mounted (page-scoped) sharing the showcase's sidebar config,
  and a contract test (`TestE2E_Sidebar_HamburgerOpensDrawer`) covers it.

## [0.6.0] - 2026-06-12

Reframes the blueprint as a **generator, not a source of truth**. `gofastr
generate` now scaffolds owned Go in an idiomatic, module-root layout you read,
edit, and commit: there is no quarantined `gen/` directory and no `// Code
generated … DO NOT EDIT.` header on the scaffold. Re-running the generator is
add-only and never clobbers your edits. Contains two **BREAKING** changes (see
below); pin a version (`go get …@v0.6.0`).

### Changed

- **BREAKING: `gofastr generate` scaffolds into the module root, not `gen/`.**
  A blueprint now scaffolds `main.go` plus `entities/` and `blueprint/`
  subpackages at the module root (imports rooted at your module), as owned Go
  with no `// Code generated … DO NOT EDIT.` header. Writes are **conflict-skip**:
  a re-run writes new files but never overwrites a file you have hand-edited;
  pass `--force` to overwrite. The blueprint is an on-ramp; once scaffolded the
  generated Go is the source of truth and the running app does not need the
  `gofastr.yml`. **Migration:** to keep the old quarantined layout, pass
  `--out=gen` (or set `app.output_dir: gen` in the blueprint); output is still
  owned Go, just in a subpackage. Build/run with `go run .` (or `go run ./<dir>`
  when `--out` is used) instead of `go run ./gen`. Monorepo examples that host a
  test package alongside the app use `output_dir` for a subpackage;
  `examples/ecommerce` now scaffolds into an owned `app/`.

- **BREAKING: `gofastr migrate diff` has been removed.** It applied a blueprint
  directly onto a live database, reconciling the running schema to the
  blueprint, i.e. treating the blueprint as authoritative over the world. Code
  generation and schema migration are separate concerns. **Migration:** use
  `gofastr migrate generate <name>` to emit a reviewable, versioned migration,
  then `gofastr migrate up` to apply it; additive column changes also converge
  on boot via `AutoMigrate`. `migrate generate` is unchanged and still accepts
  `--from=<blueprint.yml>` as an opt-in schema source.

## [0.5.1] - 2026-06-11

Patch release correcting startup readiness reporting and repository release
metadata. No breaking changes.

### Fixed

- **Startup readiness output now follows the listener bind.** `App.Start`
  prints its framework banner only after `net.Listen` succeeds, uses the
  resolved address for a new `Listening:` line, and prints API-prefixed entity
  URLs correctly. Bind failures no longer claim the server is ready.
- **Repository status surfaces match `v0.5.0`.** The security support line,
  codegen Make target, roadmap implementation statuses, architecture version,
  coverage example path, and API-versioning package paths were corrected.
  Obsolete roadmap-worktree scripts were removed, and `repolint` now guards
  supported-version drift plus retired build-script paths.

## [0.5.0] - 2026-06-10

The first-contact release: an adversarially-verified 10-dimension audit
(2026-06-09) found the engine strong but the first-touch surface broken:
the README quickstart failed verbatim, the flagship example shipped
insecure, RBAC was unreachable from the blueprint, and CI was red on every
release tag. Everything below closes those findings. Contains a **BREAKING**
auth change (see below); pin a version (`go get …@v0.5.0`).

### Changed

- **BREAKING: `battery/auth` fails closed on an empty `JWTSecret` in
  production.** An `AuthManager` with `DevMode=false` and no `JWTSecret`
  now makes `Init` return an error (the app refuses to boot) instead of
  warning and continuing; an empty HMAC key yields forgeable,
  restart-unstable JWTs. The error names the remedy (set
  `AuthConfig.JWTSecret`, or `DevMode: true` for local HTTP). The
  blueprint path rejects `app.auth` with `dev_mode: false` and no
  `jwt_secret` at `gofastr validate`/`generate` time, so a generated app
  can't be built into the broken state. **Migration:** set
  `AuthConfig.JWTSecret` from your secret store (most prod apps already
  do); `DevMode` is unchanged and still mints a per-process secret.

### Added

- **Blueprint `access:` key: per-operation RBAC from `gofastr.yml`.** An
  optional `access:` map on an entity (`read` / `create` / `update` /
  `delete`, each a permission string) threads through
  `EntityDeclaration.Access` into the generated `register.go` as
  `framework.AccessControl{…}`, where the existing CRUD chokepoint enforces
  it fail-closed (403). Fully additive: blueprints without the key produce
  byte-identical output. Closes the audit's "one leg of the secure-by-default
  triad is unreachable from the primary declaration format". Also re-exports
  `framework.AccessDeclaration` for symmetry with `EntityDeclaration`.
- **`gofastr validate <blueprint.yml>`.** Parse + full blueprint validation
  (including the module/go.mod coherence check, a render pass, and an
  entity-name check that rejects names whose generated Go identifier would
  not compile, e.g. `2fa_tokens`) without generating; exit 0/1 with
  agent-friendly file:line + remedy diagnostics. `--from`, `--config`, and
  `--out` on `gofastr generate` now also accept the space form
  (`--from x.yml`), which previously silently did nothing.
- **`unscoped-pii` lint: CLAUDE.md hard rule #6 in the toolchain.** Flags
  any auto-exposed entity (CRUD default-on or `mcp: true`) with PII-shaped
  fields and no `owner_field` / `access` / `multi_tenant`. Enabling
  `app.auth` alone does **not** suppress it: the session middleware is
  pass-through for anonymous requests (an adversarial review proved the
  original auth-suppression wrong by anonymously reading user emails from
  a generated example app). Error from `gofastr validate`, prominent
  warning from `gofastr generate`, finding in `gofastr audit lint`.
  Running it against the repo's own examples found and fixed shipped
  exposures in `blog`, `lms`, `real-estate`, `portfolio`, and
  `project-manager`: each now demonstrates a scoping pattern (RBAC-gated
  staff rosters, public-catalog reads with gated writes, lead-capture
  forms with open create and gated reads).
- **Executable-README CI gate.** `cmd/gofastr/readme_quickstart_test.go`
  extracts the README's quickstart blocks and runs them for real: blueprint
  → generate → build → boot → `GET /posts` 200; plus drift gates (no
  "unpublished"/replace-directive guidance anywhere in the embedded docs,
  README blueprint relations must resolve). The audit found this single
  gate would have caught five of its eight confirmed findings.
- **`App.OnReady(func(addr string))`**: lifecycle hook that fires after
  the listener has actually bound (and is skipped on start failure).
  Generated apps now print their startup banner from it, so a migrate
  failure can no longer print "Server starting" and then exit 1.
- **Trust surface.** `SECURITY.md` (private vulnerability reporting,
  honest v0.x support policy), `CONTRIBUTING.md` (truthful prereqs: Docker,
  Chrome, the test-isolation rules), and low-ceremony issue templates.
- **Docs: `comparison.md` and `tutorial-blueprint-app.md`.** An honest
  head-to-head (PocketBase / Encore.go / Wasp / hand-rolled Gin+sqlc,
  weaknesses included) and the missing thesis tutorial (blueprint →
  generate → secure with auth + `owner_field` + `access:` → customize in
  plain Go → deploy), every step executed end-to-end before shipping.
  Plus a README for `examples/ecommerce`.

- **In-package Postgres coverage for `framework/crud`.** The SQL-generation
  core had 66 sqlite-only test files; a focused testcontainers suite now
  exercises the representative paths (filters, sort, offset+COUNT, cursor
  keyset walk, batch rollback, upsert, eager-load include, soft-delete,
  owner+tenant fail-closed) against real Postgres.
- **First tests for `framework/owner`** (14): fail-closed verified at the
  HTTP gate (anonymous → 401 with no extractor), last-call-wins override
  warns, OwnerField-unset is inert, forged client `user_id` overridden,
  race-checked. Two layer-level fail-open contracts (`ApplyOwnerScope`,
  `InjectOwner` without an extractor) are pinned with tests and comments
  documenting that `requireScope` upstream is the actual boundary.
- **Common-mistakes callouts completed and gated.** The docs claimed every
  topic ends with one; 21 of 60 didn't. Real, code-verified callouts added
  to the 15 guide docs (including entity-declarations, "the heart of the
  model"); 6 data/index docs exempted with reasons; the claim text now
  matches reality and `TestGuideDocsEndWithCommonMistakes` enforces it.
- **Security ledger fully re-verified.** All 103 SECURITY_FINDINGS.md rows
  re-checked against current code: 102 `fixed` (each with the mitigation
  cited AND a named pinning test run and observed passing), 1 `accepted`
  (#58, an intentional accepted-risk documented in code), 0 unverified or
  open. A guard test pins header-count == row-count and valid status
  tokens (`fixed`/`open`/`needs-verification`/`accepted`).
- **Coverage floors are a CI gate.** `scripts/coverage-floors.sh` fails the
  blocking job if any claimed package drops ~2 points below its measured
  coverage. COVERAGE_NOTES.md now separates own-package numbers from the
  full-suite-overlay numbers the old 100% claims were quoting.

### Fixed

- **SECURITY: `EntityConfig.MaxListLimit` could be bypassed on two list
  paths.** The cursor path never consulted the entity cap (asked for ≤3,
  served up to 100), and an oversized `?limit` on the offset/stream path
  silently fell back to the default page size (20), exceeding any cap
  below 20. Both were hidden behind the security tests' auto-skip-on-500
  heuristic; converting those skips to hard failures (an audit
  recommendation) exposed them immediately. Oversized `?limit` now clamps
  to the effective cap on every path (`listLimitCap` shared by offset,
  stream, and cursor), with regression tests un-skipped and green.
- **crud security tests fail instead of skip on server errors.** The
  skip-on-redacted-500 heuristic (and its "SQLite can't run $N
  placeholders" rationale, which was false: the 500s were fixtures
  missing `Table`) is gone across the suite.
- **`battery/queue` tests are deterministic.** Thirteen sleep-based
  assertions replaced with Close-drain semantics, bounded `waitFor`
  polling, and an unexported clock seam for lease/visibility expiry;
  stable under `-race -count=3`; no production behavior change.
- **Doc staleness found while verifying the new callouts:** `widgets.md`
  described a `widget.Mount` return value and bootstrap route that don't
  exist (rewritten around `MountRuntime`/`RuntimeTag`);
  `ui-new-components.md` cited three nonexistent drift gates (corrected to
  the real ones); `ui-getting-started.md` had a non-compiling
  `DBFromContext` snippet and listed `APIPrefix` as roadmap (it shipped).
  `cursor-pagination.md` now documents the per-entity cap.

- **Boot-time auto-migrate now adds missing columns.** `AutoMigrate` did
  `CREATE TABLE IF NOT EXISTS` only, while deploy.md claimed
  "create tables, add columns"; adding a field to an existing entity
  broke the next boot. It now reuses the existing schema-diff machinery
  and applies the **additive** changes only (drops/renames/retypes stay
  behind `migrate diff`'s destructive gate); required new columns are
  added nullable, column adds run before index DDL, and a racing replica
  re-reads live columns on the lock-holding transaction and no-ops.
  Also fixes Postgres live-schema readers to case-fold unquoted table
  names (mixed-case entities were mis-reported as missing every boot).
- **Light color scheme now passes WCAG AA, and the axe gate can no
  longer be platform-blind.** The "Linux-only" CI axe failures were real:
  Linux Chrome defaults to `prefers-color-scheme: light`, so CI audited
  the light palette that Dark-mode dev Macs never did. Light primary/
  accent/code-comment tokens retoned (worst offender was 1.71:1, now all
  ≥4.6:1), the framework's `DefaultTheme` status tones darkened so any
  light-theme Badge/Tag chip passes AA, the `gofastr theme init` scaffold
  updated to match, and `TestAxe_AllPagesAreClean` now scans every page
  under BOTH forced schemes. The browser-e2e CI job is **blocking** again.
- **Generated auth honors `dev_mode` and validates it strictly.** The
  generator hardcoded `DevMode: true`; the blueprint key now works, with
  a deliberate default of `true` (production cookie posture,
  `__Host-session` + `Secure`, never round-trips on the plain HTTP a
  fresh app serves) announced in the generated code, the `gofastr
  generate` output, and the docs. `dev_mode: yes` is a hard error, not a
  silent coercion to prod mode. `auth.CSRF` is deliberately not mounted
  (it would 403 the JSON/MCP surface); the gap and the
  `SameSite=Strict` mitigation are documented.
- **Docs-site copy drift caught by visual review.** The site header
  advertised "pre-alpha 0.0.4" and the examples pages described blog as
  "JSON-declared", a format removed in v0.4.0. Version now lives in one
  `siteVersion` constant; blog is correctly described as Go-declared
  (users/posts/comments).
- **The README quickstart now runs verbatim, and CI enforces it.** The
  blueprint example was rewritten in block style (the in-house YAML parser
  deliberately rejects flow mappings) with every referenced entity
  declared; a long-unclosed code fence that inverted every subsequent
  block is closed; stale "unpublished / add a replace directive" guidance
  is gone (the module resolves on the proxy at v0.4.0); the
  `go test ./...` claim now states its real prerequisites.
- **Flow mappings fail with an honest error.** `core/yaml` now says
  `flow mapping "{ ... }" is not supported; use block style …` instead of
  the misleading "nested mapping must be on an indented line"; flow-list
  items that previously silently misparsed now error.
- **Relation-typed fields are validated at generate time.** A field like
  `author_id: {type: relation, to: users}` pointing at an undeclared
  entity now fails `gofastr generate`/`validate` with a remedy, instead of
  generating an app that exits 1 at startup. Blueprint `module:` that
  contradicts the enclosing `go.mod` errors with the exact fix.
- **Generated apps with auth actually authenticate.** The generator
  enabled the auth battery but never mounted `auth.SessionMiddleware`, so
  authorized requests got 401 like anonymous ones (found by dogfooding the
  secured flagship). Generated `app.go` now mounts it after
  `authMgr.Init`; the flagship test asserts the full register → login →
  create → owner-isolated list flow across two users.
- **`examples/ecommerce` no longer ships insecure.** Auth is enabled and
  `orders` / `order_items` are owner-scoped (`owner_field: user_id`);
  previously any anonymous caller or MCP agent could read every customer's
  PII and mutate orders, violating the repo's own hard rule #6.
  `BUILD_JOURNAL.md` keeps the honest history.
- **Deterministic CI.** `core/static` MIME detection now consults its
  canonical extension table before the host mime database, so Content-Type
  is identical on macOS and Linux (the 6/6-failing `TestDetectFromName` is
  fixed at the detection layer, not the test); `.js`/`.mjs` serve the
  RFC 9239 canonical `text/javascript`. `ci.yml` is restructured into a
  blocking deterministic job and an isolated, serialized browser-e2e job
  (non-blocking until the known Linux-Chrome axe contrast discrepancy in
  `examples/site` is resolved; condition documented in the workflow).
- **Docs drift purged.** `overview.md` core/ package count corrected
  (twelve → eighteen); `framework/ARCHITECTURE.md`'s layering map redrawn
  to match the real import graph (`openapi` above `crud`, the
  `slowquery → db` edge, `crud → access`); `core-ui/ARCHITECTURE.md` now
  states the real runtime size (~7,400 lines across budget-enforced split
  modules, ≤12KB-gz core) instead of "a few hundred lines";
  `examples/README.md` gained the flagship row.

### Changed

- **Example blueprints declare their real module paths.**
  `blog`/`lms`/`portfolio`/`project-manager`/`real-estate` now declare
  `module: github.com/DonaldMurillo/gofastr/examples/<name>` (matching the
  flagship), so `gofastr validate` passes in-place inside the repo.
- **Repo-wide gofmt sweep + CI gate.** 299 tracked files reformatted in one
  mechanical pass; the blocking CI job now fails on any gofmt drift in
  tracked Go files.
- **README repositioned around the wedge.** Leads with "one blueprint
  becomes a server-rendered UI and an API with secure scopes, in plain Go
  you own"; MCP/OpenAPI demoted to supporting evidence (schema-derived MCP
  became table stakes); validation-status block updated for the secured,
  CI-gated flagship.

## [0.4.0] - 2026-06-08

The blueprint becomes GoFastr's single declaration format: the legacy
`entities/*.json` path is removed (**BREAKING**), and a declaration-driven
flagship (`examples/ecommerce`) proves one `gofastr.yml` → SQL + REST + OpenAPI
+ MCP + UI end to end.

### Added

- **Declaration-driven flagship example: `examples/ecommerce`.** A complete
  storefront (five related entities, screens, nav, custom endpoints, seed data,
  and a theme) declared once in `gofastr.yml` and emitted as runnable Go by
  `gofastr generate --from=gofastr.yml` (the generated `gen/` is gitignored).
  `flagship_test.go` regenerates, builds, and runs it to prove every surface,
  SQL schema, REST CRUD, OpenAPI, the 25-tool MCP surface, and the
  server-rendered UI, is live with zero hand-written application code. See
  `examples/ecommerce/BUILD_JOURNAL.md`.

### Fixed

- **`gofastr generate` now gofmt's its generated Go.** Blueprint output is run
  through `go/format` before being written, so the emitted package is clean and
  stable across regenerations (no more spurious diffs on re-`generate`).
- **`gofastr generate --from` re-run no longer refuses to clean `main.go`.** The
  output-dir cleaner now owns `main.go` (the blueprint emits `gen/main.go`), so
  regenerating over an existing `gen/` succeeds instead of erroring with
  "refusing to clean — contains unknown entry".

### Removed

- **BREAKING: the legacy `entities/*.json` declaration format is gone.** The
  `gofastr.yml` blueprint is now the single declaration format: it decodes into
  the same `EntityDeclaration` shape and additionally emits `main.go`, screens,
  and stubs, so the JSON-file path was a strict subset. Removed:
  - Framework API: `App.EntityFromFile`, `App.EntitiesFromDir`,
    `App.GroupEntitiesFromDir`, `framework.LoadEntityDeclaration`,
    `framework.LoadEntityDeclarations`. (The `EntityDeclaration` /
    `FieldDeclaration` types and `.Config()` remain; they are the in-memory
    shape the blueprint loader decodes entities into.)
  - CLI: `gofastr generate entity <name>` and `gofastr new entity <name>` (both
    now print a removal notice and exit non-zero); the `--entities=<dir>` flag
    on `gofastr generate`, `gofastr migrate generate`, and `gofastr migrate diff`.
  - `gofastr generate` no longer defaults to "scan `entities/` and generate." It
    requires `--from=<blueprint.yml>` (or a `gofastr.codegen.yml` extension
    config). Auto-discovery of `gofastr.yml` is intentionally not done; that
    filename is also the `gofastr init` isolation config.

  **Migration:** declare entities in a `gofastr.yml` blueprint and run
  `gofastr generate --from=gofastr.yml`, or declare them in Go via
  `app.Entity(name, framework.EntityConfig{…})` (unchanged). `gofastr migrate
  generate <name> --from=<blueprint.yml>` and `gofastr migrate diff
  --from=<blueprint.yml>` replace the old `--entities=<dir>` form. The
  `gofastr.codegen.yml` extension protocol and `codegen` package are unchanged.

  _Follow-up (kiln is experimental): `kiln freeze` still writes
  `entities/*.json` as its own snapshot artifact; emitting a `gofastr.yml`
  blueprint directly is tracked for a later pass._

## [0.3.3] - 2026-06-08

The four larger features held back from v0.3.2, each additive and
backward-compatible. The OAuth token store passed the mandatory dual-model
security audit (see `AI_TEST_AUDIT.md`).

### Added

- **Typed schemas for custom `entity.Endpoint`.** New optional
  `InputSchema`/`OutputSchema` (`[]schema.Field`) fields. When set, the OpenAPI
  spec emits a typed `requestBody`/`200` response and the generated MCP tool
  advertises a typed input schema, instead of a shapeless `{type:object}`. A
  single helper (`openapi.EndpointInputSchema`) feeds both the OpenAPI and MCP
  paths. Endpoints with no schema render exactly as before.
- **OAuth2 token store + transparent refresh** (`battery/auth`). A new
  `OAuthTokenStore` interface + AES-GCM-sealed `SQLOAuthTokenStore` persists
  `{access, refresh, expiry}` per `(user, provider)`; `RefreshOAuthToken` /
  `ValidOAuthToken` refresh transparently on/near expiry via the provider's
  refresh grant (Google + GitHub). OAuth login now persists the refresh token
  (previously discarded) when a store is wired. **Opt-in**; login is unchanged
  with no store configured. `EncryptionKey` is **required** (fails closed); the
  `userID` passed to refresh/valid must be the authenticated principal.
- **Cron-expression scheduling in the queue Scheduler.** `Scheduler.Cron(spec)`
  fires on a standard 5-field cron expression (plus `@daily`/`@hourly`/… shortcuts),
  alongside the existing `Every(interval)`. Reuses `framework/cron` (now exposing
  `Parse`/`Schedule.Next`); no second cron parser. Interval schedules are unchanged.
- **Request context in i18n-rendering `framework/ui` components.** `RepeaterConfig`,
  `LightboxConfig`, `StepWizardConfig`, `PasswordInputConfig` gain an optional
  `Ctx` field so their localizable strings resolve the request's locale instead
  of always rendering the default. Nil `Ctx` preserves today's behavior.

## [0.3.2] - 2026-06-08

A developer-experience patch from the same whole-framework assessment that
drove v0.3.1: twenty DX improvements and small features, all with tests and
 shipped docs. No BREAKING changes; everything is additive.

### Added

- **`App.WithSeed(func(ctx) error)`**: register seed funcs that run AFTER
  auto-migration (tables exist) and before the listener binds, fixing the
  first-run "no such table" footgun.
- **`framework.DBFromContext(ctx)` / `WithDBContext`** + an auto-wired
  `DBContextMiddleware`: screens reach the app's `*sql.DB` from the request
  context instead of a package-level global handle.
- **`access.GetRoles(ctx)`** (and the `framework.GetRoles` facade): the reader
  half of the role-context seam, for role-based UI branching.
- **`PluginGetAs[T]`**: typed plugin lookup mirroring the existing `GetAs[T]`.
- **Typed interactive effects**: `Confirm`/`AfterText`/`AfterDisable`/`ScrollTo`/
  `PushState` builders in `core-ui/interactive`, replacing hand-written
  `data-fui-*` attribute strings.
- **`ListOptions.NestedFilters`**: in-process `ListAll`/`CountAll` now apply the
  same `?author.name=alice` EXISTS-subquery nested filters the HTTP path does.
- **`RedisQueue.Start(ctx, interval)`**: background reclaim ticker recovers jobs
  stranded by a crashed worker, matching `DBQueue`.
- **`Battery` wrappers for cache/search/storage** (`NewBattery`) with clean
  lifecycle shutdown of background goroutines.
- **`gofastr harness creds add|list|delete`**: store credentials in the
  encrypted credstore; `gofastr --help` now lists the `harness`/`agents` subcommands.
- **`audit_log.tenant_id`**: a nullable column (idempotent `ADD COLUMN`) stamped
  from the request tenant, so multi-tenant audit trails are scopeable.

### Changed

- **The OpenAPI spec advertises `?fields=` (projection) and `?trashed=`** query
  parameters so SDK generators and agents can see them.
- **Auto-CRUD registration pre-flights entity/screen path collisions** with an
  actionable diagnostic (names the entity, the colliding path, and the fixes)
  instead of the opaque ServeMux `/foods/llm.md conflicts` panic.
- Queue `Queue`/`Browsable`/`Replayable` interface assertions moved into source
  files (fail at build, not test-link).
- The `agents.md` snippet validator now understands interface methods and
  non-`New` constructors (e.g. `embed.Open` → `Index`), so it stops
  false-flagging correct interface APIs while still catching fictional methods.

### Documentation

- New **`queue.md`** and **`testkit.md`** reference pages; **`battery/embed`** now
  ships `agents.go`/`agents.md` so semantic search is discoverable to agents.
- Documented the typed list/get hooks (`OnBeforeList`/`OnAfterList`/`OnBeforeGet`/
  `OnAfterGet`) and a consolidated hook-skip matrix.
- Security docstrings on the unscoped `softdelete.Restore`/`ForceDelete`/`WithTrashed`
  helpers; "Common mistakes" sections (form-module, api-versioning); deeper
  observability docs; `GetRoles`/`PluginGetAs` docs; and stale-claim fixes.

## [0.3.1] - 2026-06-08

A correctness and developer-experience patch from a whole-framework
assessment. No BREAKING changes. Twenty fixes, all with regression tests;
the recurring theme was converting silent wrong answers into correct
behavior or loud errors.

### Security

- **Codegen and blueprints no longer drop `OwnerField`.** `renderEntityRegistration`
  emitted every scope flag except `OwnerField`, and the blueprint YAML allow-list
  rejected `owner_field` outright, so generated/blueprinted apps silently lost the
  per-user row scoping the docs hard-warn about. Both paths now preserve it.
- **Streaming list can no longer bypass `AfterList` redaction.** `?stream=true`
  skipped include resolution and the `AfterList` hook; an `AfterList` redactor would
  have been silently bypassed, leaking the fields it exists to hide. An explicit
  stream with `?include=` or a registered `AfterList` hook is now refused with `400`;
  an auto-stream (very large `limit`) falls back to the buffered path so redaction
  always runs.
- **`GOFASTR_HARNESS_MACHINE_KEY` no longer silently downgrades.** Only a raw 32-byte
  value was accepted; a hex or base64 key failed the length check and fell through to
  the default passphrase with no warning. The env var now decodes raw-32/hex-64/base64
  and errors loudly on an unparseable or wrong-length value.
- **The OpenAPI spec advertises `401`/`403` on RBAC-gated, batch, and SSE operations.**
  `EntityConfig.Access` is folded into the gated flag and `403` is added alongside
  `401`, so generated SDKs/agents see the real auth contract instead of treating
  RBAC-gated routes (and `_batch`/`_events`) as public.
- The `UpsertOne` DO-NOTHING fallback `SELECT` now applies tenant/owner/soft-delete
  scope (defense-in-depth; `upsertPreflight` already guarded the row).

### Fixed

- **`updated_at` is restamped on every UPDATE and bulk update.** It previously froze
  at its creation value because the field loop skips all auto-generate columns; cache
  invalidation and change detection silently saw stale timestamps. Clients still
  cannot forge it.
- **`ADD COLUMN` for a required field with no default no longer emits `NOT NULL`.**
  That DDL fails on a populated table (Postgres and old SQLite); the column is now
  added nullable with the deferral noted in the change summary (matches the kiln path).
- **`App.InTx` joins an ambient transaction** already in the context (e.g. when called
  from a CRUD hook) instead of silently opening a second independent transaction and
  breaking atomicity.
- **DSL `after(cursor)` is wired into `BuildDSLQuery`.** It was parsed and discarded,
  so DSL pagination always returned page 1. Composite-cursor/unknown-field entities now
  return a clear error instead of no-oping.
- **LiveSearch debounce works.** The emitted attribute (`data-fui-rpc-debounce`) did not
  match what the runtime reads (`…-ms`); debounce was silently ignored.
- **Widget dismiss closes its `EventSource`s** instead of leaking a live server SSE
  connection on every modal open/close.
- **Signal ARIA is text-mode only.** `role=status`/`aria-live` is no longer applied to
  attribute- or html-mode signal nodes (invalid ARIA + live-region spam on island swaps).
- **Carousel timers and the toc `IntersectionObserver` are torn down on SPA navigation**
  instead of leaking for the session.
- **`RedisQueue` implements `Browsable`** (`ListJobs`/`Stats` over the dead-letter list),
  so the admin queue page works on the most common non-DB production backend.
- **Scheduler enqueue failures log via `slog`** instead of `fmt.Printf`, surfacing
  otherwise-invisible job loss to the log battery/observability.
- **`MemoryQueue` handler timeout is configurable** via `WithHandlerTimeout` (default
  unchanged at 30s) so long jobs aren't silently cancelled and dead-lettered.
- **Per-page Open Graph/meta beats the global default.** Per-screen SEO is emitted before
  the sitewide `WithOpenGraph` tags, so first-match crawlers honour the page override.
- **`gofastr new entity` and `generate entity` agree on table naming** (singular
  snake_case, matching the framework default) so migrations target one table.
- **Built-in harness profiles are embedded** (`go:embed`, on-disk-wins fallback), so
  `gofastr harness --framework` works for an installed binary outside the source tree.

### Documentation

- Corrected the `access.Policy` interface in the docs from a non-existent 3-arg
  `Can(ctx, permission, resource)` to the real 2-arg `Can(ctx, permission)` (custom
  policies following the docs failed to compile), and documented that per-record
  decisions are made via `OwnerField` scoping or `Before*` hooks. A compile-time
  assertion now pins the doc to the interface. The Go interface is unchanged.
- Documented the streaming/`AfterList` exclusivity, `App.InTx` ambient-tx joining,
  the `ADD COLUMN` `NOT NULL` deferral, and corrected the stale `updated_at` hook
  comment in `migrate.go`.

## [0.3.0] - 2026-06-07

First release after the assessment-driven remediation. Highlights: MIT
LICENSE; secure-by-default authorization (multi-tenant fail-closed,
per-operation RBAC on auto-CRUD, admin default-deny); kiln free-order
authoring + same-origin guard; observability + deployment story; durable
auth token store; dead-letter replay across all queue backends; and a
broad sweep of build-quality fixes. **Contains BREAKING changes: read the
entries marked BREAKING below before upgrading from v0.2.x.**

### Security

- **BREAKING: typed-repo queries are now tenant-fail-closed.** A re-audit found
  `Repo.Query().Find/First/Count/Exists/UpdateAll/DeleteAll` (the generated
  typed-query builder) only applied `ApplyTenantScope`, which no-ops on an empty
  tenant, so on a `MultiTenant` entity a tenant-less context read/mutated across
  every tenant. The in-process `crud_api.go` already gated this; the typed-query
  path slipped. Now gated via `requireTenantContext` (honors
  `tenant.AllowCrossTenant`). Owner scope stays permissive for typed repos by
  design (trusted in-process; admin reads across owners).
- **The SSE `_events` live feed now enforces `Access.Read`.** The real-time feed
  is a read surface but skipped the per-op RBAC gate, so an authenticated user
  without the read permission could subscribe for a live stream of all writes
  despite `403` on the static read endpoints. `EventStream` now runs
  `requirePermission(opRead)` alongside the owner/tenant gates.
- **kiln: same-origin guard on the unauthenticated tool API.** `POST
  /kiln/tool/{name}`, `/kiln/agent`, and `/mcp` mutate the in-memory world with
  no auth (loopback bind is the primary control). A new origin guard refuses
  cross-origin browser POSTs (DNS-rebinding / CSRF from a page in the user's
  browser), while non-browser clients (agent, curl, MCP/ACP; no `Origin`) are
  unaffected. Docs: `kiln.md`.
- **`battery/auth` warns on a missing production JWT secret.** With
  `DevMode=false` and an empty `JWTSecret`, the auth battery now logs a loud
  startup warning (an empty HMAC key means forgeable, restart-unstable
  sessions). DevMode still auto-mints a per-process secret, also warned. New
  secrets guidance in `deploy.md` (env injection, Vault/SSM/K8s).
- **`migrate.View` name is validated as a SQL identifier.** `View.Name` was
  interpolated into `CREATE/DROP VIEW` DDL verbatim; it's now checked with
  `query.SafeIdent` and panics on an unsafe name (developer misconfig, fail-fast).
  `View.Select` remains intentionally free-form developer SQL.
- **BREAKING: admin battery is default-deny for non-admins.** With no custom
  `Config.Authorize`, the admin now requires an authenticated user holding the
  admin role (`Config.AdminRole`, default `"admin"`), detected via the
  structural `GetRoles() []string` interface (`battery/auth.User` satisfies it).
  Previously any authenticated, non-nil user reached full admin CRUD over every
  exposed entity, so a freshly-registered reader was effectively an admin.
  Authenticated-but-unauthorized now returns `403` (vs `401` for anonymous).
  Docs: `framework/docs/content/admin.md`.
- **Per-operation RBAC on auto-CRUD: `EntityConfig.Access`.** Declare the
  permission required for each operation (`Read` covers List+Get, plus
  `Create`/`Update`/`Delete`) and auto-CRUD refuses requests lacking it with
  `403`, across List/Get/Create/Update/Delete and the batch/stream variants.
  Previously auto-CRUD had **no permission check at all**: exposing an entity
  granted every authenticated user full CRUD unless the host hand-composed
  route-group middleware. New seams: package-level **`access.Can(ctx, perm)`**
  and **`access.Middleware(policy, roles)`** (re-exported as `framework.Can` /
  `framework.AccessMiddleware`) to install policy+roles in one line. **BREAKING:**
  `access.Policy.Can` / `RolePolicy.Can` drop the unused `resource any`
  parameter. Docs: `framework/docs/content/access-control.md`.
- **BREAKING: multi-tenant CRUD is now fail-CLOSED over HTTP.** A
  `MultiTenant` entity served with no tenant id in the request context is
  refused with `401` on every operation (list/get/create/update/delete, batch,
  stream, SSE), matching the in-process CRUD API which already failed closed.
  Previously the HTTP path failed *open*: an empty tenant id disabled filtering
  and returned/mutated every tenant's rows, a silent cross-tenant data leak.
  Deliberate cross-tenant access (admin tooling) must now opt in explicitly and
  server-side via the new **`tenant.AllowCrossTenant(ctx)`** marker (never from
  a client header). New seam: **`CrudHandler.RequireTenant(w, r)`**, the HTTP
  mirror of `RequireOwner`, run alongside the owner gate through a single
  `requireScope` chokepoint. Docs: `framework/docs/content/multi-tenant.md`.

### Fixed

- **`battery/embed`: custom `Store` now fails closed instead of silently
  corrupting.** A custom `Store` (anything but the built-in `FlatStore`) was
  type-asserted to `*FlatStore` in four places, so with one it would silently:
  skip persistence even with `Options.Path` set; **never purge keyword entries
  on delete** (stale hits leak forever); and **drop every keyword hit** so
  hybrid search degraded to vector-only. Replaced the assertions with optional
  capability interfaces (`Snapshot`/`LoadSnapshot`; `ChunkIDsForDoc`/`ChunkByID`/
  `AllChunks`) and made `Open()` **return an error** when `Path`/`Keyword` is set
  but the store lacks the capability. `FlatStore` implements all of them, so no
  in-tree caller changes.
- **Blueprint codegen produces compilable Go in two edge cases.** (1) An
  endpoint with no `handler` emitted `func (w http.ResponseWriter, r *http.Request) {`.
  Read by Go as a method with two receivers; the handler name now falls back
  to the endpoint `name`, and a fully-anonymous endpoint is skipped. (2) A screen
  whose body was only freeform node blocks imported `core-ui/html` without using
  it (a build error). Import accounting now only flags `html` when a top-level
  block actually emits an `html.*` call. Both are pinned by tests that parse /
  build the generated output.
- **Generated apps no longer ship Kiln's authoring engine.** `gofastr generate`
  emitted `import "…/kiln/render"` into blueprint apps that use freeform node
  blocks, which transitively pulled `kiln/expr`, `kiln/effect`, and `framework`,
  Kiln's whole build-mode evaluator, into a shipped binary. `RenderNode` is
  now a leaf package **`kiln/noderender`** (imports only `core-ui/html`,
  `core/render`, `kiln/world`); codegen targets it and `kiln/render` keeps a
  thin re-export for the live path. A new codegen build test compiles a
  generated node app and asserts its dependency graph excludes the engine.
- **UI host warns when chrome can't be injected.** The host injects the
  runtime, color-scheme bootstrap, SEO head, and widget chrome via
  `strings.Replace` on `<head>`/`</head>`/`</body>`. A custom layout missing one
  of those markers made the replace a silent no-op, shipping a subtly broken
  page. Injection now routes through a guard that logs a warning naming the
  missing marker. Unit-tested.
- **Island SSE drops are now observable.** When a client's island-update
  channel is full the update is dropped (slow consumer); this was silent. The
  manager now counts drops, exposed via `island.Manager.DroppedUpdates()`;
  wire it to a metric/health check to detect stalled streams.
- **`battery/cache`: bounded cache buffering.** The middleware buffered the
  entire response in memory before deciding cacheability, with no size cap; a
  pathological large response could pin unbounded memory. It now streams a
  response past `DefaultMaxCacheableBytes` (8 MiB) straight to the client and
  skips caching it. New `CacheMiddlewareWithLimit(cache, ttl, maxBodyBytes)`.
- **`battery/embed`: data race on the Ollama embedder's lazy dimension.**
  `OllamaEmbedder.dim` was a plain int written by `Embed` and read by `Dim` from
  another goroutine. It's now an `atomic.Int64` set via CompareAndSwap.
- **Nested `_in` filter on a BelongsTo relation now matches.** `?author.name_in=a,b`
  split into separate AND-ed `EXISTS(... = a) AND EXISTS(... = b)` subqueries, so
  a to-one relation could never satisfy both and silently returned nothing.
  Nested `_in` now coalesces into a single `EXISTS(... col IN (a,b))`, matching
  the top-level `_in` semantics, for BelongsTo/HasOne/HasMany/ManyToMany.
- **`App.Start` no longer leaks workers on bind failure.** A non-graceful
  `ListenAndServe` error (port already in use being the common case) returned
  immediately without draining, leaking every battery/cron/queue and OnStart
  worker an earlier start phase had spawned. The bind-failure path now runs the
  same `abort()`→`Shutdown` drain as every other start phase.
- **Scaffolded apps accept a bare `$PORT`.** `isolation.Runtime.Addr` now
  normalizes a bare numeric port (e.g. `PORT=8088`, as Heroku/Render/Railway/
  Cloud Run inject) to `":8088"`. Previously the generated `main.go` printed
  `http://8088` and then died with `missing port in address` on every such PaaS.
- **`examples/blog` runs again.** It loaded entities from a nonexistent
  `entities/` directory (`go run ./examples/blog` failed immediately, despite
  being the README's first step). Entities are now declared in Go (self-
  contained, runs from any cwd; `gofastr.yml` still mirrors them for the
  codegen path), and seeding runs after AutoMigrate so the demo data actually
  lands. Added a boot+HTTP-200 test (`examples/blog`), the missing test layer
  the assessment flagged.

- **kiln: free-order authoring no longer bricks the rebuild.** Adding an entity
  with a `BelongsTo` to a not-yet-created entity (e.g. `posts`→`users` before
  `users` exists) failed the live auto-migrate and left the session unable to
  rebuild. The live migrator now defers a dangling `BelongsTo` and re-derives it
  once the target is added; the durable world and `kiln freeze` keep the full
  relation. Fixes the deterministically-red `TestFreezeRoundTripWithRichWorld`.
- **kiln: poison journal entries can no longer persist.** `live.Apply` now
  validates an entry with a trial rebuild **before** the durable journal append,
  so an entry that fails to rebuild is rejected and never written (previously it
  was fsynced first, then re-failed on every restart). On any failure the
  in-memory session is restored by replaying the journal.

### Added

- **Dead-letter inspect + replay for queue and webhook.** Terminally-failed work
  could be listed but never re-run. Add optional capabilities:
  `queue.Replayable{Replay}` (implemented by `DBQueue`) and
  `webhook.ReplayableStore{ListDeadDeliveries, ResetDelivery}` (implemented by
  `SQLStore` + `MemoryStore`, surfaced via `Manager.DeadDeliveries`/`Manager.Replay`).
  Replay is idempotent and only touches terminal rows (`status='failed'` for
  queue, `'dead'` for webhook), so it can't double-run an in-flight job. **All
  three queue backends now support replay**: `RedisQueue.Replay` moves a job off
  the dead-letter list back onto the main queue (new `LRange`/`LRem` on the
  host-provided `RedisClient` interface; no new dependency), and `MemoryQueue`
  now **retains** dead jobs in a bounded list (was silently dropping them),
  implements `Browsable`/`Replayable`, and replays them. The
  admin battery surfaces a **Replay** button on the failed-jobs view behind the
  admin gate + CSRF (`POST /admin/queue/_replay/{id}`), and its queue filter
  chips no longer advertise a `dead` status `DBQueue` never writes.
- **`auth.SQLMagicLinkTokenStore`: durable token store for passwordless flows.**
  Magic-link, password-reset, and email-verification tokens were in-memory only,
  so those flows broke on restart and couldn't scale across replicas. Add a
  DB-backed `MagicLinkTokenStore` (single-use via `DELETE … RETURNING`, TTL,
  cleanup) and a `TokenStore` config field on all three plugins
  (`MagicLinkConfig`, `PasswordResetConfig`, `EmailVerificationConfig`); pass
  `NewSQLMagicLinkTokenStore(db)` in production. In-memory stays the default.
- **Observability is discoverable: `WithMetrics()` / `WithTracing()`.** The
  production-grade Prometheus metrics and OpenTelemetry tracing middleware
  existed in `core/middleware` but were never wired into `App`, re-exported, or
  documented. `WithMetrics()` adds the metrics middleware to the default chain
  and mounts a Prometheus `/metrics` endpoint; `WithTracing()` adds the otel
  span middleware (no-ops until a TracerProvider is installed). Both panic if
  combined with `WithoutDefaultMiddleware` (wire them yourself then). Re-exported
  `framework.{NewMetrics,MetricsMiddleware,MetricsHandler,Tracing,Metrics}`.
  New docs: `observability.md`, `deploy.md` (single-binary model, production
  Dockerfile, env config, migrations-as-a-step, TLS/graceful shutdown,
  health/metrics wiring).
- **`App.TryEntity(name, config) error`**: the error-returning variant of
  `App.Entity`. `Entity` panics on misconfiguration (fail-fast for hand-written
  declarations); `TryEntity` returns the error instead and recovers panics from
  deeper validation, so a single bad config (e.g. an AI-authored field, a
  dynamic schema) can't crash the process. `Entity` is now a thin panicking
  wrapper over `TryEntity`. Docs: `framework/docs/content/entity-declarations.md`.
- **`framework.WithPublicOpenAPI()` / `AppConfig.PublicOpenAPI`.** Serves
  `/openapi.json` without the auth gate. The spec is auth-gated by default (it
  enumerates every route), so a minimal app returned `401` there, surprising
  anyone following the quickstart `curl`. Swagger UI at `/api/docs/` is
  unaffected. README quickstart updated to call this out.
- **LICENSE: GoFastr is now MIT licensed.** A top-level `LICENSE` file (MIT)
  replaces the previous "all rights reserved / no license chosen" note. The code
  is now free to use, modify, and redistribute (including commercially) with the
  copyright notice preserved. This unblocks adoption, vendoring, and deployment.
- **Framework DX round-4.** Closes a focused batch from the V4 host-app feedback:
  - **`render.If(cond, html) HTML` / `render.When(cond, fn) HTML`**: inline conditional fragments. `When` is the lazy form for expensive truthy branches.
  - **`render.Classes(parts ...string) string`**: joins non-empty class strings with spaces. Pair with **`render.ClassIf(cond, name) string`** for sparse conditionals: `render.Classes("base", render.ClassIf(isActive, "active"))`. Coexists with `html.Classes(map[string]bool)` for predicate-dense cases.
  - **`html.InputConfig.Value` / `.Placeholder`** and **`html.TextAreaConfig.Content` / `.Placeholder` / `.Rows` / `.Cols`**: typed fields for the common attributes; killed the V4 papercut of falling back to `render.Tag("textarea", attrs, render.Text(content))` for prefilled edit sheets. `Attrs` remains as the escape hatch.
  - **`EntityConfig.Seed func(ctx, *sql.DB) error`**: runs once per entity after `AutoMigrate`. Completion is recorded in a new `_gofastr_seeded` ledger table; subsequent restarts skip seeded entities. Errors abort `App.Start`.
  - **`EntityConfig.SeedFS fs.FS` + `EntityConfig.SeedPath string`**: bind embedded seed data to an entity; reachable inside `Seed` via **`entity.SeedDataFromContext(ctx) ([]byte, error)`**. Removes loose JSON files from tarball-style single-binary deploys.
  - **`App.RegisterEntities(map[string]entity.EntityConfig) *App`**: sugar over multiple `Entity(...)` calls. Iterates the map in alphabetical-by-name order so route registration, OpenAPI tag emission, and MCP tool list order are deterministic across restarts. FK ordering stays correct because AutoMigrate also topologically sorts.
  - **`style.Contribute(func(*StyleSheet)) / style.Apply(*StyleSheet)`**: co-located scoped styles. Declare CSS next to the Go render code via `var _ = style.Contribute(...)` at package scope; the host calls `style.Apply(ss)` inside `createStyleSheet`. Final CSS is identical between dev and prod: no nonces, no inline `<style>`, no CSP relaxation. Distinct from `registry.RegisterStyle` (named, lazy-loaded per-component sheet); `Contribute` adds fragments to the host's global theme stylesheet. Kills the 3-file (screen + theme + reload) iteration cycle.
  - `App.Router()` doc comment now points application-level code at `App.Use` / `App.Group` and documents `Router()` as the plugin/internal surface.
  - **`App.Entity` panics at registration** when `SeedFS` is set but `SeedPath` is empty, a misconfiguration that would otherwise silently mark the entity as seeded with empty data on first run.
  - **`App.Start` failure paths drain via `Shutdown`**: AutoMigrate / RunSeeds / InitPlugins / runStartHooks errors no longer leak goroutines past Start returning. The app lifecycle context is created before AutoMigrate so RunSeeds and individual Seed functions can observe cancellation.
  - **`migrate.RunSeeds` reads the ledger in one round-trip** (was N+1 per entity) and emits per-seed lifecycle slog events (`seed start`, `seed done`, `seed skip`, `seed ledger read`) when a logger is attached via `migrate.WithSeedLogger(ctx, l)`.
  - **`webhook.VerifyTimestamped` rejects non-positive tolerance** (was: silently skipped the replay check) and out-of-range timestamps. Added **`webhook.DefaultTimestampTolerance = 5 * time.Minute`** as the suggested default.
  - **`entity.Registry.AllSorted() []*Entity`**: returns entities in alphabetical-by-name order so order-sensitive consumers (`OpenAPI` tag emission, `crud.RegistryLLMMD`) produce byte-stable output across restarts. Existing `All()` keeps the map shape but its godoc now spells out that map iteration is randomised. Fixes a pre-existing non-determinism that broke ETag caching of `/openapi.json` and `/api/llm.md`.
  - **`gofastr audit deps`** CLI command: scans the project for packages whose `init()` mutates framework-wide state (`style.Contribute`, `registry.RegisterStyle`, `render.RegisterComponent` / `RegisterLayout` / `RegisterFunc`). Output is grouped by Go import path; pairs with the documented supply-chain trust model on `style.Contribute`. Docs: `framework/docs/content/audit-deps.md`.
- **`core/dotenv` package + auto-load in `framework.NewApp()`.** Probes `.env.local`, `.env.<APP_ENV>` (when `APP_ENV` set), and `.env` from CWD before option processing. Existing `os.Environ` always wins. Parser handles double/single-quoted values, escapes, optional `export` prefix, comments; rejects malformed input loudly. Bracket-form `${VAR}` expansion with cycle detection, depth cap, undefined-as-empty, and `\${literal}` escape. Disable via `GOFASTR_DOTENV=off` in the process env. `cmd/gofastr migrate` now routes through this instead of its ad-hoc 1-key scanner. Docs: `framework/docs/content/dotenv.md`.
- **SSR auth policies.** `core-ui/app` exposes a `Policy { Decide(ctx) Decision }` machinery with four decision kinds (Allow / Redirect / RenderAlt / Block). Attach via `Screen.WithPolicy(p)` or `NewScreenGroup(prefix, layout, policies...)`. Construct decisions through the new `core-ui/app/decide` subpackage so call sites don't shadow common variable names: `decide.Allow()`, `decide.Redirect(url)`, `decide.RenderAlt(factory)`, `decide.Block(status, msg)`.
- **`battery/auth.SessionPolicy(opts...)` and `RolePolicy(roles, opts...)`** are the SSR counterparts to the existing `RequireSession` / `RequireRole` middleware. Options: `WithRedirect(url, ...RedirectOpt)`, `WithRenderAlt(factory)`, `WithBlock(status, msg)`. `RedirectOpt`: `NoNext()` to suppress the auto-appended `?next=<request-path>`.
- **`auth.SessionFrom(ctx) (User, bool)`**: cheap in-component getter for ctx-aware chrome (sibling nav, conditional CTAs). Pair with `RenderCtx` for in-page gating without policy machinery.
- **`auth.Roles(roles ...string) []string`**: ergonomic literal-list helper so `auth.RolePolicy(auth.Roles("admin", "owner"), ...)` reads cleanly. Documents the asymmetry with the variadic `auth.RequireRole`.
- **`component.ContextComponent { RenderCtx(ctx) HTML }`**: the optional ctx-aware render interface. Does NOT embed `Component` (so a type can satisfy it via just one method). Embed `component.ContextOnly{}` to also satisfy `Component` with a stub `Render` that the framework never calls.
- **`framework.entity.EntityDeclaration.OwnerField` JSON key (`owner_field`).** Mirrors `EntityConfig.OwnerField` so per-user CRUD scoping works for entities declared in JSON, not just Go.
- **DevMode auto-mints a random JWT secret** when `AuthConfig.JWTSecret == ""`. 32 cryptographically-random bytes, base64-encoded, logged as WARN. Sessions invalidate on restart; set `JWTSecret` for stable dev tokens.
- **`X-Gofastr-Location` partial-redirect signal.** Policy-Redirect outcomes on a partial fetch return 200 + that header + empty body (NOT 303; the runtime fetcher uses `redirect:'follow'` and would auto-chase a 303, losing the header). The runtime's `loadPage` calls itself with the redirected URL and updates `pushState`.

### Removed (greenfield cleanup)

- **BREAKING: escape-hatch field `Attrs` renamed to `ExtraAttrs`** across `core-ui/html/*.Config`, `core-ui/patterns/{disclosure,sortablelist,multiselect}.Config`, and every `framework/ui/*.Config` that exposes a passthrough HTML attribute bag. The new name signals "extra attributes beyond the typed surface" so callers reach for typed fields first. `core/featureflag.Flag.Attrs` stays; it's primary data, not an escape hatch. `html.Attrs` *type* alias is unchanged.
- **BREAKING: 410 GONE compat endpoints removed**. `/__gofastr/theme.css`, `/__gofastr/styles.css`, `/__gofastr/routes.js`, `/__gofastr/catalog.js`, `/__gofastr/css/<path>` now 404 instead of serving a 410 with a migration hint. Use `/__gofastr/app.css` for CSS; routes + catalog ship inline as `<script type="application/json">` in the SSR'd page; per-component CSS comes from `/__gofastr/comp/<name>.css` via `registry.RegisterStyle`.
- **Dead code removed**: `migrate.alreadySeeded` (replaced by batch `readSeededSet`), `i18nui.replaceAll` (inlined to `strings.ReplaceAll`).
- **Doc framing cleanup**: removed "legacy", "back-compat", "kept for", "transitionally" language from comments that describe current first-class APIs (cursor pagination, runtime.js, framework facade, decodeCursorAny, App.Shutdown).

### Changed

- **BREAKING: form intercept is opt-in.** `<form>` elements with the default `application/x-www-form-urlencoded` or `multipart/form-data` enctype are NOT intercepted by `runtime.js`. The browser submits them natively (cookies set, `Location:` followed, file uploads, password-manager UX all work without any framework involvement). Forms posting to a JSON endpoint must opt INTO interception with `enctype="application/json"` OR `data-fui-spa`. `data-fui-rpc` still triggers RPC dispatch as before. **Migration:** `grep -rn '<form' .`: forms that POST to a JSON CRUD/island handler need `enctype="application/json"` added; forms that POST to a redirect-returning handler (auth, settings) need no change.
- **BREAKING: `core-ui/app.App.RenderPage` / `RenderPartial` now wrap richer `*Result` variants.** Returns an error for `Redirect` and `Block` decisions (the legacy shape can't express them). Use `App.RenderPageResult` / `RenderPartialResult` for the policy-aware shape.
- **BREAKING: `core-ui/app.Router.Render` → `Router.RenderRaw`** and **`App.RenderScreen` → `App.RenderScreenRaw`**. Renamed to call out that they bypass the Policy chain. HTTP-serving code must use `App.RenderPageResult`; `RenderRaw` is for SSG/internal callers.
- **BREAKING (effectively no-op): `core/router.Middleware` is now a type ALIAS for `core/middleware.Middleware`.** Anonymous-func cast no longer needed when feeding `battery/auth.SessionMiddleware(mgr)` (or any battery middleware) into `Router.Use(...)`. Existing `router.Middleware(x)` conversions still compile. NOTE: `core/middleware/tracing_test.go` moved to `package middleware_test` because the alias introduces a test-only cycle.
- **BREAKING: `Screen.Policies` field unexported.** Use `Screen.WithPolicy(p)` to add, `Screen.PolicyChain()` to read a copy. Matches `ScreenGroup.policies` (already unexported).
- **Kiln-rendered `form` nodes default `enctype="application/json"`** because they target CRUD endpoints. The world API accepts an explicit `enctype` prop to override.

### Fixed

- **SECURITY (P0): `/auth/register` no longer honors client-supplied `roles`.** Was an anonymous privilege escalation: any visitor POSTing `roles=admin` (form or JSON) was created with admin role. Form-encoded requests were CSRF-reachable from any origin. Now roles are server-assigned to `["user"]` by default; role elevation must happen via a separate admin-gated flow. Regression tests in `battery/auth/register_roles_security_test.go`.
- **SECURITY (P0): `X-Gofastr-Location` open-redirect sealed.** A policy returning `decide.Redirect("//evil.com")` (or any non-relative URL) was emitted into the header raw, which the runtime feeds to `loadPage()`, a cross-origin fetch with credentials. Sealed via `isSafePartialRedirect` in uihost: only same-origin relative paths flow through the header path; absolute / protocol-relative / scheme-bearing / backslash-bypass URLs fall through to a hard 303 (which the browser handles safely). 8-case regression table in `framework/uihost/partial_redirect_test.go`.
- **(P0) Mutex copy in `renderComponentInScreen`.** The previous `tmp := *screen` copied a `sync.Mutex` while the caller held the lock; `go vet` flags it as a contract violation and it was a real concurrent-render corruption risk. Replaced with a free `wrapByScreenType(t, title, content)` helper reused from `Screen.RenderCtx`.
- **(P0) `RenderAlt` cross-user data leak via shared instance.** `WithRenderAlt(alt component.Component)` captured `alt` by pointer; concurrent anonymous requests racing through different screens with the same `landing` instance would clobber its `SetParams`/`Inject`/`Load` mutations across users. Changed to `WithRenderAlt(factory func() component.Component)`; framework calls the factory once per request. Race-tested under `-race` with 32 parallel requests across 8 distinct gated screens.
- **(P0) Partial-redirect `X-Gofastr-Location` was dead-lettered.** `handlePartialPage` previously set the header AND `http.Redirect(303)`. The runtime fetch silently chased the 303 server-side and the header never reached client JS. Now: 200 + header + empty body; runtime detects, replaces `pushState`, loads the redirect target. Chromedp e2e in `framework/uihost/partial_redirect_e2e_test.go`.
- **(P0) TagInput Enter swallow ate legitimate submits.** Chromium dispatches the implicit form submit despite a bubble-phase `preventDefault` on single-input forms. The prior defensive one-shot listener on the form ate the NEXT submit (the user's actual Save click). Replaced with a same-tick timestamp guard: a document-level capture-phase submit listener swallows submits within 50ms of the last tag-input Enter; legitimate submits a few hundred ms later proceed.

### Tests

New coverage added during the adversarial review + tightening pass:

- `framework/uihost/partial_redirect_e2e_test.go`: full chromedp chain for SPA-nav into a Redirect-policy screen.
- `framework/uihost/partial_redirect_test.go`: httptest for the 200+header contract, full-page 303 non-regression, `X-Gofastr-Location` open-redirect rejection (8-case table), ContextOnly screens through full uihost dispatch.
- `framework/uihost/native_form_e2e_test.go`: chromedp confirming an unadorned `<form action="/x" method="POST">` (no enctype, no opts) submits browser-native, Set-Cookie sticks, 303 followed.
- `framework/uihost/render_alt_visual_test.go`: RenderAlt anon→landing screenshot.
- `framework/uihost/safe_path.go`: `isSafePartialRedirect` helper.
- `core-ui/app/policy_test.go`: RenderAlt factory-per-request (concurrent across 8 screens), policy resolver edge cases.
- `battery/auth/policy_test.go`: `SessionPolicy` / `RolePolicy` matrix incl. `?next=` table (6 cases), `WithRenderAlt`, anon→403 default, anon→redirect override, `NoNext()`.
- `battery/auth/register_roles_security_test.go`: privilege-escalation regression (JSON + form).
- `battery/auth/manager_dev_secret_test.go`: random JWT secret minting / explicit-secret preservation / prod-mode opt-out.
- `core/router/middleware_alias_test.go`: alias compile-time + Router.Use acceptance.
- `core-ui/component/context_component_test.go`: ContextOnly satisfies Component, ContextComponent preferred over Render.
- `framework/entity/declaration_owner_field_test.go`: JSON round-trip + omitempty.

## 2026-05-23: round-1 DX feedback + 6 rounds of adversarial review

Commit `2044154`. Addressed FRAMEWORK-FEEDBACK.md from a third-party
app (`wtf-do-i-eat`). Highlights:

### Added

- **`EntityConfig.OwnerField`**: declarative per-user CRUD scoping. Auto-CRUD now injects `WHERE owner_field = <ctx user>` for List/Get/Update/Delete and auto-stamps Create.
- **`battery/auth.SessionMiddleware(mgr)`**: cookie → ctx user loader (the missing counterpart to JWT-only `RequireAuth`).
- **`battery/auth.RequireSession(opts...)` + `WithRedirectOnFail(path)`**: HTTP middleware to gate JSON/API routes (or, with redirect option, browser flows).
- **`battery/auth.VerifyAuthEntitiesPrivate()`**: startup audit that fails fast if `users`/`sessions` entities are exposed via REST or MCP.
- **CSRF helpers + form-encoded auth endpoint negotiation.**

### Fixed (security)

- Open-redirect via `next=/\evil.example` and percent-encoded backslash variants in `successRedirect`.
- Anonymous SSE event leak.
- Anonymous batch endpoints mutating others' rows.
- Hook OR-clause precedence bypass.

## 2026-05-22: worktree isolation mode

Commit `118605c`. First-class runtime resolver for git-worktree
collisions on `PORT`, SQLite files, Postgres database names, and
service env values. See `framework/docs/content/isolation.md`.
