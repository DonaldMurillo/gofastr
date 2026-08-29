# Heavy-JS plugin platform (`framework/pluginhost`)

`framework/pluginhost` lets a GoFastr app mount a **heavy-JavaScript
plugin**, a megabyte-class client bundle like a WYSIWYG editor or a
diagram renderer, as a genuinely third-party, isolated module. It is
the client-side mirror of the process-isolation track (#37): the same
question ("what can code we didn't audit actually reach?") answered for
untrusted DOM-touching JavaScript.

It was not designed up front. It was distilled from the first such
plugin (the `gofastr-plugins` WYSIWYG editor) after the isolation model
survived a measured go/no-go gate (p99 keystroke latency ≤ 16 ms inside
the sandbox), then proven general by a second plugin (mermaid) that
reused it without modification.

## The isolation model: secure by default

The plugin bundle runs inside an **opaque-origin sandboxed iframe**:
`sandbox="allow-scripts"`, and `allow-same-origin` is **never** added.
Two independent, authoritative enforcement points guarantee this:

1. The sandbox derivation (`Manifest.SandboxString` server-side and
   `sandboxFor` in the broker JS) **always** strips `allow-same-origin`
   and forces `allow-scripts`; a mis-configured or tampered manifest
   cannot produce a de-opaqued frame. `Manifest.Validate` (run by
   `NewClientModule`) additionally rejects such a manifest loudly at
   construction.
2. The framed asset's `Content-Security-Policy` carries `sandbox
   allow-scripts`, so even a **top-level** load of the frame document
   (not just an embed) is forced into an opaque sandbox by the browser.

Consequences the browser enforces (not our code, not review):

- `document.cookie`, `localStorage`, `sessionStorage`, the host DOM and
  globals, the CSRF token, and other plugins' data are **unreachable**
  from the frame.
- The frame has no network capability of its own; its only channel to
  the app is `postMessage`, brokered by the host.
- A crashed or malicious bundle cannot deface the page or exfiltrate a
  session, including via a compromised transitive npm dependency, which
  is the realistic threat: the app owner *deliberately installs* the
  plugin, but nobody audits megabytes of dependency tree per upgrade.

Assets are served **same-origin** from the plugin's route prefix via
`pluginhost.NewAssetServer`, so the app's strict CSP needs zero edits.
Framed assets get a scoped relaxation (framing headers + a CSP keyed to
the explicit request origin; inside an opaque frame, `'self'` resolves
to `null` and spec-correct browsers like Safari refuse subresources).

## The protocol

One versioned envelope in both directions:
`{v, id, type: request|response|event, src, method, params, result, error}`.

- Handshake: the frame speaks first (`ready`), the host answers `init`
  with the document, theme tokens, and the capability grant set.
- host→plugin: `init`, `themeChanged`, `teardown`, `hostPointerdown`
  (interaction-outside relay so in-frame overlays can dismiss).
- plugin→host: `ready`, `docChanged`, `save`, `resize`, `focusChanged`,
  `metric`, `themeApplied`, `bootError`.
- Legacy correlated-event pairs (`requestSave`/`uploadResult`,
  `requestUpload`) predate the request channel below; new plugins use
  `sendRequest`/`onRequest` instead of inventing paired events.
- **Source validation:** `event.source === iframe.contentWindow`, never
  `event.origin`; an opaque frame's origin is the literal string
  `"null"`, so origin-string checks are a trap.
- Unknown methods are ignored, so additive events are non-breaking.

The host side is `framework/pluginhost/host/pluginhost.js`, served at
its own route (`pluginhost.RegisterBrokerRoute`, idempotent across
plugins). It is **not** part of `runtime.js`; pages without plugins
ship zero extra bytes and the core payload budgets are untouched.

### Requests, both directions

`request`/`response` correlation is platform-owned in both directions —
plugins never hand-roll a request id, a pending map, or a timeout.

The frame side is `framework/pluginhost/frame/frameclient.js`
(`pluginhost.RegisterFrameClientRoute`, served with the framed CORP
relaxation so opaque-origin frame documents can load it; or bundle
`pluginhost.FrameClientJS()`). Inside the frame:

```js
var client = window.__gofastrPluginFrame;
client.onEvent("init", function (params) { /* theme, doc, caps */ });
client.onRequest("getState", function (params) { return state; });
client.ready({ domReady: true }).then(function (init) { /* … */ });
client.sendRequest("rows", { page: 2 }, 5000).then(render, showError);
```

On the host, an adapter answers frame requests with per-instance
handlers (`api.onRequest(method, handler)`) or a static
`registration.onRequest(method, params, api)` fallback.

One contract, both directions:

- A request is **always answered**: no handler → an
  `{code: "E_NO_HANDLER"}` error response; a handler throw/rejection →
  `{code: "E_HANDLER"}`. Silence is not a protocol state.
- The in-flight map is bounded (64); a saturated sender gets an
  immediate `{code: "E_SATURATED"}` rejection and nothing is posted.
  The bound is sender-side: inbound dispatch is uncapped, the same
  trust posture as the event path, so host-side request handlers must
  stay cheap or debounce, exactly like `onEvent` handlers.
- Invalid timeouts fall back to the 5s default; a timed-out request
  rejects `{code: "E_TIMEOUT"}` and its late response is dropped by id.
- Teardown rejects every outstanding request with
  `{code: "E_TEARDOWN"}` on both sides (the frame also fails
  outstanding requests on `pagehide`), so no promise ever hangs.

## Capabilities: reuse the scope registry, don't invent one

Grants use the **same `resource:verb` grammar as battery/auth token
scopes** (`document:read`, `document:write`, `upload:images`,
`theme:read`) and are enforced server-side with **default-deny**:
`pluginhost.Allow(ctx, granted, required)` permits an action only when
`required` is covered by the plugin's `granted` set (the ceiling, via
`auth.ScopeMatch`, the same wildcard matcher as token scopes) AND the
caller's own authority permits it. A plugin can therefore never exceed
its granted capabilities, even under a session cookie (where an unscoped
`auth.HasScope` alone would pass everything). Mount privileged plugin
routes behind `pluginhost.Guard(granted, required, next)`, which fails
**closed** with `403 E_CAPABILITY_DENIED`. This is the reconciliation
#37 calls for: one permission vocabulary across process-isolated modules,
API tokens, and client plugins. Do not build a parallel capability
catalog for plugins; extend the scope vocabulary.

The client half is advisory UX (the editor hides upload UI without
`upload:images`); the server half is the enforcement (the upload route
403s without the scope). Never trust the frame's own claim of its
grants.

## Host-page requirements

A manifest describes what the FRAME gets. Some plugins also need something
from the page around them: a barcode scanner built the way [issue #273](https://github.com/DonaldMurillo/gofastr/issues/273)
recommends — host page captures, sandboxed frame decodes — needs the HOST
page's `getUserMedia` to work. GoFastr's default security headers send
`Permissions-Policy: geolocation=(), microphone=(), camera=()`, which denies
those features to the page itself, so the scanner dies with a console error
a user only sees after clicking the control that starts the camera:

```
Permissions policy violation: camera is not allowed in this document.
```

The default denial is deliberate: no page should silently turn on a camera
or microphone, and `SecurityHeadersConfig.PermissionsPolicy` is the
one-line opt-out. What was missing is a way for a plugin to SAY it needs
that opt-out. `Manifest.HostRequirements` is it:

```go
m, err := pluginhost.NewClientModule("scanner", pluginhost.Manifest{
	Entry: "/__gofastr/plugin/scanner/scan.html",
	HostRequirements: []string{
		"permissions-policy:camera",
	},
}, assets)
```

Tokens are `permissions-policy:<feature>` against a closed registry of the
Permissions-Policy spec's policy-controlled features (`camera`,
`microphone`, `geolocation`, `clipboard-write`, `fullscreen`, ...).
`Manifest.Validate` rejects anything else at registration — unknown
prefix, typo'd feature, embedded header syntax — so a bad declaration is
a build error, not a silently unsatisfiable requirement. The frame itself
is opaque-origin and can never hold these permissions; the declaration is
about the host page, and the working shape is always host-page capture +
frame processing.

### The boot check

There is no central registry of client modules to hook, so the check is a
helper the app calls once at startup, next to where it wires its security
headers:

```go
secCfg := middleware.SecurityHeadersConfig{} // or your own policy
pluginhost.CheckHostRequirements(slog.Default(), secCfg.PermissionsPolicy, scanner)
```

It logs and never fails — a plugin cannot take an app down by declaring
something. An empty `PermissionsPolicy` (the untouched default) is treated
as the framework's default header, so the exact case above is caught. The
warning names the plugin, the token, and the fix:

```
WARN plugin requires a host-page permission the Permissions-Policy denies
     plugin=scanner requirement=permissions-policy:camera
     policy=geolocation=(), microphone=(), camera=()
     fix="allow it on the host page, e.g. camera=(self), or unset the empty allowlist camera=()"
```

The fix for the scanner is then one config line:

```go
secCfg.PermissionsPolicy = "geolocation=(), microphone=(), camera=(self)"
```

The check warns only when it is confident: it fires when every directive
naming the required feature carries the empty allowlist `()`, the one
Permissions-Policy shape that unambiguously denies the feature to the page
itself. `camera=(self)`, `camera=*`, an unnamed feature, or an origin list
stay silent — origin lists cannot be decided at boot at all, and a warning
that fired on grants would train developers to ignore the check.

See [security headers](security.md) for the full default header set and
what each field controls.

## Mounting

`pluginhost.MountMarker` emits the mount marker the broker scans for:
`data-fui-plugin="<name>"` plus `data-fui-plugin-docid` / `-doc` /
`-minheight` / `-capabilities` (all documented in the
core-ui/ARCHITECTURE.md attribute table and the
[runtime contract](runtime-contract.md)). A plugin adds its own
adapter script (registered via `window.__gofastrPluginHost.register`)
that supplies its `Manifest` and handles its plugin-specific events;
the generic broker owns everything protocol-level.

### Progressive enhancement: MountConfig.Fallback

A plugin with a Go-side renderer sets `MountConfig.Fallback` to a
`render.HTML` node — the chart plugin's pure-Go SVG, say. The broker
wraps it inside the marker and drives one lifecycle:

- **loading** → the fallback is visible, the frame hidden;
- **ready** → the frame's live view takes over, the fallback is hidden
  (not removed — recovery stays possible);
- **bootError** → the fallback shows again. This is the load-bearing
  half: a frame that dies degrades to the static server-rendered node,
  not an empty box, and the page still works with JavaScript off.

The fallback is host-trusted HTML in the page's own trust domain, built
server-side by the plugin's `Mount()`; it never comes from the frame.
It renders in the **host page** (full privileges), not the sandbox, so
escape any user-derived data you interpolate into it
(`render.Escape` / `render.Text`) — an unescaped label here is stored
XSS in the host page, the very thing the frame sandbox exists to
prevent. Plugins without a fallback keep the frame visible while
loading, exactly as before.

## Opting out: the trusted mount

Isolation is the default; a **loud, host-side opt-out** exists for
plugins the app owner compiles in and vouches for (code the team wrote
itself, where the geometry/theming costs of the frame aren't worth
paying).
The wysiwyg plugin's `WithTrustedMount()` is the reference: same plugin
API and protocol envelopes, transport swapped from postMessage to
direct calls, no iframe. The opt-out is never a default and never
selectable by the plugin itself; only the host can grant it.

## The registry

Discovery is a **convention, not a service**: the `gofastr-plugins`
repo carries a curated `plugins.json` (module path, version,
`frameworkCompat`, isolation, sandbox, capabilities, entry route,
schema version per plugin). An app imports a plugin package directly
and mounts it with `app.RegisterPlugin(...)`; the registry file is the
human/tooling index, updated in the same change as a plugin's version
or capability set.

## Common mistakes

- **Adding `allow-same-origin` to "fix" a frame that can't load its
  assets.** That de-opaques the frame and deletes the entire isolation
  guarantee. The real fix is the framed-asset CSP relaxation the
  `AssetServer` already applies: `'self'` means `null` inside an
  opaque frame, so framed responses carry an origin-keyed CSP instead.
- **Checking `event.origin` in the broker or the frame.** The opaque
  frame's origin is the string `"null"`; string checks either always
  fail or get written as `origin === "null"`, which any sandboxed frame
  on any site satisfies. Compare `event.source` identity instead.
- **Treating the client capability list as enforcement.** It is UX.
  Enforcement is `pluginhost.Guard` / `pluginhost.Allow` (default-deny,
  grant-set ∩ caller-authority) on the plugin's server routes; a hostile
  frame can claim any grants it likes.
- **Putting the broker into `runtime.js`.** It belongs on its own
  route: plugin pages are rare, the core payload budget (12.5 KB gz) is
  load-bearing, and `RegisterBrokerRoute` is already idempotent.
- **Inventing plugin-only permission names.** Use the `resource:verb`
  scope grammar so token scoping, wildcards, and admin tooling keep
  working; a parallel vocabulary drifts immediately.
- **Letting a plugin choose its own trust tier.** `isolation` in the
  manifest describes the sandboxed default; the trusted in-page mount
  is granted only by host-side code the app owner writes.
- **Expecting the sandboxed frame to hold the permission itself.** The
  opaque-origin frame can never be granted `camera`, `microphone` or any
  other policy-controlled feature. Declare what the HOST page needs via
  `Manifest.HostRequirements` and use the host-captures / frame-decodes
  shape; `CheckHostRequirements` says at boot when the app's
  `Permissions-Policy` denies it.
