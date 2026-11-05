# Proposal: presentation-host extension seams

**Status: proposed; not implemented by this PR.** This document specifies two
follow-ups to the host seams merged in PR #663. It adds no exported API, runtime
behavior, support-tier change, or dependency. The API names remain subject to
implementation review; none of the proposed operations is callable today.

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

## Existing capabilities and demonstrated gaps

The following are available today:

- `framework.App.Serve(net.Listener)` runs the complete application lifecycle
  using a host-supplied listener, including an in-process listener.
- The web runtime's `window.__gofastr_transport` slot lets a host carry runtime
  requests over its own transport.
- `framework/localapp` owns installation-local data and identity.

See [local apps](../../framework/docs/content/local-apps.md),
[plugins](../../framework/docs/content/plugins.md), and the
[runtime contract](../../framework/docs/content/runtime-contract.md).

Two gaps remain:

1. A WebView presentation that must durably checkpoint a draft before leaving
   currently composes the router's internal `_interceptPopstate` and `_pushURL`
   hooks. These are not a documented external registration contract.
2. A native presentation can omit web screens, but still needs in-memory Serve
   to obtain the complete startup/shutdown lifecycle. Calling `InitPlugins` or
   migrations manually is not an equivalent lifecycle.

The existing prototype works with these adapters. Neither gap justifies moving
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

## Implementation order and migration

1. Implement the public web navigation guard with contract documentation and
   browser acceptance tests. Migrate gofastr-native away from property wrapping
   only after both workspace and released/pinned-consumer tests pass.
2. Extract the shared lifecycle phases and add listener-free activation with the
   startup-failure/concurrency acceptance matrix. Keep the existing in-memory
   Serve adapter until this complete contract is available.
3. Replace the consumer's adapters, update dependency pins, and document actual
   supported APIs in the embedded docs and architecture contract in the same
   implementation PRs. Native UIKit and WebView must both remain supported.

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
- Next time: implement against the existing lifecycle/router paths and make the
  acceptance guards fail deliberately before claiming the new seams are ready.
