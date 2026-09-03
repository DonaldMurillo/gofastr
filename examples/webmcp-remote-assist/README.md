# webmcp-remote-assist

One Go binary, one origin, two roles: a support console whose in-browser
AI agent can guide an operator through a camera session.

This is the reference example for three framework surfaces working
together:

- **Browser WebMCP** (`framework/experimental/webmcp`): server-declared
  tools registered on `navigator.modelContext`, scoped to the support
  documents, gated by the support role cookie.
- **Sequenced realtime state** (`core/stream.StateChannel` + the
  `ws` runtime module): one channel per session, per-role snapshots and
  events, reconnects that rehydrate instead of resurrecting.
- **WebRTC with GoFastr signaling**: offer/answer/ICE relayed over the
  session's WebSocket; media never crosses the Go server.

## Run it

```bash
cd examples/webmcp-remote-assist && gofastr dev   # or: go run .
```

Open <http://localhost:8090> (the framework's dev isolation may remap
the port; the startup log names the real one).

1. **Support sign in** with the key from the start-up log mints an
   HttpOnly support cookie.
2. **Create session** → the console opens with a one-time join link.
3. Send the link to the operator (another browser, another machine).
4. **Operator** opens it: a confirmation page (fetching the link spends
   nothing, so a chat preview cannot burn it) whose button trades the
   token for an operator cookie (HttpOnly, `Path=/session`) and opens
   the operator page.
5. Operator clicks **Share camera** (video only — no microphone is
   ever requested). Support sees the feed peer-to-peer.
6. Support sends instructions two ways that share one command path:
   the visible form, and the `send_instruction` WebMCP tool an
   in-browser agent can call.
7. Operator reads the instruction and clicks **Mark as shown**;
   support sees the acknowledgement correlated with the agent
   invocation id when the command came from one.
8. Leaving the console through the header nav is a real navigation:
   the next document carries no support tools.

To drive the WebMCP tools by hand, use a Chromium with
`chrome://flags/#enable-webmcp-testing` (or automation with
`--enable-blink-features=WebMCP`), then in the console on the support
page:

```js
const mc = document.modelContext || navigator.modelContext;
const tools = await mc.getTools();        // inspect_session, send_instruction, clear_instruction
const t = tools.find(x => x.name === "inspect_session");
JSON.parse(await mc.executeTool(t, JSON.stringify({ session: "..." })));
```

## The boundaries it exists to show

| Boundary | Where |
|---|---|
| Discovery is authority | The manifest and bridge script are wrapped in the same role check as the tool endpoints (`WithAssetAuthorization`, `WithPageScope`). |
| Marker ≠ authorization | `X-Gofastr-WebMCP: 1` attributes a call to the observer; the role cookie decides. Spoofing the header without the cookie is a 403. |
| One typed command | `assistCommand` + `applyCommand` in `session.go`: the manual form, the two agent tools, and the ack all decode into it. |
| Role filtering before serialization | `sessionSource.FilterEvent`: an offer is delivered to support only, an answer to the operator only; the invocation ref is stripped for the operator. |
| Hard navigation out of capability pages | `WithDocumentScope("/support…")`: the host's SPA runtime turns the scope edge into a document boundary. |
| No state resurrection | Snapshots and events share one sequence; `createSequencedReducer` applies only `sequence > applied`. The browser test drops the operator's socket server-side and asserts the cleared instruction stays cleared. |
| Media bypasses the server | `static/app.js` does `getUserMedia({video, audio: false})` and WebRTC with empty `iceServers`; the Go server relays SDP/ICE JSON only. |
| No microphone, enforced by the browser | The app's `Permissions-Policy` opens `camera=(self)` and keeps the framework's default `microphone=()`, so even a page script that asked for audio would be refused. |

## What is demo-grade on purpose

- **The support login is a shared key.** `ASSIST_SUPPORT_KEY` from the
  environment, or a random one printed once at start. A real deployment
  puts `battery/auth` (or equivalent) at `/support/login`; the cookie
  shape and every downstream check stay the same.
- **State is in RAM.** One process owns the session map and the
  channels. Multi-replica means the session store moves to a database
  and the channel fanout to a broker; the page code does not change.
- **Sessions are short-lived** (10 minutes) and join links are
  single-use: only the confirmation page's POST spends one. Both are
  enforced server-side and both render the same 410 recovery screen,
  so a caller cannot probe the difference.

## Deployment notes

- **HTTPS and WSS.** `getUserMedia` and WebMCP are secure-context APIs.
  The pages build their WebSocket URL from the page scheme (`wss` under
  HTTPS, `X-Forwarded-Proto` respected); terminate TLS at your proxy
  and forward the upgrade headers for `/session/:id/ws` and
  `/support/session/:id/ws`.
- **STUN/TURN.** The example ships empty `iceServers` because its
  browser test runs both peers on one machine. Real deployments need at
  least a STUN server (`stun:stun.l.google.com:19302`) for NAT
  traversal, and a TURN server if one side sits behind a symmetric NAT
  or strict firewall — decide based on your users, not on the example.
- **Cookies.** Both role cookies are `HttpOnly` and `SameSite=Lax`.
  The support cookie is `Path=/` because the WebMCP assets live under
  `/__gofastr/`, outside the `/support` tree; the operator cookie is
  `Path=/session`, so it never rides on a support URL. `Secure` is
  decided per request from TLS or `X-Forwarded-Proto`, so plain-HTTP
  localhost works and a TLS deployment never sends the cookie in clear.
- **Caching.** The WebMCP mount uses `WithPrivateAssets()`
  (`Cache-Control: private, no-store`): the bridge and manifest are
  requester-dependent, and a shared cache must never replay an
  authenticated manifest to anonymous traffic. `app.js` is
  hash-versioned and immutable.
- **Browser/model compatibility.** WebMCP needs Chromium 146+ behind
  `chrome://flags/#enable-webmcp-testing`, or the origin trial from
  Chrome 149. On any browser without the API the bridge feature-detects
  and does nothing; the manual controls keep working. Automation passes
  `--enable-blink-features=WebMCP`.
- **Safe logging.** The WebMCP observer (`WithObserver`) logs phase,
  tool name, method, declared path, status, error class, duration, and
  invocation id — never the input body, headers, or query string; tool
  inputs are where secrets live. The browser side is the same posture:
  `ws.js` never logs close reasons or payloads, and the page's
  `window.__assist` debug state carries phases and envelope types only.
  The host sends `Cache-Control: no-store` on every rendered page, so
  instruction text and join links stay out of shared and history
  caches; the example's tests pin that contract for the session pages.
- **CSP note for third-party libraries.** The example loads one
  external script (`app.js`) through the uihost script rail and uses no
  `eval`. If you add a library that wants code generation (template
  compilers, some WebRTC stats dashboards), run it inside a sandboxed
  `iframe` or a worker with its own CSP — never by widening the host
  document's `script-src` to `unsafe-eval`. Trusted host-page assets
  (this example's `app.js`) and sandboxed iframe plugins are different
  trust domains; treat them differently.

## Tests

```bash
go test ./examples/webmcp-remote-assist/ -count=1          # HTTP + source level
go test ./examples/webmcp-remote-assist/ -count=1 -run TestRemoteAssistFlow -v  # one Chrome, two tabs
```

The browser suite launches Chromium with fake media flags
(`--use-fake-device-for-media-stream`,
`--use-fake-ui-for-media-stream`) and `--enable-blink-features=WebMCP`,
plays support and operator in two tabs of one browser, and covers
discovery, the shared command path, the ack, server-side transport
death with recovery, and the hard navigation out of the console.
