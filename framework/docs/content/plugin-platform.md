# Heavy-JS plugin platform (`framework/pluginhost`)

`framework/pluginhost` lets a GoFastr app mount a **heavy-JavaScript
plugin** — a megabyte-class client bundle like a WYSIWYG editor or a
diagram renderer — as a genuinely third-party, isolated module. It is
the client-side mirror of the process-isolation track (#37): the same
question ("what can code we didn't audit actually reach?") answered for
untrusted DOM-touching JavaScript.

It was not designed up front. It was distilled from the first such
plugin (the `gofastr-plugins` WYSIWYG editor) after the isolation model
survived a measured go/no-go gate (p99 keystroke latency ≤ 16 ms inside
the sandbox), then proven general by a second plugin (mermaid) that
reused it without modification.

## The isolation model — secure by default

The plugin bundle runs inside an **opaque-origin sandboxed iframe**:
`sandbox="allow-scripts"`, and `allow-same-origin` is **never** added.
Two independent, authoritative enforcement points guarantee this:

1. The sandbox derivation (`Manifest.SandboxString` server-side and
   `sandboxFor` in the broker JS) **always** strips `allow-same-origin`
   and forces `allow-scripts` — a mis-configured or tampered manifest
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
  session — including via a compromised transitive npm dependency, which
  is the realistic threat: the app owner *deliberately installs* the
  plugin, but nobody audits megabytes of dependency tree per upgrade.

Assets are served **same-origin** from the plugin's route prefix via
`pluginhost.NewAssetServer`, so the app's strict CSP needs zero edits.
Framed assets get a scoped relaxation (framing headers + a CSP keyed to
the explicit request origin — inside an opaque frame, `'self'` resolves
to `null` and spec-correct browsers like Safari refuse subresources).

## The protocol

One versioned envelope in both directions:
`{v, id, type: request|response|event, src, method, params, result, error}`.

- Handshake: the frame speaks first (`ready`), the host answers `init`
  with the document, theme tokens, and the capability grant set.
- host→plugin: `init`, `themeChanged`, `requestSave`, `uploadResult`,
  `teardown`, `hostPointerdown` (interaction-outside relay so in-frame
  overlays can dismiss).
- plugin→host: `ready`, `docChanged`, `save`, `requestUpload`, `resize`,
  `focusChanged`, `metric`, `themeApplied`, `bootError`.
- **Source validation:** `event.source === iframe.contentWindow`, never
  `event.origin` — an opaque frame's origin is the literal string
  `"null"`, so origin-string checks are a trap.
- Unknown methods are ignored, so additive events are non-breaking.

The host side is `framework/pluginhost/host/pluginhost.js`, served at
its own route (`pluginhost.RegisterBrokerRoute`, idempotent across
plugins). It is **not** part of `runtime.js` — pages without plugins
ship zero extra bytes and the core payload budgets are untouched.

## Capabilities — reuse the scope registry, don't invent one

Grants use the **same `resource:verb` grammar as battery/auth token
scopes** (`document:read`, `document:write`, `upload:images`,
`theme:read`) and are enforced server-side with **default-deny**:
`pluginhost.Allow(ctx, granted, required)` permits an action only when
`required` is covered by the plugin's `granted` set (the ceiling — via
`auth.ScopeMatch`, the same wildcard matcher as token scopes) AND the
caller's own authority permits it. A plugin can therefore never exceed
its granted capabilities, even under a session cookie (where an unscoped
`auth.HasScope` alone would pass everything). Mount privileged plugin
routes behind `pluginhost.Guard(granted, required, next)`, which fails
**closed** with `403 E_CAPABILITY_DENIED`. This is the reconciliation
#37 calls for — one permission vocabulary across process-isolated modules,
API tokens, and client plugins. Do not build a parallel capability
catalog for plugins; extend the scope vocabulary.

The client half is advisory UX (the editor hides upload UI without
`upload:images`); the server half is the enforcement (the upload route
403s without the scope). Never trust the frame's own claim of its
grants.

## Mounting

`pluginhost.MountMarker` emits the mount marker the broker scans for:
`data-fui-plugin="<name>"` plus `data-fui-plugin-docid` / `-doc` /
`-minheight` / `-capabilities` (all documented in the
core-ui/ARCHITECTURE.md attribute table and the
[runtime contract](runtime-contract.md)). A plugin adds its own
adapter script (registered via `window.__gofastrPluginHost.register`)
that supplies its `Manifest` and handles its plugin-specific events —
the generic broker owns everything protocol-level.

## Opting out — the trusted mount

Isolation is the default; a **loud, host-side opt-out** exists for
plugins the app owner compiles in and vouches for (code the team wrote
itself, where the geometry/theming costs of the frame aren't worth
paying).
The wysiwyg plugin's `WithTrustedMount()` is the reference: same plugin
API and protocol envelopes, transport swapped from postMessage to
direct calls, no iframe. The opt-out is never a default and never
selectable by the plugin itself — only the host can grant it.

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
  `AssetServer` already applies — `'self'` means `null` inside an
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
  route: plugin pages are rare, the core payload budget (12 KB gz) is
  load-bearing, and `RegisterBrokerRoute` is already idempotent.
- **Inventing plugin-only permission names.** Use the `resource:verb`
  scope grammar so token scoping, wildcards, and admin tooling keep
  working; a parallel vocabulary drifts immediately.
- **Letting a plugin choose its own trust tier.** `isolation` in the
  manifest describes the sandboxed default; the trusted in-page mount
  is granted only by host-side code the app owner writes.
