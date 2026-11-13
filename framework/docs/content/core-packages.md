# Core packages

WebSocket keepalive with a negative `WSConfig.PongTimeout` disables pong expiry
and checks activity every quarter of `ReadIdleTimeout`, with a 10ms floor.

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
`Respond` encodes `O` before it writes the status, so a value JSON cannot
encode (a NaN, a channel) answers the generic 500, never an empty 200.
A nil output answers 204 No Content, and so does a nil pointer: a
`Handler[I, *Out]` returning `(nil, nil)` is a 204, not a 200 `null`. A
nil slice or map stays a 200 `null`, since it is a list's empty result. A panic in the handler is a generic 500; a panic after the
response started (inside a `ResponseType`'s `WriteBody`, once the 200 and
part of the body went out) is logged and aborts the connection with
`http.ErrAbortHandler`, so the client sees a broken response, never an
error document spliced onto a success. `WriteError` sends an `*Error`'s
`Code` when it is in 400-599; a nil `*Error`, or a `Code` outside that
range (a forgotten zero, a 1xx, a 2xx), is sent as 500. Its body is the
one error envelope every route shares, crud's included:
`{"error": "<message>", "code": 400, "reason": "<stable code>",
"fields": {...}, "field_reasons": {...}, "success": false}`, with
`reason` absent for `Errorf`'s own wording and `fields`/`field_reasons`
absent with no violations.

`Bind` applies header, query, path, then JSON values in that priority order.
Its destination must be a non-nil pointer; an invalid destination returns
an error rather than panicking.
A present higher-priority value replaces an earlier value, including a JSON
zero value; an omitted property preserves the value already bound.
`header`, `query` and `path` tags on an untagged embedded struct bind as
if declared on the outer struct, the way `encoding/json` promotes body
keys, so a shared `Pagination` struct can be embedded in many inputs. A
nil embedded pointer is allocated only when one of its values arrives.
`ToolAdapter` (the MCP tool form) binds the same promoted tags.
A pointer input (`Handler[*T, O]`, or `UnmarshalStrict(data, &p)` with
`p` a `*T`) gets every struct rule: `T`'s tags bind, its strict keys
hold, and the pointer is allocated when a value arrives, as
`encoding/json` allocates it. `Bind` caps the body at 1 MiB; a larger
one is a 413 whose `Err` is the cap's `*http.MaxBytesError`.

A tagged field binds a string, a number or a bool, or any type that
implements `encoding.TextUnmarshaler`, directly or through a pointer:
`?since=2026-11-01T09:00:00Z` fills a `time.Time`, and a UUID type or an
enum with its own `UnmarshalText` binds the same way. The type's
`UnmarshalText` runs on every tag surface (query, path, header, tool
params), so an enum that refuses `bogus` in a JSON body refuses
`?status=bogus` too; its error is a 400 naming the parameter
(`error.invalid_parameter`, or `error.invalid_header` for a header).

An empty query or header value is the same as an absent one, for every
kind: `?page=` and `?q=` leave the field at its zero value and a pointer
field nil (a `Handler[*T, O]` input stays nil too). A repeated
parameter binds its first value only, so `?q=&q=x` is an absent `q`.
**Behaviour change (after v0.95.0):** `?q=` used to bind `""` into a
string field (and `?page=` failed an int field with a 400). To receive an
explicit empty, add the `,empty` tag option, `query:"q,empty"` or
`header:"X-Foo,empty"`: a string gets `""`, a number, bool or
`TextUnmarshaler` field its zero value, and a pointer field a non-nil
pointer to that zero value, so `*string` tells `?q=` (pointer to `""`)
from no `q` at all (nil). Path values are never empty and take no
option. The tag name is the part before the first comma; any other
option (`query:"q,emtpy"`), `,empty` on a `path` tag, or a tag with no
name fails every `Bind` with a plain error (a 500), so the typo shows on
the first request. `ToolAdapter` reads a tagged param given as `""` the
same way. It also refuses two params keys that fold to one name
(`{"q":"safe","Q":"danger"}`) across the whole params object, before
splitting it into tagged values and the body, with the same
`error.json_folded_keys` a strict body decode returns.

Every decode of client-controlled JSON outside `Bind` goes through the
same strict rule: `DecodeStrict(r, &dst)` for a reader,
`UnmarshalStrict(data, &dst)` for bytes already in memory (a websocket
frame, a buffered body; pass `handler.UseNumber()` to keep numbers in
`any` and map values as `json.Number`, so an integer past 2^53 keeps its
digits), and `CheckTopLevelKeys(data, fold)` when the
caller normalises keys itself. Struct destinations refuse duplicate,
case-folded, and unknown top-level keys; map destinations refuse
duplicate and case-folded ones. Nested objects get the same check at
every depth (`CheckObjectKeys`), folded with `FoldJSONKey`, the
decoder's own rule: `ſource` (U+017F) and the Kelvin sign fold onto
ASCII exactly as `encoding/json` matches them to a field, which
`strings.ToLower` does not. Pass `FoldJSONKey` when you call either
check yourself ahead of a decode into a struct. Stdlib `encoding/json` keeps the last
duplicate and matches tags case-insensitively, so the same body can
mean two things to two readers; refusing it is the only resolution that
privileges neither. Cap the size first (`http.MaxBytesReader`). The
contracts rule GOFASTR1407 reports any raw `json.NewDecoder`/`Unmarshal`
of request or frame bytes, and the repo's own `jsonexact.Unmarshal`/`Decode`
(exact numbers, the same duplicate and case rules), so a site that does not use these helpers
needs a `//gofastr:allow(GOFASTR1407) <why>` marker and a reason.

A body these helpers refuse answers 400 with `reason` `invalid_json`
(or the key refusal's own code: `json_duplicate_key`,
`json_folded_keys`, `json_unknown_field`, `json_multiple_values`, or
`json_not_utf8` for a body that is not valid UTF-8: `encoding/json`
would turn each bad byte into U+FFFD, so the value stored would not be
the one sent; `handler.CheckUTF8` is the same check for a site that
decodes by other means, and crud's JSON bodies run it too). The
message says what was wrong in JSON's terms and never quotes
`encoding/json`, whose text names the server's Go types and structs:

| Body | Message |
| --- | --- |
| `{"ids":[1]}` into `[]string` | `invalid JSON: ids[0]: expected a string, got a number` |
| `{"items":[{"qty":1.5}]}` into an `int` | `invalid JSON: items[0].qty: expected a whole number in range` |
| `[1]` into a struct or map | `invalid JSON: expected an object, got an array` |
| `{"ids": [1,}` | `invalid JSON: syntax error at byte 12` |
| `{"ids":[` | `invalid JSON: the body ends before the JSON value is complete` |
| empty | `invalid JSON: the body is empty` |
| `{"meta":{"a":1,"a":2}}` | `invalid JSON: duplicate key "a" in meta` |

A path is the client's own keys, with array indices in brackets
(`a.b[0].c`); a key that is not a plain name is quoted (`tags["a b"]`).
A type is one of JSON's six: string, number, boolean, object, array,
null. Anything else (a field's own `UnmarshalJSON` refusing, a read that
failed) reads `the value could not be decoded` or `the body could not be
read`. Each part is a phrase, so a catalog translates the whole message.
`JSONErrorDetail(err)` (`BodyReadDetail(err)` for a failed read,
`JSONTrailingData()` for more bytes after the one value) gives you the
same phrase for a decode you run yourself: pass it as the
`{{detail}}` of your own frame, or call `.Error()` for English (a
JSON-RPC message). The framework's own JSON decodes outside a body do
the same: a `?where=` tree (`filter.where_invalid_json`,
`filter.where_invalid_node`), a `?cursor=` token
(`pagination.cursor_token`, which a token whose JSON is the wrong shape
now answers too, in place of the decoder's text naming the token's Go
struct), and a `_batch` envelope's key refusals (`invalid_body`, its
detail one of `error.envelope.not_object`, `malformed_key`,
`duplicate_key`, `unknown_key`).

### router

A method-based HTTP router over Go 1.22's `http.ServeMux`: `METHOD
/pattern` registration with `{id}` path capture, concurrency-safe
middleware chaining after registration (`Use` adds outside-in, the first
added is outermost; `UseInner` adds middleware that runs after every `Use`
and `Group` middleware in the tree, next to the handler), route groups
(a top-level `Group` prefix starts with `/` or names a host with a dot
or `localhost`, without a port; `Group("v1")` panics, since the mux
would read `v1` as a host; a group's `NotFound` and `MethodNotAllowed`
answer the misses under its prefix, the longest matching prefix
winning, even when the root serves the request; a 405 runs the
middleware of the group that holds the path's routes, so CORS set on
`Group("/api")` answers the preflight of `POST /api/items`), and sanitized path
params (`Param` strips CR/LF/NUL, a decoded `/`, smuggled `..`, and in a
`{name...}` catch-all a leading or doubled `/`, which only `%2F` produces;
`handler.Bind` applies the same rule to `path:"..."` tags, and any other
reader of `r.PathValue` should go through `Param` or the exported
`SanitizePathParam`). `Param` panics when the matched route declares
no wildcard by that name, so a misspelt `Param(r, "idd")` fails the
first request that reaches it (a 500 through the recovery middleware)
instead of returning `""`. A request built by hand with `SetPathValue`
and no pattern is not checked. `handler.Bind` does not panic: a
`path:"..."` field the route does not declare stays zero, so one input
struct can serve both `POST /users` and `PUT /users/{id}`.
`ResourceGroup(name)` returns a sub-router whose routes name the REST
resource they serve; `RouteResource(ctx)` reads the name in middleware,
before the handler runs; a route outside any ResourceGroup clears a
name its request inherited from a route that redispatched it. Entity
routes use it so `auth.RequireAPIScopes` scopes `/_batch`, the other
subroutes and the entity's Endpoints to its table. The
framework owns the root `*Router` and hands it to batteries and authors
to mount routes. `Prefix()` returns a group's full prefix and `Root()`
the router at the top of its Group chain, for a registrar that must
serve an absolute URL outside its own prefix (routes added there skip
the group's middleware). Direct. Start at `core/router/router.go`:
`Router`.

`SetRouteGate` runs before the middleware chain. A false result or a
panic answers 404 on a matched route and excludes that method from a
405's `Allow` header; a panic is logged. `SetServeHook` also runs before
the chain: its panic is logged and the handler still runs. `PreRoute`
runs before matching, including locale-prefix rewrites. Its panic is
logged and answers a localized 500 without running middleware or a
handler. These hooks have their own guards because the app's Recovery
middleware cannot catch a panic raised before it runs.

## Real-time

### stream

The HTTP-facing real-time primitives: SSE (`SSEWriter` for one response,
`SSEBroker` for multi-subscriber fan-out), a minimal WebSocket client
(`Upgrade`, `WebSocketConn`, `Hub`), and `ChunkedWriter`. Direct when you
add an SSE or WebSocket endpoint; the framework's island-push and event
bus layer on `SSEBroker`. Start at `core/stream/sse_broker.go`:
`SSEBroker`.

A subscriber that asks for `?event=<name>` gets only events of that
name, and the broker never queues the others for it, so under
drop-oldest an event it filtered out cannot push out one it asked for.
`SSEWriter.SetID` cuts the id at its first CR, LF or NUL, and writes no
`id:` field when nothing is left: an empty `id:` line would reset the
browser's Last-Event-ID.

Above the WebSocket client sits `StateChannel`
(`core/stream/state_channel.go`), for apps that reconnect into live
state: it hydrates each new connection with a role's snapshot and then
delivers events with strictly increasing sequences, so the browser
helper (`core-ui/runtime/src/ws.js`,
`__gofastr.createSequencedReducer`) can reject anything stale and a
snapshot captured before a mutation cannot resurrect the state that
mutation replaced. Your side of the contract is the `SnapshotSource`:
`SnapshotFor` returns the snapshot payload and its sequence from ONE
immutable read (the channel sends the larger of that version and one
above every event it already sent, so a reconnect's snapshot never ties
an event the old connection applied), and `FilterEvent` shapes each event per role BEFORE
serialization. Filtering there is the minimization boundary — a field
`FilterEvent` strips never crosses the transport, while hiding a field
in the UI is not data minimization at all. Sequences share one space
per channel across roles; delivery is best-effort like `Hub` (a slow
connection drops events, a connection that cannot accept its snapshot
is closed to reconnect), and like the other stream primitives a
channel spans replicas only through `fanout`. Each accepted connection
carries a `ConnectionID()` (set `WSConfig.ConnectionID` to mint your
own) for correlating a client's reconnects in server-side logs.

Two knobs exist for sources whose events are not recoverable from a
snapshot. `StateChannel.CloseOnOverflow(true)`, called before `Run`,
closes a connection whose send buffer cannot take an event (the
default drops the event for that connection alone, the right posture
for presence), so the client reconnects and re-hydrates. It also
deepens the channel's own job queue from 64 to 4096 events, and an
event past that closes every connection it would have reached instead
of vanishing. And
`WebSocketConn.CloseWithStatus(code, reason)` closes with a status
code and a reason in the close frame, for a server that accepted the
handshake only to refuse the connection: a browser cannot read an
HTTP status off a failed handshake but can read a close code. The
reason is kept to printable ASCII and 123 bytes; a reserved or
out-of-range code goes out as 1002 and is returned as an error; a
close the peer began first still echoes the peer's own code.
`stream.WithUpgradeHook(ctx, fn)` makes `Upgrade`, serving a request
whose context carries it, hand `fn` each connection it opens: a server
reaches the WebSockets its handlers own that way. `framework.App` uses
it to close every open one with 1001 when `Shutdown` starts, since
`http.Server.Shutdown` neither waits for nor closes a hijacked
connection.

`WebSocketConn` follows RFC 6455's strict readings: a text message
that is not valid UTF-8 closes with 1007, a message past `ReadLimit`
closes with 1009, and every malformed frame closes with 1002 (reserved
bits or opcode, an unmasked client frame, a fragmented or oversized
control frame, a continuation with nothing to continue or a new message
inside one, a close reason that is not valid UTF-8, a payload length in
a longer form than it needs). No data frame goes out after the
connection's own Close frame, and `Close` first writes the messages
`Write` already queued, oldest first, for up to `WriteTimeout` (10s
when it is zero or negative), the same bound cutting a frame the pump is
still writing to a stalled peer; `Write` returns `ErrClosed` once `Close`
has started (a `Write` racing `Close` may still be accepted and
dropped). A close the peer started, or one after a failed write, sends
the Close frame at once with no drain. One read pump owns the socket and hands each message to `Read`
through a small buffer. An app that reads must take a message at least
once per `ReadIdleTimeout` + `PongTimeout`, or the connection closes
(the keepalive cannot run while a message waits). `PongTimeout` counts
from the moment the Ping is written, so a long data write that delays
the Ping does not eat into the peer's time to answer. An app that never
calls `Read` is push-only: once the buffer fills, further inbound data
is dropped so pings and the Close handshake still flow. The pump answers
a Ping itself, so an app limiting its own frames never counts them:
`MaxControlFramesPerSecond` (default 32, burst twice that; negative
disables) caps the Pings and Pongs a client sends, and past it the
connection closes with 1008.

The same rule holds on the way out. `Write` and `WriteString` send a
text frame, and a text frame must be valid UTF-8, or the browser fails
the connection with 1007. So they check the bytes first and return
`stream.ErrInvalidUTF8`, queueing nothing, when the check fails.
`Hub.Broadcast` and `Hub.BroadcastWait` now return an error too: the
same `ErrInvalidUTF8`, before the message goes to anyone (a dropped
message is still not an error). `StateChannel` never queues an
envelope that is not UTF-8 (a `json.RawMessage` or `MarshalJSON` can
carry such bytes past `json.Marshal`): a snapshot like that closes the
connection, and an event like that is dropped. To send arbitrary bytes,
use `WriteBinary`, or `Hub.BroadcastBinary` / `BroadcastBinaryWait`.
They send a binary frame (opcode 0x2) holding exactly the bytes given,
and the browser hands it to the page as a `Blob` or `ArrayBuffer`.
Text and binary share one send queue, so they reach the peer in the
order they were written, including the messages `Close` drains.
`Read` returns text and binary messages alike as bytes and does not
say which opcode a message came in on, so a protocol that mixes the
two tells them apart in its own framing.

```go
if err := conn.WriteString(msg); errors.Is(err, stream.ErrInvalidUTF8) {
	err = conn.WriteBinary([]byte(msg)) // not text: send the bytes as binary
}
```

`Upgrade` refuses a handshake RFC 6455 does not accept before it
hijacks: a method other than GET, a request older than HTTP/1.1, no
`Connection: Upgrade` token, a version other than 13, or a key that is
not base64 of 16 bytes. `Connection` and `Sec-WebSocket-Protocol` are
read across every line the client sent them on. A zero or negative
`WSConfig.SendBuffer` means the default of 32. After hijacking, buffered
response data and the 101 response share a write deadline: `WriteTimeout`
when positive, otherwise 10 seconds even when frame deadlines are disabled.
The deadline is cleared before the connection starts its frame pumps;
deadline, flush, or response-write failures close the hijacked connection. A
transport that hijacks the connection itself runs the same check,
`stream.CheckUpgradeRequest(r, originOK)`, before writing its 101;
`originOK` decides the Origin as `WSConfig.CheckOrigin` does.

`battery/rtc`, the WebRTC signaling package, is the
packaged StateChannel consumer for rooms of peers (see
[WebRTC rooms](rtc.md)).

### fanout

The lossy, best-effort cross-replica transport behind real-time
delivery. A `Fanout` interface (`Publish` / `Subscribe`) with an
in-process backend and a Redis adapter (`NewRedis`), plus a node-id
envelope that stops broadcasts from re-broadcasting. `PublishQueue`
publishes from hot paths without blocking; `PublishQueueReporting` is
the same queue with a per-payload callback that runs when the payload
is not published (backend error, timeout, or `ErrPublishDropped` on
overflow), for a caller that must act on a loss. Indirect by
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

### a2a

The Agent2Agent (A2A) v1.0 task-exchange server: a JSON-RPC handler
for SendMessage and friends, with named Go skills (invoked via
`Message.metadata["skill"]` or a data part with a `"skill"` key), SSE
streaming, push-notification webhooks behind the SSRF-guarded client,
and a memory or SQL store where every task and config is scoped to the
owner `Config.Owner` resolves. Direct: call `NewServer(Config{…})` and
mount the handler behind your auth middleware; the agent card in
`framework/uihost` advertises it. Start at `core/a2a/server.go`:
`NewServer`.

### openapi

An OpenAPI 3.1 builder plus serving handlers. A `Spec` accumulates
paths, schemas, and security schemes and `Build()` emits the doc;
`SetDescription` fills `info.description`, which a per-request
`RequestView` copy keeps (the docs page shows it as plain text, one
paragraph per blank-line-separated block);
`Handler` serves it (auth-gated by default) and `DocsHandler` serves an
API reference page whose `public` flag gates both the page and its
nested spec route (the framework passes its `PublicOpenAPI` setting, so
a public spec gets a public browse page). The page lists every
operation the caller can see (method, path, summary), grouped by tag,
and links the spec. It is plain HTML with no script, stylesheet or
inline style, so it renders under the default Content-Security-Policy
in the browser's own light or dark styles, and its labels are
`openapi.docs.*` phrases. `DocsHandlerWith(spec, DocsConfig{...})`
chooses the paths: `BasePath` is the mount, `SpecPath` the spec under
it (default `/openapi.json`), and `PagePath` a second page path, so
`{BasePath: "/swagger", SpecPath: "/v1/swagger.json", PagePath:
"/index.html"}` serves the Swagger-style layout. Anything else under
the mount is 404. A `Spec` may carry a
`RequestView` — a per-request spec builder the auth-gated `Handler`
consults, so the document can drop what THIS caller may not read; the
framework's entity spec uses it to hide entities with no read grant,
and `PublicHandler` (the full-disclosure opt-in) ignores it.
`Nullable` widens a property to take `null` as well: its type `T`
becomes `[T, "null"]` and its enum gains `null`. The framework's entity
schemas apply it to every optional field (not `Required`, not
generated, not a key column), since the route stores the `null` a write
sends and reads return it; a custom endpoint's `InputSchema` and
`OutputSchema` stay as declared. Mostly
indirect: the framework generates the
spec from your routes and entities. Reach for it directly to add custom
operations or mount the docs endpoint. Start at `core/openapi/spec.go`:
`Spec`.

### schema

`core/schema` defines entity fields, validates values, and generates JSON
schemas. Start at `core/schema/schema.go`: `Field`, `Validate`, `JSONSchema`.
Validation accepts precise `json.Number` numeric values. Generated JSON
and OpenAPI object properties and required lists use an explicit `WireName`.
Character bounds and text patterns match validation; generated enum lists
own their slices, so editing a schema does not change its declaration.

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
source HTML escaped, no raw-HTML passthrough. Link and image URLs go
through the canonical `core-ui/urlsafe` allow-list (`Anchor` for hrefs,
`ImageSource` for image sources), so schemes outside it — `file:`,
`ftp:`, `blob:`, `ws:`, protocol-relative references — render as `#`.
Direct; no framework auto-wiring. Start at `core/markdown/markdown.go`:
`Render`.

Frontmatter is a leading `---` block whose every non-blank line is a
`key: value` pair with a non-blank key, a `#` comment, or a continuation
of the key above it: an indented line or a `- item` line (a YAML list,
nested map or folded string). Continuation lines are skipped, so that
key reads as empty. Any other line makes the block body: a document that
opens with `---`, prose, then another `---` keeps the prose, rendered
after a thematic break.

Line breaks follow CommonMark. A plain newline inside a paragraph, list
item or blockquote is a soft break: it renders as a newline, which the
browser shows as a space, so source hard-wrapped at 72 columns reads as
one flowing paragraph. A line that ends in two or more spaces, or in a
backslash, is a hard break and renders as `<br>`.

A list item owns the lines indented two or more spaces past its marker,
so it can hold a nested list, a fenced code block or a second paragraph.
Items separated by a blank line stay one list. An ordered list that does
not begin at 1 carries `start`, so a list that resumes at `3.` after a
paragraph keeps counting from 3. In a table cell `\|` is a literal
pipe, inside a code span too. A link URL keeps its balanced parentheses
(`[Go](https://en.wikipedia.org/wiki/Go_(game))`), and a `*` or `_` with
a space after it is text, not emphasis (`2 * 3 * 4`).

Fenced blocks follow CommonMark on two points that matter for
documentation. A fence is three **or more** of the same character and
closes only on a run at least as long, so a ` ```` ` block can hold a
` ``` ` example — the only way to document fenced syntax. And the info
string is a language followed by options, not one opaque name:
`ParseFenceInfo` splits it into `FenceInfo{Lang, Meta}`. `Lang` becomes
`class="language-X"` as before; `Meta` is everything after it,
verbatim, carried on the `<code>` tag's `data-meta` attribute for a
renderer to interpret. `core/markdown` assigns it no meaning:
`framework/ui.Markdown` is what maps fence options onto
`CodeBlockConfig` (`title=`, `showLineNumbers`, `scroll`, highlight
ranges, `diff`, `words=`, `wrap`; the full table lives in
[UI components index → CodeBlock](ui-new-components.md#codeblock-line-highlighting-diffs-wrapping)).

Before this, the whole info string became the language, so
` ```go title="main.go" ` emitted
`class="language-go title=&quot;main.go&quot;"`, matched no language,
and silently cost the block its syntax highlighting.

### static

A hardened file server for `embed.FS` / `fs.FS`: ETag caching,
configurable `Cache-Control`, MIME detection, SPA fallback, and
traversal / dotfile / forbidden-config rejection. Seekable files (disk,
`embed.FS`) are served through `http.ServeContent`, so `Range`,
`If-Range` and multi-range requests get `206` and video can seek; a
filesystem whose files cannot seek gets the whole body and no
`Accept-Ranges`. Preconditions follow RFC 9110 order (`412` for a
failed `If-Match` before any `304`). The ETag is a content hash; a file
over 16 MiB is hashed in the background and served without an ETag
until the hash lands, so the first request does not read the whole
file. On a disk-backed
`FS` (`os.DirFS`), a request whose target resolves through a symlink
out of the root is refused with 404 — the read side is contained by
the configured root the same way write sinks elsewhere in the repo
are, including a root directory created after `Handler` was built,
which the handler binds on the first request that finds it; `embed.FS`
cannot carry symlinks and is unaffected. Direct: call
`Mount` (or `Handler`) to serve embedded CSS/JS/images; methods other
than GET and HEAD receive 405 with `Allow: GET, HEAD`. The framework
uses it for its own assets too. `Prefix` is `/static`, `static` or
`/static/` alike; `Mount` serves `/static/...` and redirects a bare
`/static`. The handler strips the prefix on a segment boundary only
(`/staticx.js` is not `x.js`), and a path without it is served from
the root, so the handler also works behind `http.StripPrefix`. A
request for a directory without the trailing slash (`/docs`) gets
`301` to the slash form (`/docs/`, query kept) when the directory has
an `IndexFile`, as `net/http`'s `FileServer` does, so the index's
relative links resolve under it. The `Location` is relative
(`./docs/`): it lands under the handler's `Prefix` or an outer
`StripPrefix`, and never on another host, whatever the path spells
(`//evil.com`, `/\evil.com`). With `SPA: true`, a miss is a client
route (`/dashboard/settings`, `/users/42`, `/u/jane.doe`, `/blog/v1.2`)
and gets `IndexFile`, served `no-cache` whatever `MaxAge` says, so the
shell is never stored under a URL that names no file. A miss whose last
segment ends in a well-known asset extension gets `404`, not the shell:
a browser would run that HTML as a script or a stylesheet. The asset
extensions, matched in any case, are `.js` `.mjs` `.css` `.map`, `.png`
`.jpg` `.jpeg` `.gif` `.svg` `.webp` `.avif` `.ico`, `.woff` `.woff2`
`.ttf`, and `.json` `.wasm` `.txt` `.xml`. A client route that ends in
one of those (`/feeds/latest.json` as a screen) needs a server route of
its own; any other dot is part of the route.
`NotFoundFile` answers a miss with a
file from the same `FS` under status 404 (a static export's
`404.html`; `gofastr docs serve` uses it). For a handler that serves
its own bytes, `RefusesIn(fsys, path)` reports whether the handler
would refuse a requested path (the UI host's static dir uses it), and
`Refuses(path)` judges the spelling alone, for a tree the caller walked
(the static export uses it). Both read a path the way Windows opens it,
on every OS: `\` separates segments, so `/assets%5C..%5C.env` is
refused. A segment that ends in a dot or a space, or holds a colon, is
refused on every OS whatever it names, because Windows opens `app.css.`
and `app.css::$DATA` as `app.css` and `web.config.` as `web.config`; in
SPA mode such a path is a miss and a client route still gets the index.
On every OS a file whose name holds a backslash is judged by the pieces
it splits into: `a\b.txt` is served, `a\.env` and `x\web.config` are
refused. A segment holding `~` followed by a digit has the shape of an
NTFS 8.3 short name, which a Windows volume that generates them opens
under its long name (`WEB~1.CON` as `web.config`, `ENV~1` as `.env`).
The handler lists the segment's directory and refuses the name unless
an entry carries exactly that name, on every OS: a real `photo~1.png` is
served, and `PHOTO~1.PNG`, which a case-insensitive volume would open
as it, is refused. An `embed.FS` cannot hold an alias, so its names are
opened without a listing. `Refuses` skips the short-name check: a walk
yields each entry's real name. A name like `a~b.txt` or `notes.txt~` is
served.
`IfNoneMatch(r, etag)`
checks an `If-None-Match` list the RFC 9110 way: comma lists, `*`, and weak
comparison. Start at `core/static/static.go`: `Handler`.

## App plumbing

### config

A typed config loader. `Load` reflects `config:` / `default:` /
`required:` / `sensitive:` / `validate:` struct tags and binds from
pluggable `Source`s (default `EnvSource`; `MapSource` for tests),
recursing into nested structs with a `SCREAMING_SNAKE` prefix (value or
pointer form: a pointer is allocated and bound when any nested key is
present, left nil otherwise) and redacting `sensitive` values from
errors, including a `Validate` error that quotes one with `%q` or
JSON-encodes or URL-escapes it. `validate:"min=1,max=65535"` enforces numeric bounds (string bounds
count characters; a `time.Duration` takes duration bounds such as
`min=1s,max=5m`); `validate:"oneof=debug info warn error"` restricts string
choices. A rule `Load` cannot apply (an unknown rule, a bound on a bool)
fails `Load`. An empty value (`KEY=`) leaves the field as it was, without
the `default:` tag, and the rules still check it. Recursive configuration
struct shapes are rejected.
Integer values must fit the destination field: `int` uses the target
architecture's width (32 or 64 bits), while `int64` always accepts the
64-bit range. Out-of-range values fail `Load` rather than truncating.

A named struct field (`DB DBConfig`) reads its keys under `DB_`. An
embedded struct does not: its fields are promoted the way
`encoding/json` promotes them, so in `type Cfg struct{ Base }` Base's
`config:"PORT"` reads `PORT`, never `BASE_PORT`. A pointer embed is
allocated when one of its keys is present, and an unexported embedded
type promotes its exported fields. A field declared on the outer struct
wins over a promoted one with the same key; two embedded structs
promoting one key at the same depth make `Load` fail with an error
naming both, as does a `config:` tag on an embedded struct. To keep a
prefix, name the field (`Base Base` reads `BASE_PORT`).

A promoted field the outer one shadows is never read from a source, so
it gets its own `default:` tag. When `Cfg` embeds `Base` (whose
`Port` reads `PORT` with `default:"8080"`) and declares its own `Port`
reading `PORT`, `cfg.Port` reads `PORT` and `cfg.Base.Port` holds
`8080`, so a method on `Base` reading `b.Port` sees its default, not
`0`. The shadowed field's `required:` and `validate:` tags do not
apply. When a source still holds a key spelled the old way
(`BASE_PORT`) and no field reads it, `Load` logs one warning per key
through `slog`'s default logger, naming the key it reads now; `Load`
still succeeds.

A field whose type (or pointer type) implements
`encoding.TextUnmarshaler` (`time.Time`, `slog.Level`, `netip.Addr`, a
validated enum) is bound through `UnmarshalText`, so a bad value fails
`Load` instead of binding unvalidated text or being skipped.
Direct: call `config.Load(&cfg)` at startup. Start at
`core/config/config.go`: `Load`.

### netguard

The single "is this IP internal" predicate (loopback, private,
link-local, site-local IPv6, CGNAT, cloud-metadata), normalizing
IPv4-mapped IPv6 first and checking IPv4 addresses carried by NAT64,
6to4, IPv4-compatible IPv6, and Teredo. Teredo is refused if either
its server or its decoded client IPv4 is internal, with a `Reason` helper. Tiny: two exported funcs. Indirect:
outbound-fetch surfaces (webhooks, A2A push, the harness WebFetch tool,
the Web Bot Auth directory fetch) enforce it at dial time;
app authors do not call it. Start at `core/netguard/netguard.go`:
`IsInternal`. `GuardedTransport` ignores `HTTP_PROXY`/`HTTPS_PROXY`:
through a proxy the dial check would see only the proxy's address while
the proxy connected to the target. A deployment that must egress
through a proxy supplies its own client where the surface takes one
(battery/webhook `Options.HTTPClient`, which keeps a per-request target
check) or turns the surface's private-network opt-out on and leaves the
egress policy to the proxy. `GuardPlainTransport` clones a
caller-supplied `*http.Transport` that has no proxy and no dialer of its
own and gives the clone the guarded dialer, so a custom TLS or pool
setting keeps the dial-time check. A transport that would speak HTTP/2
on its own still does after the swap. Any other transport gets the
weaker per-request check.

### textsafe

The one predicate for characters that forge, reorder or hide text: C0
and C1 controls, the Unicode line separators, and the invisible set
(zero-width, joiners, bidi overrides and isolates, the tag characters
U+E0000–U+E007F that hide ASCII in a prompt, and the Hangul fillers
U+115F, U+1160, U+3164, U+FFA0). `IsInvisible` and `IsUnsafe` judge one
rune, so they report every tag character. The string helpers
(`StripInvisible`, `StripUnsafe`, `ContainsInvisible`, `ContainsUnsafe`)
keep a subdivision flag whole: U+1F3F4, then 3 to 7 tag lowercase
letters or digits, then CANCEL TAG (Scotland, England, Wales). Any other
tag run, one behind a black flag included, is stripped or refused. Start
at `core/textsafe/textsafe.go`.

`Storable(s string) bool` answers a different question: whether a string
is valid UTF-8 without a NUL byte, so Postgres can bind it as text without
SQLSTATE `22021`. Use it before a SQL bind and refuse caller input with
400 or a typed validation error; do not replace or strip the bytes.
Empty strings, line breaks and ordinary Unicode pass. Lone surrogates
encoded as WTF-8 fail the UTF-8 check. This is not `ContainsUnsafe`:
storage permits characters that a header, log or identity may forbid.

`Recovered(value)` renders a panic value for logs, strips unsafe
characters, and truncates it to 4 KiB. `LogPanic(ctx, msg)` is the shared
swallow-and-log guard for host callbacks: use it directly as
`defer textsafe.LogPanic(ctx, "observer panicked; ignored")`. It calls
`recover` itself and logs once through `slog.Default().ErrorContext`,
with the scrubbed value under `panic` and the stack under `stack`.
An ordinary return logs nothing. Do not wrap the call in a deferred
closure: recover must run directly in the deferred function. A panic
returns the caller's current named results, so gates start with a false
result and pre-route hooks track whether they completed. Keep a local
recover when a callback needs an error, a protocol response, cleanup,
backoff, a re-panic, or a different logger.

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
- **Expecting `markdown` to pass through raw HTML.** It escapes all source HTML by design. If you need embedded HTML, build it with `render` directly rather than smuggling it through markdown. An HTML comment block (`<!-- … -->` on its own lines, also under a list item) is the one exception: it renders as nothing, so a tooling directive such as `<!-- gofastr:compile … -->` never shows up as page text.
- **Adding `on*` handlers via `render.Attr`.** The attribute allow-list drops them; use a real event binding (`data-cui-*` via `core-ui/interactive`) instead.
