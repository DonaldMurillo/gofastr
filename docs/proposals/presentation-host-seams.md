# Proposal: presentation-host extension seams

**Status: implemented in GoFastr; WebKit acceptance and consumer migration
pending.** This document
specified two follow-ups to the host seams merged in PR #663. Where each one
stands now:

| Seam | GoFastr | gofastr-native |
|---|---|---|
| Listener-free lifecycle ([section 2](#2-listener-free-application-lifecycle)) | Implemented: `App.Run`, `framework.Host`, `framework.ServicesHost`, `App.ServicesReady` and `framework.ErrAlreadyStarted`. Reference: [Application hosts](../../framework/docs/content/application-hosts.md). | Not migrated. Its `mobilehost` service-only mode still runs the App through `Serve` on an in-memory listener. |
| Navigation guards ([section 1](#1-supported-asynchronous-navigation-guards)) | Implemented as the `navguard` runtime module (`__gofastr.guardNavigation`) and core's `__gofastr.replaceURL`, with Chromium browser tests that also run with the Navigation API removed. Reference: [runtime contract](../../framework/docs/content/runtime-contract.md). | Not migrated. Its WebView presentation still wraps `_interceptPopstate` and `_pushURL`. No WebKit (Safari, WKWebView) run has accepted the guard. |

Neither seam changes a support tier in [Public API and support
tiers](../public-api.md): both ship in the Provisional `framework/` and
`core-ui/` trees. The sections below keep the original goals and ownership.
Each ends with what is implemented and what is still open.

The motivating consumer is
[`gofastr-native`](https://github.com/DonaldMurillo/gofastr-native): a separate
project where applications author Go screens, actions, behaviors, and device
service requests. Presentation libraries render those definitions with UIKit or
a WebView. Existing whole-web GoFastr applications remain supported too.

## Scope and ownership

Keep GoFastr web-first. Add small public extension points where a presentation
package currently needs an internal hook or an unnecessary transport. Do not
add a mobile widget tree, native theme, device-service registry, Swift/Kotlin
renderer, or universal navigation stack to core.

| Owner | Responsibility |
|---|---|
| GoFastr | Application lifecycle, data and authorization services, and supported extension points in its web router. |
| Presentation package | Its screen definitions, rendering, component styling, focus, gestures, and renderer-specific navigation. |
| Platform host | OS lifecycle and capabilities, including keyboard, camera, notifications, and sharing. |
| Application | Go-authored task screens and actions composed from its presentation package. No app-owned CSS, JavaScript, or native screen templates. |

For GoFastr web presentations, the existing design-system ownership rules remain
unchanged. A separate native presentation package owns its own controls and
rendering; it must not patch GoFastr's private styles. Sharing application
services must not require every renderer to implement an HTTP response, return
HTML, or adopt a common widget representation. Desktop, canvas, and terminal
renderers are possible consumers of the lifecycle seam, not implementations
promised by this proposal.

The implemented seams keep this split. `ServicesHost` and a custom `Host` run
the App's services; the presentation package still owns every pixel it draws.
The guard API is for renderer libraries. Applications keep authoring screens and
actions in Go and do not write JavaScript to use it.

## Capabilities before this work, and the gaps

These were available before either seam:

- `framework.App.Serve(net.Listener)` runs the complete application lifecycle
  using a host-supplied listener, including an in-process listener.
- The web runtime's `window.__gofastr_transport` slot lets a host carry runtime
  requests over its own transport.
- `framework/localapp` owns installation-local data and identity.

See [local apps](../../framework/docs/content/local-apps.md),
[plugins](../../framework/docs/content/plugins.md), and the
[runtime contract](../../framework/docs/content/runtime-contract.md).

Two gaps remained:

1. A WebView presentation that must durably checkpoint a draft before leaving
   composed the router's internal `_interceptPopstate` and `_pushURL` hooks.
   These are not a documented external registration contract. Section 1's
   guard is the supported replacement; the consumer has not switched yet.
2. A native presentation could omit web screens, but still needed in-memory
   Serve to obtain the complete startup/shutdown lifecycle. Calling
   `InitPlugins` or migrations manually is not an equivalent lifecycle.
   `App.Run` with `ServicesHost` now closes this gap in GoFastr.

The existing prototype works with these adapters. Neither gap justified moving
native screens or device behavior into GoFastr core.

## 1. Supported asynchronous navigation guards

### Proposed contract

Expose a documented, composable guard-registration operation in the web runtime.
Registration returns an idempotent disposer; packages must not replace router
properties or depend on listener registration order. This is a renderer-library
API, not a requirement for application authors to write JavaScript.

Each navigation attempt supplies:

- A unique attempt identity, the source and destination URLs, and source and
  destination history-entry identities where known. URL equality alone is not
  entry equality.
- Intent: push, replace, history traversal, or a managed overlay departure.
- A cancellation signal for superseded attempts or disposed registrations.

A guard can allow or deny immediately, or return an asynchronous decision. The
router must await that decision before replacing the leaving screen. A rejected
decision denies departure and reports the error without discarding the editor.
Guards run in documented registration order and stop at the first denial.

The initial use case is a durable draft barrier: the renderer calls its Go
behavior, persists Keep/checkpoint state, then allows navigation only after an
acknowledgement for the current editor revision. A failed persistence operation
must leave the screen and buffer available; a delayed acknowledgement from an
obsolete attempt must not authorize a newer departure.

### Required router behavior

- Cover router-managed links, programmatic navigation, and Back/Forward. Define
  the managed form/redirect paths covered by the implementation rather than
  claiming that browser document unloading is universally interceptable.
- Compose with intercepted overlays and their existing leave guards. Closing an
  overlay and leaving a page must not create independent competing navigators.
- On `popstate`, account for the browser having already changed history. Preserve
  the leaving DOM and reconcile or restore the entry before a deferred decision;
  permit one approved traversal without running the same guard again. Unknown
  or foreign history must fail safely without fabricating a destination entry.
- A newer attempt cancels the older decision's authority to navigate. Cancellation
  is not proof that an already-started checkpoint or mutation was undone.
- Do not replay application writes, saves, or OS effects to recover navigation.
  Dispose must revoke pending decisions and release listeners and subscriptions.
- Preserve existing no-guard behavior, client-side navigation, scroll restoration,
  and view-transition/reduced-motion behavior. No full reload as a cancellation
  or state-recovery shortcut.

Hard unloads, process termination, and uncontrolled external navigation remain
outside this guard's durability guarantee. Background checkpointing is still a
presentation/host responsibility. This is not a new tab or native-stack router.

### Acceptance tests

Use real-browser tests through the router, not a second simulated history
implementation:

- Dirty editor: Back, Forward, links, and programmatic departures wait for a
  deliberately delayed durable acknowledgement.
- Denial or checkpoint failure preserves URL/history consistency, DOM, input,
  focus, and the user's ability to try another action.
- A late allow from a superseded attempt cannot navigate; repeated URLs with
  different entry identities are handled correctly.
- A guarded overlay departure composes with its page's guard; registering and
  disposing multiple packages does not overwrite another package's behavior.
- Reloaded and foreign history, rapid traversals, scroll restoration, and
  reduced-motion navigation retain their documented behavior.

### Status

The guard is in the runtime as the `navguard` demand module, beside core's
`__gofastr.replaceURL`. Their contract is the
[runtime contract](../../framework/docs/content/runtime-contract.md)'s
"Guarding departures" section, with "History entries", "Back and Forward
under a guard", "When there is no known way back" and "Rewriting the
address"; this summary does not replace it.

Implemented:

- A renderer package calls `await __gofastr.loadModule('navguard')`, then
  `__gofastr.guardNavigation(fn)`. The call returns a disposer that is safe to
  call more than once. Nothing loads the module on its own.
- Guards run in registration order, one at a time. Each returns allow, deny or
  a promise of either; a throw or a rejection denies and is reported. The first
  denial ends the decision.
- Each attempt is frozen: `id`, `intent` (`push`, `replace`, `traverse` or
  `overlay`), `from` and `to` as `{url, entry}`, `delta`, `signal` and
  `affects(node)`. `entry` is the Navigation API entry key where the browser
  has the API. Elsewhere it is read off the tag core writes on every history
  entry it makes (`history.state.__fui`, the entry's id, and `__fup`,
  `[numbering, position]`), as `numbering:position:id`. It is `null` for an
  entry that does not exist yet or that no router wrote. One URL can name
  several entries; the entry, never the URL, says which. A superseded or
  disposed attempt's signal aborts and its late answer authorizes nothing.
- A blocked move dispatches `gofastr:navigateblocked` on `window` with the
  attempt, a reason (`denied`, `error`, `disposed`, `pending` or `unknown`)
  and any error.
- `__gofastr.replaceURL(url)` replaces the URL of the entry the browser is on
  and does nothing else: no fetch, no guard asked, no new entry. The entry
  keeps its id, position and state. It returns `false` and writes nothing for
  another origin, a path outside the app's base, another document, a write
  the browser refuses, or, with the module loaded, while a guarded Back or
  Forward is unresolved. It is the supported spelling of the
  `_pushURL(url, {replace: true})` call a presentation used to make.

A Back or Forward the guards do not allow at once is undone first, so the
page's DOM, inputs, focus and scroll stay as they were. With the Navigation
API the module returns to the shown entry by key. Without it (Safari before
26.2, Firefox before 147, and WKWebView on the matching OS releases) it calls
`history.go` with the distance between the two entries' tags. On allow it goes
to the destination entry again, and only that landing reaches the router,
without a second ask. No entry is added, dropped or rewritten. While one of
these traversals is in flight, a new link or `navigate()` is refused
(`pending`) and `replaceURL()` returns `false`, so no write from the wrong
entry cuts off forward history.

The module knows no way back when the landing has no tag (history code outside
the runtime wrote it), when the two entries are in different numberings (a
`#fragment` jump's entry starts one of its own), when `traverseTo` throws or
its commit rejects, or when a fallback `history.go` does not land after a few
tries. Then it pushes nothing: the browser stays on the landing and the page
on the entry it shows. Allowed, the landing goes on to the router. Denied,
that landing alone takes the page's URL and keeps its own state and identity,
and the report says `unknown`. The module never pushes an entry of its own to
recover: pushing the page's URL again would fabricate an entry and cut off the
forward history.

The browser tests are `core-ui/runtime/navguard_e2e_test.go` (delayed
checkpoints, denial, superseded decisions, rapid traversals, repeated URLs,
composition and disposal, overlay departures, no-guard behavior, scroll and
transitions) and `core-ui/runtime/navguard_history_e2e_test.go` (the history
contract: denied Back and Forward, a second Back while pending, repeated URLs,
reload, replace and branch, foreign entries, a refusing `traverseTo`, the leave
guard, the intercept stack, envelope navigation, RPC navigation and
`replaceURL`, plus mutations that each break one scenario). Its `no-api` cases
hide the Navigation API in Chromium; they are not Safari or Firefox.

What the guard is asked about depends on how the page leaves. The runtime
contract's departure table is authoritative; in short:

- Router-managed departures (a link the router takes, `navigate()`, a
  `data-cui-spa` GET form) are asked before anything is discarded. An RPC
  button whose answer redirects is asked after its write, so a denial keeps
  the screen but does not undo the write.
- Back and Forward between router pages are asked after the browser has moved
  its history; the module restores the shown entry while the decision is
  pending, as above.
- URL-only writes (`replaceURL()`, pagination, widget deep links,
  `X-Gofastr-Push-State`) depart nothing and are not asked. A guard therefore
  sees fewer events than a wrapper of `_pushURL` did, and a presentation that
  tracked those writes needs its own record of them. A later Back or Forward
  across their entries is asked like any other.
- Hard unloads (reload, tab close, the address bar, external and
  document-boundary links) and an intercepted POST or JSON form whose answer
  loads a new document are not asked. Only `beforeunload` can prompt there.
- History written by code outside the runtime is not asked when written. A
  later traversal onto it is asked when its URL differs from the router's
  page, and without the Navigation API it has no known way back, since the
  entry carries no tag. A traversal onto another document's entry is not
  asked.

Open:

- WebKit acceptance. No run on Safari or a WKWebView has accepted the guard,
  with or without the Navigation API; the `no-api` cases above are Chromium
  with the API removed. A WebView consumer needs its own run on the engine it
  ships, including the browser's own history limits (Safari throttles history
  writes, which `replaceURL()` reports as `false`).
- The kit's leave guard (`ui.FormConfig.LeaveGuard`) keeps its own repair when
  it declines a Back or Forward the guards allowed: by the distance its own
  tags give, or, when it has none, a push of the page's URL again. This work
  does not change that path.
- gofastr-native still wraps `_interceptPopstate` and `_pushURL`. It switches
  to the guard and `replaceURL` only after its workspace and pinned-dependency
  tests pass against a GoFastr version that carries them.

## 2. Listener-free application lifecycle

### Proposed contract

Expose a supported operation that activates an application's services without
creating or starting an HTTP server, listener, or client. Application
construction remains separate from activation. Web serving stays the default;
`Start` and `Serve` retain their contracts.

The listener-free path must share the existing lifecycle implementation rather
than copy its phases into an external module. It must preserve:

- Schema registered during plugin initialization, migration and seed ordering,
  and the existing startup-hook ordering and application lifecycle context.
- Startup failure rollback, cancellation, background-worker cleanup, resource
  ownership, and the existing idempotent Shutdown contract.
- A well-defined ready state only after all required initialization succeeds.
  Transport readiness and hooks requiring an address must be distinguished from
  service readiness; never invent a URL or pretend that a socket was bound.
- Concurrent start/stop semantics. The proposal recommends one activation per
  App instance: reject duplicate/conflicting starts, prevent ready-after-stop,
  and construct a new App for restart rather than imply same-instance restart
  support that has not been implemented.

Before implementation, settle the activation operation's exact signature and
context lifetime: whether its context bounds startup only or owns the running
application too. Document how cancellation reaches rollback and Shutdown in
either case. Also specify how HTTP-dependent plugins/hooks declare their
requirement and whether listener-free activation rejects them. Do not silently
skip required initialization.

This operation removes the need to *run* HTTP for native services. It does not
promise that the framework has no HTTP imports, that HTTP routes cease to exist,
or that the binary sheds every web-related dependency. It does not change CRUD
authorization or replace the separate typed-repository work.

### Acceptance tests

Exercise the public lifecycle against a real temporary installation/database:

- Plugin-declared schema exists before seed/start consumers need it; installation
  identity and persisted data survive shutdown and reconstruction.
- Initialization, migration, seed, and startup-hook failures each unwind resources
  and workers without publishing readiness.
- Cancellation, concurrent stop during startup, duplicate starts, and repeated
  Shutdown calls obey the selected context/activation contract.
- Native service operations remain owner-authorized without a web presentation.
- `Start` and caller-supplied `Serve` keep their HTTP behavior, ready hooks,
  listener ownership, and graceful-shutdown guarantees.

Tests must prove that listener-free activation creates no server/listener, not
merely that no request was sent. A shutdown budget cannot forcibly terminate a
hook that ignores cancellation; document cooperative-cancellation limits.

### Status

Implemented in `framework/host.go` and `framework/host_http.go`. The
[Application hosts](../../framework/docs/content/application-hosts.md)
reference is the contract; this summary records the decisions the proposal
left open.

- **The operation.** `App.Run(ctx, host)` boots the App and hands it to a
  `framework.Host`, which has two methods, `Run(ctx, *Session) error` and
  `Stop(ctx) error`. `Session` is opaque. `Start` and `Serve` are `Run` with
  the built-in HTTP host, so all three share one boot order, rollback and
  `Shutdown`. `framework.ServicesHost()` constructs no `http.Server`, listener
  or HTTP client.
- **Context lifetime.** `ctx` owns the whole run, not only startup. When it is
  done, `Run` shuts the App down with the deadline the signal handler uses
  (`AppConfig.ShutdownTimeout` plus the readiness grace) and returns
  `Shutdown`'s error. Boot phases, `OnStart` hooks and `Host.Run` get the App's
  lifecycle context, which keeps `ctx`'s values and which only `Shutdown`
  cancels. `Run` installs no signal handler; `Start` still does.
- **Readiness.** `App.ServicesReady()` closes after every boot phase succeeds
  and never closes when boot fails or `Shutdown` comes first. `OnReady` stays
  HTTP transport readiness. Under a host with no listener, `Run` refuses an App
  with an `OnReady` hook (before boot, or as a rollback when a plugin or
  `OnStart` hook registers one) and refuses interactive first-run setup.
  Headless setup runs. Routes, `WrapServer`, TLS and the server timeouts stay
  configured and unused.
- **One activation per App.** A second `Run`, `Start` or `Serve` returns
  `framework.ErrAlreadyStarted`. One that begins after `Shutdown` returns nil
  and starts nothing. A restart builds a new App.
- **Stop during boot.** `Shutdown` cancels the lifecycle context, the running
  phase returns, no later phase starts, `Host.Run` is never called and
  `Host.Stop` is. The teardown runs on the context of the `Shutdown` that began
  it. If that context ends before the phase returns, `Shutdown` returns its
  error and the teardown still runs once the phase returns, on a fresh
  five-second budget that keeps the context's values. A failure of that
  teardown is logged, because the caller that began it has stopped waiting.
- **Errors.** A phase that returns only the stop's own cancellation
  (`context.Canceled`, `sql.ErrTxDone` for a transaction rolled back by it, or
  SQLite's `SQLITE_INTERRUPT`) is a graceful stop and `Run` returns nil. Any
  other phase error fails `Run` even while a stop is pending. Whoever began a
  teardown reports its error once; `Run` joins a rollback's teardown error to
  the boot error. The `OnStart` hooks that interactive setup defers follow the
  same split: one stopped by a `Shutdown` is silent, and any other failure is
  logged and shuts the App down.
- **Cooperative cancellation.** Nothing terminates a goroutine. A boot callback
  that calls `Shutdown` synchronously waits for a teardown that cannot start
  until the callback returns: it returns at its context's deadline, and with no
  deadline it never returns. A `Host.Stop` that ignores its context holds the
  teardown. The reference names the safe forms: return an error from the
  callback, or call `Shutdown` on another goroutine.

The acceptance list above is covered by the `framework/host_*_test.go` files.
The no-transport proof runs in a fresh process and checks that no socket
descriptor, `net/http` server, connection or client goroutine, or
`http.Server` appears once the App serves. The local-installation test runs
plugin-declared schema, phase order, owner-scoped data, and identity, secret
and rows persisting across a rebuilt App under `ServicesHost`.

Open:

- gofastr-native's service-only mode still calls `Serve` on an in-memory
  listener. Moving it to `ServicesHost` is a separate change in that
  repository, followed by a dependency pin on a GoFastr version that carries
  `App.Run`.
- A host that runs `ServicesHost` installs the owner extractor itself, as an
  HTTP host does: `localapp.Install()` for a one-person app (see
  [local apps](../../framework/docs/content/local-apps.md)). Without it an
  owner-scoped call fails with "owner context required" even when the
  context carries `data.User`.

## Implementation order and migration

1. Implement the public web navigation guard with contract documentation and
   browser acceptance tests. *Implemented in GoFastr, with Chromium tests run
   with and without the Navigation API; WebKit acceptance has not run.* Migrate
   gofastr-native away from property wrapping only after both workspace and
   released/pinned-consumer tests pass.
2. Extract the shared lifecycle phases and add listener-free activation with the
   startup-failure/concurrency acceptance matrix. *Implemented in GoFastr.* Keep
   the existing in-memory Serve adapter until the consumer migrates.
3. Replace the consumer's adapters, update dependency pins, and document actual
   supported APIs in the embedded docs and architecture contract in the same
   implementation PRs. Native UIKit and WebView must both remain supported.
   *Not started in gofastr-native.*

Keep these implementation changes independently reviewable; neither needs to
wait for a universal renderer interface. No release or removal of the existing
adapters is implied by merging this proposal.

## Implementation notes

- Inspect the consumer's `go.work` and `go.mod`: a sibling framework checkout
  need not be the version used by its build. Test the selected workspace and
  the public dependency separately.
- The consumer's draft/navigation and native lifecycle contracts are recorded
  in `docs/mobile-reliability.md` and `docs/mobile-lifecycle.md` in gofastr-native.
  Compilation is not native gesture, keyboard, or provider acceptance.
- Implement against the existing lifecycle/router paths and make the
  acceptance guards fail deliberately before claiming the new seams are ready.
