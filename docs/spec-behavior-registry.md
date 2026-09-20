# Spec: behaviour registers like style

Status: accepted and underway, proposed 2026-09-15. Steps 1–3 of the
sequence below are done (the seam with its tests, `framework/headless`'s
registration, and dependencies + readiness with the action primitive and
the two action adapters); steps 4–6 are open. Implements the direction
from the runtime exploration: the browser runtime is composed on the fly
per page and loads its features lazily, and a component's behaviour is
registered by the package that renders its markup, the way its
stylesheet already is.

## The problem

`core-ui/runtime` ships one kernel (`runtime.js`, eight fragments, 13.2 KB
gzip) and 55 demand-loaded modules under `core-ui/runtime/src`. The
kernel is sound and stays as it is. The modules are the problem:

- 34 of them bind markup that only `framework/ui` renders (banner, tabs,
  sidebar, menu, panehost, optimisticaction, themeswitch, and so on); 15
  bind core-ui's own patterns and widgets; six are kernel-side or
  explicit APIs. All 55 live in one directory that belongs to none of
  their owners.
- The kernel finds a module by a hand-written table, `_moduleMarkers` in
  `frag/boot.js`, mirrored by hand in `preload.go`'s `demandLoadMarkers`,
  with a test that keeps the two copies equal. About 450 `ui-*`
  component names are literals inside the runtime.
- A stylesheet has a seam: `registry.RegisterStyle` in the component's
  package, a catalog in `<head>`, one bundle per page, a marker scan
  after hydration. Behaviour has none. Adding a component with behaviour
  means a file in `core-ui/runtime/src`, an entry in the kernel's table,
  an entry in `preload.go`, an entry in `fragments.go`, a row in the
  ARCHITECTURE table, and a budget line.

## Goal

One seam for behaviour, shaped like the one for style:

```go
// framework/headless/behavior.go
//go:embed runtime.js
var runtimeJS string

var Behavior = registry.RegisterBehavior("headless", runtimeJS,
    registry.Markers("[data-hui-reveal]", "[data-hui-when]", "[data-hui-drop]"))
```

The registering package owns the JavaScript, embedded beside the Go
that renders the markup it binds. The host serves it at
`/__gofastr/runtime/<name>.js?v=<hash>` like any module, lists it in the
module manifest the kernel already reads, and tells the kernel its
markers in one more inert JSON block. The kernel scans registered
markers exactly as it scans its own table, loads the module once when a
marker appears, at boot, on DOM insertion, or after a client
navigation, and the module attaches. Nothing about how a module behaves
once loaded changes.

## Non-goals

- No trigger vocabulary. The marker is the one trigger, as today. What
  Angular's incremental hydration contributes is the loading side, a
  chunk fetched on demand whose behaviour attaches when it lands, and the
  kernel already does that on insertion.
- No change to the kernel's role or fragments, and none to the CSS
  pipeline.
- No module moves in the first change. `framework/ui`'s 34 modules stay
  where they are until the seam has a real client; moving them is its own
  change per package, and the kernel's table shrinks as they go.
- No event replay before load. The insertion scan precedes interaction
  in practice; if a measured case shows otherwise it is a separate
  change.

## Design

### Registration (`core-ui/registry`)

```go
func RegisterBehavior(name, js string, opts ...BehaviorOption) *Behavior

func Markers(selectors ...string) BehaviorOption // required: at least one
func LoadIdle() BehaviorOption                   // load after first paint, as `idle: true` modules do

func (b *Behavior) Name() string
func (b *Behavior) Entry() *BehaviorEntry

type BehaviorEntry struct {
    Name    string
    Source  string   // as registered
    Markers []string // attribute selectors
    Idle    bool
}

func Behaviors() []*BehaviorEntry          // sorted by name
func LookupBehavior(name string) (*BehaviorEntry, bool)
```

Rules, each a panic at registration with the reason, so a mistake is a
startup failure and not a dead marker:

- `name` matches `^[a-z][a-z0-9-]{0,63}$` (the bound every module
  name keeps) and is not the name of an embedded
  kernel module (`runtime.ModuleNames()`); the two are one namespace
  because they share one URL and one manifest. A style and a behaviour
  may share a name: a component registers both under its own name.
- Every marker is an attribute selector, `[data-x]` or `[data-x="v"]`,
  on a `data-` attribute, and the value carries no control character,
  backslash, quote or bracket: a string `querySelector` would throw on
  is refused at registration, because one throw in the kernel's scan
  would abort the boot pass for every module. Nothing else scans (no
  `role=` selectors for registered behaviours; the two kernel modules
  that use them predate this seam). A `data-fui-*` marker is permitted
  only when the attribute is already in `core-ui/ARCHITECTURE.md`'s
  table, checked by a gate that reads every `registry.Markers(...)`
  call site in the tree rather than the registry of one test binary,
  which links only what imports it, so hard rule 5 holds through this
  seam as well.
- A duplicate name with identical source and options is a no-op, as
  `RegisterStyle` is; a different definition panics.
- `js` is non-empty and is served as registered, minified under the same
  gate as the embedded modules (`GOFASTR_ENV`, `GOFASTR_DEV`,
  `RUNTIME_NOMINIFY`, `RUNTIME_MINIFY`). The registry stores the source;
  minification and hashing happen where the embedded modules' do, in
  `core-ui/runtime`, so the two kinds of module are one kind from the
  host down.

### The module contract

A registered module is an IIFE with the contract every `src/*.js` module
already keeps, now written down:

1. It binds only to its own markers, by attribute, never by class.
2. It sets `window.__gofastr.loadedModules[<name>] = true` FIRST —
   before it installs anything, right after its own early-return
   guard — so the kernel does not fetch it twice AND a retry cannot
   re-execute a half-failed file into double-installed listeners (a
   script that failed with its flag unset has its load rejected and
   fetched again).
3. It registers `window.__gofastr._moduleScanners[<name>] = fn(root)`,
   idempotent against already-wired elements, so the kernel can hand it
   newly inserted DOM and the document after a client navigation.
4. It reads no `data-fui-*` attribute it does not own, and writes none.
5. It fetches only same-origin, and forwards the CSRF token on unsafe
   methods the way `src/rpc.js` does.

The kernel treats a registered module and an embedded one identically
after the fetch.

### Serving and the manifest (`core-ui/runtime`, `core-ui/widget`, `framework/uihost`)

- `runtime.Module(name)` and `runtime.ModuleNames()` cover registered
  behaviours as well as embedded modules. `Module` returns the minified
  source under the existing gate; the version hash is the same eight
  hex bytes of SHA-256 over the served bytes. Every consumer that
  enumerates modules (the serve route, `RuntimeModuleHash`, the manifest,
  the PWA precache list, the static exporter's dump) therefore covers
  them with no change of its own.
- The manifest block `#gofastr-runtime-modules` keeps its shape (name to
  hash). A second inert block, `#gofastr-behaviors`, carries
  `{ "<name>": { "s": ["[data-x]", ...], "i": true, "r": ["<name>", ...],
  "x": [{ "event": "click", "selector": "[data-x]" }, ...] } }` for
  registered behaviours only. `s` is the markers, `i` the idle flag, `r`
  the required modules, `x` the interactions the kernel's bridge
  retains. Every key but `s` is omitted when the behaviour declares
  nothing for it, so a behaviour that wants none of them pays no bytes.
  Emitted wherever the manifest is emitted: live pages, the static
  export, the embed frame.
- Preload: `runtime.NeededModules(pageHTML)` also matches registered
  markers, so the host's `<link rel="preload" as="script">` covers them.
  A marker `[data-x]` matches as the attribute name `data-x`;
  `[data-x="v"]` as `data-x="v"`, with the same name-boundary rule the
  table uses today.

### The kernel (`frag/boot.js`)

One addition. At boot the kernel reads the descriptors once — the
global a live page sets, or the inline block an export and the embed
frame carry — and both the scan and the interaction bridge iterate the
kernel's own table and this list together:

```js
const _registered = (() => {
  try {
    const o = window.__gofastr_behaviors ||
      JSON.parse((document.getElementById('gofastr-behaviors') || {}).textContent || '{}');
    return Object.entries(o).map(([n, v]) => ({
      name: n,
      selector: v.s.join(','),
      idle: !!v.i,
      requires: v.r || [],
      interactions: (v.x || []).filter((y) => y && y.event),
    }));
  } catch (_) { return []; }
})();
```

`_scanForModules` iterates `_moduleMarkers.concat(_registered)`, and so
does the interaction bridge's install loop: a registered behaviour's
interactions are retained during its own cold-cache fetch and replayed
on the original node exactly as a table module's are. The parse sits
above the bridge because the bridge installs its listeners in the same
boot pass and reads this list there; below it, the `const` is a
temporal dead zone and the page dies.
`loadModule` needs no change: the name resolves through the manifest to
the same URL shape, and the identifier guard already rejects anything
that is not `[\w-]+`. `data-fui-prefetch="<name>"` works for a registered
name for the same reason.

A malformed block (a `window.__gofastr_behaviors` that is not a
descriptor map, an inline block that is not JSON) is caught by the same
`try`: the kernel registers no behaviours and boots on its own table.
The catch is silent on purpose — measured, not skipped: a
`console.warn` costs 17 gzipped bytes at the shortest useful wording
against the 8 bytes of clearance the core line carries, and unlike a
failed fetch the browser console reports nothing on its own, so the
number is written here and in the catch's comment for whoever next
finds bytes to spend.

### Ownership gate (`fragments.go`, `attrdoc_test.go`)

Every `data-fui-*` attribute in the runtime sources has one owner today.
The gate gains one clause: a registered behaviour's markers must be
`data-` attributes, and any `data-fui-*` among them must be in the
documented table. A registered behaviour is not an owner in
`fragments.go`; it owns its own prefix.

### Dependencies and readiness

A behaviour may need another module before it can bind: the action
adapters need the action primitive. The loader is the one place every
load goes through (marker scan, idle queue, hover prefetch, interaction
bridge), so dependencies live there and nowhere else. Teaching the
bridge to read registered descriptors was its own change (2026-09-20,
the interaction-descriptor layer): a behaviour declares the
interactions it needs retained — `Interactions(...)` beside
`Markers(...)`, the bridge's own spec shape (event, selector, and for
a keydown the keys and the scope selector that arms the retention) —
the behaviours block carries them as `x` beside `s`, `i` and `r`, and
the kernel's bridge installs its retention listeners over the
registered descriptors exactly as over its own table. That was the
prerequisite the lightbox move waited on (sequence step 5's ordering
note), because a lightbox's first click or arrow key can land while
its module is still cold-fetching. The lightbox then moved
(2026-09-20, same day): `framework/ui/lightbox.js` is the first
registered module that declares interactions — the prev/next clicks
and the arrow keys over an open viewer — and with it the kernel's
table and bridge literals lost their last interaction entry. The
kernel names no lightbox at all now; `TestRuntimeDemandInteractionBridgeIsGeneric`
fails on one appearing again.

- `registry.Requires(names...)` declares the modules that must be
  loaded before this one. A name is an embedded kernel module or a
  registered behaviour. The manifest carries it as `r`; the inline
  block and `window.__gofastr_behaviors` both do.
- `loadModule(name)` loads a module's requirements first, in parallel,
  and only then appends the module's script. A requirement's own
  requirements load the same way. A cycle is refused at
  `BehaviorsJSON` time with a panic that names it — at the FIRST
  RENDER that builds the manifest, not at startup: the registry is
  only complete once every package's init has run, and the manifest
  builder is the first reader of the whole graph. A requirement that
  is neither embedded nor registered is refused the same way.
- Readiness is registration, not transport. A module says it is ready
  by setting `window.__gofastr.loadedModules[name] = true` — before it
  installs anything, per the module contract: a script that failed
  halfway with its flag unset has its load rejected and its cached
  promise dropped, so a retry re-executes the file and would install
  every listener of the first pass twice. The loader
  resolves the module's promise when that flag is set after the script
  has run; a script that ran and never set its flag rejects with
  "module failed to register" and drops its cached promise, so a
  retry fetches again rather than returning a fulfilled promise for a
  module that is not there.
- Preload follows requirements: `NeededModules` lists a needed
  behaviour's requirements with it (the list is sorted, not ordered;
  `loadModule` orders the loads), so the primitive arrives with its
  dependents. The static exporter includes them for the same
  reason. Hover prefetch (`data-fui-prefetch`) prefetches them too,
  because it goes through `loadModule`.
- A module with no marker of its own (a primitive) is reachable only
  through `Requires` or an explicit `loadModule`. It is still a module:
  served, hashed, budgeted and linted like the rest.

### The action primitive

The optimistic and toggle action modules were one machine written
twice: a same-origin mutation request with the CSRF header and the
invalidation hook, a state flip through idle, pending, committed and
rolled back, `hidden` swapped between two label parts, `aria-busy`
while pending, and an event at each step. Only the attribute
names they read, `aria-pressed` and the group mutex on the toggle, and
the shake on the optimistic one were their own.

`core-ui/runtime/src/action.js` is that machine once, kernel-side, with
no marker and no attribute name of any package:

- `window.__gofastr.action.request(url, method)` performs the
  mutation: origin check, CSRF header (rpc.js's `_csrf`), invalidation
  on success, and resolves to `true` on 2xx and `false` otherwise.
- `window.__gofastr.action.bind(el, spec)` binds the lifecycle to an
  element. `spec` names `endpoint`, `method`, the `idle` and `done`
  parts (elements), and optionally `group` (a mutex key), `untoggle`
  (an endpoint, which makes the element a toggle) and `pressed`
  (whether to mirror `aria-pressed`). It sets `data-state`, swaps
  `hidden`, sets `aria-busy` while pending (never `disabled`: pending
  ignores clicks, a disabled button drops keyboard focus, and
  `disabled` has other owners), and
  dispatches `action:start`, `action:committed`, `action:rolled-back`
  and `action:untoggle` on the element, bubbling. Binding twice is a
  no-op. A `group` converges on one committed member: the revoke runs
  at click time (so a displaced sibling is restored when the new
  member's commit fails) and again on settlement, so two members
  clicked inside one round trip — both past the per-element re-entry
  guard — still end with the last completer committed and the other
  idle. Group members whose elements left the document are pruned on
  `gofastr:navigate`, so the registry is not the last reference to a
  page that navigated away.

Owners bind their own markup to it:

- `framework/ui`'s `optimisticaction` and `toggleaction` become
  registered behaviours in the Go files that render their markup,
  `Requires("action")`, read their `data-fui-optimistic-*` and
  `data-fui-toggle-*` attributes, and call `bind`. They dispatch the
  documented `optimistic-action:*` and `toggle-action:*` events
  alongside the primitive's, because those names are public. The shake
  is a class the optimistic adapter adds on `action:rolled-back`. They
  register a scanner and set their loaded flag, which the originals
  did not.
- `framework/headless` binds `[data-hui-action]` with its own hooks
  (`data-hui-action-endpoint`, `-method`, `-group`, `-untoggle`) and
  drops the borrowed `data-fui-comp` markers, which were also the
  stylesheet's identity. Its failure announcement listens to
  `action:rolled-back`, so a failed toggle speaks.

## What must be tested

Unit, in `core-ui/registry`:

- name rules, marker rules, the `data-fui-*` clause, duplicate
  identical no-op, duplicate different panic, `Behaviors()` order,
  reset for tests as `IsolateForTest` does for styles.

Unit, in `core-ui/runtime` and `core-ui/widget`:

- a registered behaviour appears in `ModuleNames()`, `Module()` returns
  it minified under each gating state, its hash is stable and changes
  with the source, `NeededModules` matches its markers with the boundary
  rule, `serveRuntimeModule` serves it immutable by hash and 404s an
  unknown name, the manifest and the `#gofastr-behaviors` block carry
  it and escape `</` in both.
- the existing gates still pass: byte-identical composition,
  `SYMBOLS.txt`, attrdoc ownership, both demand-load tables equal, the
  budgets at their new lines, and `TestCoreBudgetRejectsCliffOverflow`
  against the padded fixture.

Browser, in `core-ui/runtime` (chromedp against `httptest`):

- a page with the marker fetches the module once and the module attaches
  (a probe attribute the module writes);
- a page without the marker never fetches it;
- a marker inserted later (island swap, widget mount) loads it through
  the insertion scan;
- after a client navigation the module's scanner runs over the new
  document;
- `LoadIdle` defers to idle and still attaches;
- `data-fui-prefetch="<name>"` on hover fetches before any click;
- a failing fetch does not strand the page: the marker element stays,
  `loadedModules[<name>]` stays unset, nothing is thrown; the browser's
  own network error is the signal, as it is for a table module, because
  the scan path swallows the rejection on purpose (a warning would be
  kernel bytes for a case the console already reports).

Browser, in `examples/site`:

- the site registers one real behaviour from a package that is not
  `core-ui/runtime`, and the runtime-split suite's contract holds for it:
  no marker no fetch, marker fetches, manifest is content-addressed,
  mutation observer loads it, SPA navigation rescans it, hover prefetch.
- the static export contains the module file and the behaviours block;
  the static composition ships the same `boot` fragment the live suites
  cover, so the scan itself is not re-proven from an exported page.

Documentation, in the same change: `core-ui/ARCHITECTURE.md` ("Component
CSS" gains "Component behaviour"), the `data-fui-*` table if any marker
touches it, `framework/docs/content/ui-new-components.md` and
`runtime-minification.md`, and `runtime-contract.md`.

Dependencies and the primitive add:

- a registered behaviour with `Requires` loads its requirement first
  and binds only after it (browser test with a delayed requirement);
- a requirement that does not exist, and a cycle, panic at
  `BehaviorsJSON` with the names;
- a module whose script runs and never registers rejects the load and
  a retry fetches again (browser test with a probe that throws);
- `NeededModules` and the static export list a needed behaviour's
  requirements;
- the primitive: a real POST that succeeds commits, a real 422 rolls
  back with the event, a group revokes its sibling, an untoggle
  reverts, binding twice binds once, and an island swap of the button
  rebinds the new element (browser tests against a test server, not
  synthetic events);
- the two framework/ui adapters keep every existing test in
  `framework/ui`, `examples/site` and `core-ui/runtime` green
  unchanged, and gain a rebind-after-swap test each.

## Sequence

1. This seam, with the tests above and no module moved.
2. `framework/headless` registers its `data-hui-*` module: the first
   real client, and the proof the seam carries a whole design system's
   behaviour. (Done 2026-09-15: `framework/headless/behavior.go`.)
3. Dependencies and readiness in the loader, the action primitive, and
   the two action adapters through the seam as the first modules moved:
   the hardest clients first, with their existing tests intact. Then
   `framework/headless` binds its own action hooks and drops the
   borrowed `data-fui-comp` markers. (Order set on 2026-09-16 after an
   outside evaluation found the action modules never re-armed after an
   island swap and the borrowed markers collided with the stylesheet
   identity.)
4. A thin skin and one real mixed screen on the docs site before any
   bulk move: form validation, Password and FileUpload, nested
   conditions, an optimistic action, an island-backed pager.
5. `framework/ui`'s remaining modules in small groups by ownership,
   each a `RegisterBehavior` in the Go file that renders its markup;
   the kernel's table and `preload.go`'s mirror lose the entry; the
   `ui-*` literals leave the runtime with it. The interaction bridge
   reads registered descriptors too before lightbox moves. (Done
   2026-09-20: `registry.Interactions`, the manifest's `x` field, and
   the kernel's merged install loop — this change is what unblocked
   the lightbox move, which landed with it: the lightbox is the first
   of these moves, the module, its viewer anatomy
   (`framework/headless.LightboxViewer`) and its descriptor all owned
   by the component's packages, and the kernel's table holds no
   lightbox entry.)
6. `core-ui/patterns`, the same way. What remains in `core-ui/runtime`
   is the kernel, its fragments, and the kernel-side modules: the
   primitives, the manifest-driven loaders and the widget internals.

## Open questions

- Whether `LoadIdle` is worth its option in the first change, or whether
  every registered behaviour is marker-immediate until one needs idle.
- Whether the static exporter should also dump the behaviours block
  into every page or only into pages whose markers it finds; today it
  dumps every embedded module regardless.
