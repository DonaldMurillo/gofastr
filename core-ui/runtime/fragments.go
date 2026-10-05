package runtime

// This file is the declared attribute→fragment map for runtime composition
// (spec: scratchpad/SPEC-runtime-composer.md). It is step 1 of the split:
// the map and its build-time gate are authored against today's UNPLIT
// runtime, so the classification can be validated before any code moves.
//
// Nothing in this file changes runtime.js or src/*.js. It is pure
// declaration; the gate that enforces it lives in attrdoc_test.go.
//
// Hard rule 5 (CLAUDE.md) already forbids shipping a new data-cui-*
// attribute without updating ARCHITECTURE.md and the runtime test suite.
// This map extends that obligation: a new attribute must also declare an
// owning fragment here, or the build fails.

import "slices"

// fragmentClass labels HOW a fragment enters a composition.
//
// The distinction is the whole safety story for composition (spec §"Two
// classes of behavior"):
//
//   - marker: behavior is triggered by a data-cui-* marker in the DOM. The
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
// sse owns zero data-cui-* attributes: it is triggered by push-target
// markers (any [data-island] region, the offline banner's
// [data-hui-system-offline]) rather than by a data-cui-* attribute; the
// privileged <meta name="gofastr-sse"> is availability, never "open".
// boot-embed is triggered by <meta name="gofastr-embed"> and owns one
// attribute, data-cui-embed-state, which reports the frame's lifecycle.
//
// The action module is the same shape reached the other way: no
// marker, no data-cui-* attribute (the adapters that bind through it
// read their own), loaded because a registered behaviour declared
// Requires("action") and the loader honours the declaration on every
// path. It is also a public API, window.__gofastr.action.
//
// The ws module also owns zero data-cui-* attributes and has no marker
// at all: it is a pure API module an application loads explicitly with
// __gofastr.loadModule('ws') (connectWebSocket /
// createSequencedReducer). Nothing scans the DOM for it. An optional
// API module of that shape outside core registers instead with
// registry.OnRequest (core-ui/localdb), so it never enters this tree.
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
	// (MUTUALLY EXCLUSIVE, never compose both). It owns NO data-cui-*
	// attributes of its own: it cross-references data-cui-open /
	// data-cui-toast / data-cui-deeplink, whose owner stays widgets-boot
	// (the gate treats cross-references as non-transferable, and
	// TestFragmentMapNoDuplicate forbids a second assignment). Same
	// shape as rpc-stub and sse, both absent from fragmentAttrs.
	"widgets-boot-static": {name: "widgets-boot-static", class: bootClass, deps: []string{"kernel"}},
	"sse":                 {name: "sse", class: bootClass, deps: []string{"kernel"}},
	"compute":             {name: "compute", class: markerClass, deps: []string{"kernel"}},
	"boot-embed":          {name: "boot-embed", class: bootClass, deps: []string{"kernel"}},
}

// fragmentAttrs maps each CORE fragment to the data-cui-* attributes whose
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
//     tab-index family. data-cui-tab-index lives here because it is read
//     inside setSignal's attr-mode branch (aria-selected mirroring).
//   - The <a>-click hijack owns the nav markers; data-cui-layout /
//     data-cui-screen-group decide shell-vs-<main> swaps on navigation.
//   - kernel owns the CSS scanner (data-cui-comp / data-cui-scope / data-cui-style), the
//     boot-mode read (data-cui-static), the module-prefetch bridge
//     (data-cui-prefetch), and the module-load-failure safety net
//     (data-cui-toast-fallback, created by window.__gofastr._fallbackToast).
//   - widgets-boot owns the eager open/toast delegators that must exist
//     before the /__gofastr/widgets catalog resolves (data-cui-open,
//     data-cui-toast, data-cui-deeplink). These are boot-class even though
//     they respond to clicks: the LISTENER INSTALLATION is what cannot
//     self-heal, per the spec's class definition.
//
// Attributes whose behavior lives in an on-demand src/*.js module are NOT
// here. See moduleAttrs. Together the two tables assign every data-cui-*
// attribute in the runtime sources to exactly one owner; attrdoc_test.go
// asserts the assignment is complete and drift-free.
var fragmentAttrs = map[string][]string{
	"kernel": {
		"data-cui-os",
		"data-cui-bundle",
		"data-cui-trusted",
		"data-cui-comp",
		"data-cui-scope",
		"data-cui-style",
		"data-cui-static",
		"data-cui-prefetch",
		"data-cui-toast-fallback",
	},
	"signals": {
		"data-cui-signal",
		"data-cui-signal-mode",
		"data-cui-signal-attr",
		"data-cui-signal-set",
		"data-cui-signal-inc",
		"data-cui-signal-toggle",
		"data-cui-flash-on-update",
		"data-cui-flash-duration-ms",
		"data-cui-scroll-bottom-on-update",
		"data-cui-tab-index",
	},
	"nav": {
		"data-cui-spa",
		// data-cui-nav="off" is read on the anchor at click time: nav
		// declines the soft navigation and lets the browser do a full
		// document load.
		"data-cui-nav",
		// data-cui-layout is emit-only since the chain rewrite (CSS/debug
		// contract); nav's swap decisions read the -key/-slot pair.
		"data-cui-layout",
		"data-cui-layout-key",
		"data-cui-layout-slot",
		// data-cui-doc marks a document-lifetime script (uihost's
		// RegisterDocumentScript rail): the live set of these srcs is
		// the document's capability identity, compared against the
		// destination route's manifest docScripts at every soft-nav
		// entry point. A difference is a hard document load.
		"data-cui-doc",
		// data-cui-lang / data-cui-skip-label ride the outermost layer
		// the server renders (App.LangForPath / SkipLabelForPath): the
		// runtime copies them onto documentElement.lang and the skip link
		// after every swap, they live outside the shell it replaces.
		"data-cui-lang",
		"data-cui-skip-label",
		"data-cui-screen-group",
		// The fills-envelope, view-transition and loading-content
		// families moved to their demand modules with the opt-in split
		// (see moduleAttrs: envelope, transition, loading). nav keeps
		// only the layout-chain spine the plain navigator needs.
	},
	"widgets-boot": {
		"data-cui-open",
		"data-cui-toast",
		"data-cui-deeplink",
	},
	"boot-embed": {
		// Set on the embed root as the frame moves through loading → ready
		// (content injected) or → error (no parent, refused handshake, failed
		// content fetch). Read by tests and available to a host page's own
		// styling; nothing in the runtime branches on it.
		"data-cui-embed-state",
	},
	"compute": {
		"data-cui-compute",
	},
}

// moduleAttrs maps each on-demand runtime module (src/<name>.js) to the
// data-cui-* attributes whose behavior it owns.
//
// Every entry is markerClass: the kernel's _scanForModules demand-loads the
// module when it sees the module's primary marker (the scanner table near
// the bottom of runtime.js is the authoritative marker→module map), and
// companion attributes ride along. A module not listed here still loads,
// this is the attribute-ownership map, not the module registry.
//
// Modules that own zero data-cui-* attributes are absent ON PURPOSE:
// compute and sse (their attribute is claimed by the like-named core
// fragment. See fragments note); widgetfocus and
// widgetlinks (triggered by internal JS markers, not data-cui-* at all);
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
	"desktop": {
		// battery/desktop's module. The mousedown delegator that calls
		// startDrag lives in src/desktop.js; the drag itself is native
		// (performWindowDragWithEvent: through the script message
		// channel), which is why the listener is here and not in the
		// widget/dismiss modules.
		"data-cui-window-drag",
	},
	"activelink": {
		// data-cui-activelink hands a link's first-paint mark to the
		// sweep, data-cui-match-prefix adds section matching, and
		// data-cui-activelink-skip opts out of it.
		"data-cui-activelink",
		"data-cui-match-prefix",
		"data-cui-activelink-skip",
	},
	"animate": {
		"data-cui-animate-signal",
		"data-cui-animate-class",
	},
	// carousel is retired: the carousel is headless.Carousel's (bound
	// by the headless-carousel registered module through data-hui-*
	// hooks; the deferred-slide virtual scroll went with its reader).
	// combobox is retired: the combobox anatomy is headless.Combobox's
	// (framework/headless, bound by headless-combobox through
	// data-hui-* hooks). data-cui-static-options went with it.
	"computed": {
		"data-cui-computed",
		"data-cui-computed-deps",
	},
	// disclosure and menu are retired: the disclosure anatomy is
	// headless.Disclosure's (framework/headless, bound by the
	// headless-disclosure and headless-menu modules through
	// data-hui-* hooks).
	"dragdismiss": {
		"data-cui-drag-dismiss",
		"data-cui-drag-handle",
		"data-cui-dragging",
	},
	"dropdown": {
		"data-cui-dropdown-wrap",
		"data-cui-dropdown",
		"data-cui-dropdown-open",
		"data-cui-dropdown-panel",
	},
	"intercept": {
		"data-cui-intercept-overlay",
		"data-cui-intercept-as",
		"data-cui-intercept-close",
	},
	// Lightbox's wiring (data-fui-lightbox*, data-fui-zoomed) moved to
	// framework/ui/lightbox.js, a registered behaviour: its attributes
	// are read by a registered source, not by anything in this package,
	// so they left this table.
	// multiselect is retired: the multiselect is headless.MultiSelect
	// (bound by the headless-multiselect registered module through
	// data-hui-* hooks).
	// OptimisticAction's wiring is the kernel's action primitive
	// (data-hui-action*, bound by the headless module): nothing in
	// this package reads it, so it has no row here. The
	// data-cui-optimistic-* hooks and framework/ui/optimisticaction.js
	// are retired.
	// panehost is retired: the pane host is headless.PaneHost's (bound
	// by the headless-panehost registered module through data-hui-*
	// hooks; the trigger controls are core-ui/interactive's
	// data-hui-pane-open-control/-close/-swap/-key).
	"poll": {
		"data-cui-poll",
		"data-cui-poll-src",
	},
	// The layout demand modules (docs/DESIGN-layout-outlets.md "Opt-in"
	// table): each loads on its marker and owns its family.
	"envelope": {
		// The fills-envelope family the envelope module
		// parses the <template data-cui-fill> envelope, resolves the targets by
		// data-cui-outlet / data-cui-area address, and applies every fill. It
		// also owns the scroll-anchor records (keyed off whatever identity the
		// content carries) and, once loaded, the navigator itself.
		"data-cui-fill",
		"data-cui-outlet",
		"data-cui-area",
	},
	"loading": {
		// The loading-content family the module
		// clones the inert <template data-cui-loading="<addr>"> the server
		// renders beside an outlet, area, or slot cell into the region after
		// data-cui-after ms of in-flight wait, parks the old nodes, restores
		// them on failure, and marks the region data-cui-loadstate="shown"
		// (data-cui-min is the no-flash hold the apply honors).
		"data-cui-loading",
		"data-cui-after",
		"data-cui-min",
		"data-cui-loadstate",
	},
	"transition": {
		// a view-transition name marker the server
		// renders on a placed cell; the transition module mirrors it onto the
		// CSSOM view-transition-name before a navigation's snapshots.
		// data-cui-vt-when gates the name on a media condition.
		// data-cui-vt-kinds is the document's declared keyed-transition
		// vocabulary, on <html> at first paint and on the doc shell every
		// swapped payload's root layer carries; the module copies it onto the
		// documentElement and gates the X-Gofastr-Transition pick against it.
		"data-cui-vt",
		"data-cui-vt-when",
		"data-cui-vt-kinds",
	},
	"popover": {
		"data-cui-popover-anchor",
		"data-cui-popover-side",
		"data-cui-popover-trigger",
	},
	"headless-feedback": {
		"data-cui-toast-stack",
	},
	"rpc": {
		"data-cui-rpc",
		"data-cui-rpc-method",
		"data-cui-rpc-signal",
		"data-cui-rpc-close",
		"data-cui-rpc-reset",
		"data-cui-rpc-body",
		"data-cui-rpc-open",
		"data-cui-rpc-navigate",
		"data-cui-rpc-trigger",
		"data-cui-rpc-after-text",
		"data-cui-rpc-after-done",
		"data-cui-rpc-after-disable",
		"data-cui-rpc-debounce-ms",
		"data-cui-rpc-scroll-to",
		"data-cui-rpc-error-toast",
		"data-cui-confirm",
		"data-cui-push-state",
	},
	"reveal": {
		"data-cui-reveal",
	},
	// scrollspy is retired: the rail is headless.Rail and the observer
	// that marks the active entry is headless-rail's (data-hui-*,
	// owned by that package's modules).
	// shortcut is retired: the chord bindings are headless-navigation's
	// (data-hui-shortcut-*, owned by that registered module's markers).
	// sidebar is retired: the sidebar is bound by the headless-sidebar
	// registered module (framework/headless) through data-hui-* hooks.
	// sortablelist is retired: the sortable list is headless.SortableList
	// (bound by the headless-sortablelist registered module through
	// data-hui-* hooks).
	"textarea": {
		"data-cui-autogrow",
	},
	// toc is retired: the table of contents is headless.TableOfContents
	// (server-rendered items) and its active state is headless-toc's.
	// ToggleAction's wiring is the same action primitive
	// (data-hui-action*): no row here. The data-cui-toggle-* hooks
	// and framework/ui/toggleaction.js are retired.
	// tabs is retired: the tab strip is headless.Tabs's (bound by the
	// headless-tabs registered module through data-hui-* hooks).
	// tree is retired: the tree is headless.Tree (bound by the
	// headless-tree registered module through data-hui-* hooks).
	"widgethelpers": {
		"data-cui-persist-storage",
		"data-cui-charcount-source",
		"data-cui-clear-on-esc",
		"data-cui-submit-on-enter",
		"data-cui-disable-when-invalid",
		"data-cui-fill-input",
		"data-cui-fill-text",
		"data-cui-tick-elapsed",
	},
	"widgets": {
		"data-cui-widget",
		"data-cui-action",
		"data-cui-backdrop",
		"data-cui-rpc-refresh",
		// data-cui-ctx (#321): read by openWidget off the trigger the
		// eager delegator passed along, and used to key the chrome fetch
		// + client cache. widgets-boot cross-references it by passing btn;
		// the behavior lives here, so ownership stays with this module.
		"data-cui-ctx",
	},
}

// ownerKind labels which namespace an attribute's owner lives in.
type ownerKind string

const (
	ownsByFragment ownerKind = "fragment"
	ownsByModule   ownerKind = "module"
)

// attrOwner resolves a data-cui-* attribute to its owning fragment or
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
