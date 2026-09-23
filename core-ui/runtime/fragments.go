package runtime

// This file is the declared attribute→fragment map for runtime composition
// (spec: scratchpad/SPEC-runtime-composer.md). It is step 1 of the split:
// the map and its build-time gate are authored against today's UNPLIT
// runtime, so the classification can be validated before any code moves.
//
// Nothing in this file changes runtime.js or src/*.js. It is pure
// declaration; the gate that enforces it lives in attrdoc_test.go.
//
// Hard rule 5 (CLAUDE.md) already forbids shipping a new data-fui-*
// attribute without updating ARCHITECTURE.md and the runtime test suite.
// This map extends that obligation: a new attribute must also declare an
// owning fragment here, or the build fails.

import "slices"

// fragmentClass labels HOW a fragment enters a composition.
//
// The distinction is the whole safety story for composition (spec §"Two
// classes of behavior"):
//
//   - marker: behavior is triggered by a data-fui-* marker in the DOM. The
//     kernel scanner demand-loads src/*.js modules, including rpc. Core signal
//     behavior is marker-class but remains composed in every bundle.
//
//   - boot: the fragment installs listeners or fetches at boot with no DOM
//     marker to recover from. Omitting it is a deliberate SSR decision
//     (manifest-driven, exactly as `intercept` already is at runtime.js's
//     _scanForModules), not something the runtime can self-heal at request
//     time. nav's <a> hijack, widgets-boot's catalog fetch, sse's
//     bootstrap, and the always-on kernel substrate.
//
// Never make a boot-class behavior marker-driven: that is the silent-failure
// case (a composition that omits the fragment ships a button that does
// nothing). The class is declared per-fragment below and asserted by the
// gate; changing it is a design decision, not an edit.
type fragmentClass string

const (
	markerClass fragmentClass = "marker"
	bootClass   fragmentClass = "boot"
)

// fragmentDef is one compositional unit of the runtime.
type fragmentDef struct {
	name  string
	class fragmentClass
	// deps names the fragments this one is declared to depend on. The
	// composer closes over them (spec §"Fragment set"). kernel has none;
	// every other fragment depends, transitively, on kernel. The gate
	// asserts this graph is acyclic and that every named dep exists.
	deps []string
}

// fragments is the declared fragment set from the runtime-composer spec.
// These are the names the attribute map and the composition table (full /
// static / embed) may use, nothing else.
//
// sse owns zero data-fui-* attributes: it is triggered by the privileged
// <meta name="gofastr-sse"> marker rather than by a DOM attribute. boot-embed
// is triggered by <meta name="gofastr-embed"> and owns one attribute,
// data-fui-embed-state, which reports the frame's lifecycle.
//
// The action module is the same shape reached the other way: no
// marker, no data-fui-* attribute (the adapters that bind through it
// read their own), loaded because a registered behaviour declared
// Requires("action") and the loader honours the declaration on every
// path. It is also a public API, window.__gofastr.action.
//
// The ws module also owns zero data-fui-* attributes and has no marker
// at all: it is a pure API module an application loads explicitly with
// __gofastr.loadModule('ws') (connectWebSocket /
// createSequencedReducer). Nothing scans the DOM for it.
// boot-embed depends on kernel. RPC requests inside an embed route through
// boot's delegation bridge and load src/rpc.js at interaction time. It also
// relies on boot's mutation observer to hydrate injected content, but boot is
// the kernel tail rather than a declared fragment.
//
// sse and compute keep declared fragment names for the composer specification
// although their code is already in demand modules. Their privileged markers
// remain claimed below; rpc has completed the carve and is owned by moduleAttrs.
var fragments = map[string]fragmentDef{
	"kernel":       {name: "kernel", class: bootClass, deps: nil},
	"signals":      {name: "signals", class: markerClass, deps: []string{"kernel"}},
	"nav":          {name: "nav", class: bootClass, deps: []string{"kernel", "signals"}},
	"widgets-boot": {name: "widgets-boot", class: bootClass, deps: []string{"kernel"}},
	// widgets-boot-static is the static-mode counterpart of widgets-boot
	// (MUTUALLY EXCLUSIVE, never compose both). It owns NO data-fui-*
	// attributes of its own: it cross-references data-fui-open /
	// data-fui-toast / data-fui-deeplink, whose owner stays widgets-boot
	// (the gate treats cross-references as non-transferable, and
	// TestFragmentMapNoDuplicate forbids a second assignment). Same
	// shape as rpc-stub and sse, both absent from fragmentAttrs.
	"widgets-boot-static": {name: "widgets-boot-static", class: bootClass, deps: []string{"kernel"}},
	"sse":                 {name: "sse", class: bootClass, deps: []string{"kernel"}},
	"compute":             {name: "compute", class: markerClass, deps: []string{"kernel"}},
	"boot-embed":          {name: "boot-embed", class: bootClass, deps: []string{"kernel"}},
}

// fragmentAttrs maps each CORE fragment to the data-fui-* attributes whose
// runtime behavior it OWNS.
//
// Ownership rule (applied to every attribute, including the ~55 that appear
// in BOTH runtime.js and a src/*.js module): the owner is the fragment (or
// module) whose code implements the attribute's handler, the function that
// runs when the attribute is present. A cross-reference from another
// fragment or module does NOT transfer ownership. Concretely:
//
//   - dispatchRPC owns the rpc-* family (read inside the RPC dispatch path).
//   - setSignal + the click-delegator's signal branch own the signal/flash/
//     tab-index family. data-fui-tab-index lives here because it is read
//     inside setSignal's attr-mode branch (aria-selected mirroring).
//   - The <a>-click hijack owns the nav markers; data-fui-layout /
//     data-fui-screen-group decide shell-vs-<main> swaps on navigation.
//   - kernel owns the CSS scanner (data-fui-comp / data-fui-style), the
//     boot-mode read (data-fui-static), the module-prefetch bridge
//     (data-fui-prefetch), and the module-load-failure safety net
//     (data-fui-toast-fallback, created by window.__gofastr._fallbackToast).
//   - widgets-boot owns the eager open/toast delegators that must exist
//     before the /__gofastr/widgets catalog resolves (data-fui-open,
//     data-fui-toast, data-fui-deeplink). These are boot-class even though
//     they respond to clicks: the LISTENER INSTALLATION is what cannot
//     self-heal, per the spec's class definition.
//
// Attributes whose behavior lives in an on-demand src/*.js module are NOT
// here. See moduleAttrs. Together the two tables assign every data-fui-*
// attribute in the runtime sources to exactly one owner; attrdoc_test.go
// asserts the assignment is complete and drift-free.
var fragmentAttrs = map[string][]string{
	"kernel": {
		"data-fui-os",
		"data-fui-bundle",
		"data-fui-trusted",
		"data-fui-comp",
		"data-fui-style",
		"data-fui-static",
		"data-fui-prefetch",
		"data-fui-toast-fallback",
	},
	"signals": {
		"data-fui-signal",
		"data-fui-signal-mode",
		"data-fui-signal-attr",
		"data-fui-signal-set",
		"data-fui-signal-inc",
		"data-fui-signal-toggle",
		"data-fui-flash-on-update",
		"data-fui-flash-duration-ms",
		"data-fui-scroll-bottom-on-update",
		"data-fui-tab-index",
	},
	"nav": {
		"data-fui-spa",
		// data-fui-nav="off" is read on the anchor at click time: nav
		// declines the soft navigation and lets the browser do a full
		// document load.
		"data-fui-nav",
		// data-fui-layout is emit-only since the chain rewrite (CSS/debug
		// contract); nav's swap decisions read the -key/-slot pair.
		"data-fui-layout",
		"data-fui-layout-key",
		"data-fui-layout-slot",
		// data-fui-doc marks a document-lifetime script (uihost's
		// RegisterDocumentScript rail): the live set of these srcs is
		// the document's capability identity, compared against the
		// destination route's manifest docScripts at every soft-nav
		// entry point. A difference is a hard document load.
		"data-fui-doc",
		// data-fui-lang / data-fui-skip-label ride the outermost layer
		// the server renders (App.LangForPath / SkipLabelForPath): the
		// runtime copies them onto documentElement.lang and the skip link
		// after every swap, they live outside the shell it replaces.
		"data-fui-lang",
		"data-fui-skip-label",
		"data-fui-screen-group",
	},
	"widgets-boot": {
		"data-fui-open",
		"data-fui-toast",
		"data-fui-deeplink",
	},
	"boot-embed": {
		// Set on the embed root as the frame moves through loading → ready
		// (content injected) or → error (no parent, refused handshake, failed
		// content fetch). Read by tests and available to a host page's own
		// styling; nothing in the runtime branches on it.
		"data-fui-embed-state",
	},
	"compute": {
		"data-fui-compute",
	},
}

// moduleAttrs maps each on-demand runtime module (src/<name>.js) to the
// data-fui-* attributes whose behavior it owns.
//
// Every entry is markerClass: the kernel's _scanForModules demand-loads the
// module when it sees the module's primary marker (the scanner table near
// the bottom of runtime.js is the authoritative marker→module map), and
// companion attributes ride along. A module not listed here still loads,
// this is the attribute-ownership map, not the module registry.
//
// Modules that own zero data-fui-* attributes are absent ON PURPOSE:
// compute and sse (their attribute is claimed by the like-named core
// fragment. See fragments note); searchinput (triggered by its
// data-fui-comp CSS marker, which kernel owns, and otherwise driven
// by rpc/signals); widgetfocus and
// widgetlinks (triggered by internal JS markers, not data-fui-* at all);
// preload (manifest-triggered like intercept, boot loads it when any
// route declares a preload mode, and it reads route data, not markers);
// actionloader (triggered by the __gofastr_actions manifest global and
// keyed on data-component/data-widget, which widgets owns).
//
// Ownership for an attribute referenced by several module files is resolved
// to the module that implements the behavior, determined by the scanner
// table's primary marker, then by the single module that references a
// companion attribute. The one non-obvious case: the seven general-purpose
// form helpers (charcount-source, clear-on-esc, disable-when-invalid,
// fill-input, persist-storage, submit-on-enter, tick-elapsed) are owned by
// `widgethelpers`, which is a genuine independently-loadable module (it
// self-registers a scanner and widgets.js demand-loads it), NOT by widgets.
var moduleAttrs = map[string][]string{
	"activelink": {
		// Carved out of the nav fragment (level-1 budget): the idle-loaded
		// module owns prefix-matched aria-current highlighting and the
		// data-fui-activelink-skip opt-out from it.
		"data-fui-match-prefix",
		"data-fui-activelink-skip",
	},
	"animate": {
		"data-fui-animate-signal",
		"data-fui-animate-class",
	},
	// carousel is retired: the carousel is headless.Carousel's (bound
	// by the headless-carousel registered module through data-hui-*
	// hooks; the deferred-slide virtual scroll went with its reader).
	// combobox is retired: the combobox anatomy is headless.Combobox's
	// (framework/headless, bound by headless-combobox through
	// data-hui-* hooks). data-fui-static-options went with it.
	"computed": {
		"data-fui-computed",
		"data-fui-computed-deps",
	},
	// disclosure and menu are retired: the disclosure anatomy is
	// headless.Disclosure's (framework/headless, bound by the
	// headless-disclosure and headless-menu modules through
	// data-hui-* hooks).
	"dragdismiss": {
		"data-fui-drag-dismiss",
		"data-fui-drag-handle",
		"data-fui-dragging",
	},
	"dropdown": {
		"data-fui-dropdown-wrap",
		"data-fui-dropdown",
		"data-fui-dropdown-open",
		"data-fui-dropdown-panel",
	},
	"infinitescroll": {
		"data-fui-infinite-scroll",
		"data-fui-infinite-sentinel",
		"data-fui-infinite-cursor",
		"data-fui-infinite-items",
		"data-fui-infinite-root-margin",
	},
	"intercept": {
		"data-fui-intercept-overlay",
		"data-fui-intercept-as",
		"data-fui-intercept-close",
	},
	// Lightbox's wiring (data-fui-lightbox*, data-fui-zoomed) moved to
	// framework/ui/lightbox.js, a registered behaviour: its attributes
	// are read by a registered source, not by anything in this package,
	// so they left this table.
	"multiselect": {
		"data-fui-multiselect",
		"data-fui-multiselect-chips",
		"data-fui-multiselect-remove",
	},
	// OptimisticAction's wiring is the kernel's action primitive
	// (data-hui-action*, bound by the headless module): nothing in
	// this package reads it, so it has no row here. The
	// data-fui-optimistic-* hooks and framework/ui/optimisticaction.js
	// are retired.
	// panehost is retired: the pane host is headless.PaneHost's (bound
	// by the headless-panehost registered module through data-hui-*
	// hooks; the trigger controls are core-ui/interactive's
	// data-hui-pane-open-control/-close/-swap/-key).
	"poll": {
		"data-fui-poll",
		"data-fui-poll-src",
	},
	"popover": {
		"data-fui-popover-anchor",
		"data-fui-popover-side",
		"data-fui-popover-trigger",
	},
	"headless-feedback": {
		"data-fui-toast-stack",
	},
	"rpc": {
		"data-fui-rpc",
		"data-fui-rpc-method",
		"data-fui-rpc-signal",
		"data-fui-rpc-close",
		"data-fui-rpc-reset",
		"data-fui-rpc-body",
		"data-fui-rpc-open",
		"data-fui-rpc-navigate",
		"data-fui-rpc-trigger",
		"data-fui-rpc-after-text",
		"data-fui-rpc-after-done",
		"data-fui-rpc-after-disable",
		"data-fui-rpc-debounce-ms",
		"data-fui-rpc-scroll-to",
		"data-fui-confirm",
		"data-fui-push-state",
	},
	"reveal": {
		"data-fui-reveal",
	},
	// scrollspy is retired: the rail is headless.Rail and the observer
	// that marks the active entry is headless-rail's (data-hui-*,
	// owned by that package's modules).
	// shortcut is retired: the chord bindings are headless-navigation's
	// (data-hui-shortcut-*, owned by that registered module's markers).
	// sidebar is retired: the sidebar is bound by the headless-sidebar
	// registered module (framework/headless) through data-hui-* hooks.
	"sortablelist": {
		"data-fui-sortable",
		"data-fui-sort-key",
		"data-fui-sortable-item",
		"data-fui-sortable-rpc",
		"data-fui-sortable-group",
		"data-fui-sortable-container",
		"data-fui-sortable-version",
		"data-fui-sortable-conflict",
	},
	"textarea": {
		"data-fui-autogrow",
	},
	// toc is retired: the table of contents is headless.TableOfContents
	// (server-rendered items) and its active state is headless-toc's.
	// ToggleAction's wiring is the same action primitive
	// (data-hui-action*): no row here. The data-fui-toggle-* hooks
	// and framework/ui/toggleaction.js are retired.
	// tabs is retired: the tab strip is headless.Tabs's (bound by the
	// headless-tabs registered module through data-hui-* hooks).
	"tree": {
		"data-fui-tree-toggle",
	},
	"widgethelpers": {
		"data-fui-persist-storage",
		"data-fui-charcount-source",
		"data-fui-clear-on-esc",
		"data-fui-submit-on-enter",
		"data-fui-disable-when-invalid",
		"data-fui-fill-input",
		"data-fui-fill-text",
		"data-fui-tick-elapsed",
	},
	"widgets": {
		"data-fui-widget",
		"data-fui-action",
		"data-fui-backdrop",
		"data-fui-rpc-refresh",
		// data-fui-ctx (#321): read by openWidget off the trigger the
		// eager delegator passed along, and used to key the chrome fetch
		// + client cache. widgets-boot cross-references it by passing btn;
		// the behavior lives here, so ownership stays with this module.
		"data-fui-ctx",
	},
}

// ownerKind labels which namespace an attribute's owner lives in.
type ownerKind string

const (
	ownsByFragment ownerKind = "fragment"
	ownsByModule   ownerKind = "module"
)

// attrOwner resolves a data-fui-* attribute to its owning fragment or
// module. Returns ("", "") for an unassigned attribute; the gate test
// asserts that never happens for any attribute in the runtime sources.
//
// For a module-owned attribute the returned name is the bare module name
// (e.g. "poll"), not a prefixed token, callers that need to distinguish
// the namespace use the kind.
func attrOwner(attr string) (kind ownerKind, name string) {
	for frag, attrs := range fragmentAttrs {
		if slices.Contains(attrs, attr) {
			return ownsByFragment, frag
		}
	}
	for mod, attrs := range moduleAttrs {
		if slices.Contains(attrs, attr) {
			return ownsByModule, mod
		}
	}
	return "", ""
}
