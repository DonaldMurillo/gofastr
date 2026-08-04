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
// static / embed) may use — nothing else.
//
// sse owns zero data-fui-* attributes: it is triggered by the privileged
// <meta name="gofastr-sse"> marker rather than by a DOM attribute. boot-embed
// is triggered by <meta name="gofastr-embed"> and owns one attribute,
// data-fui-embed-state, which reports the frame's lifecycle.
//
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
	// (MUTUALLY EXCLUSIVE — never compose both). It owns NO data-fui-*
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
// module) whose code implements the attribute's handler — the function that
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
// here — see moduleAttrs. Together the two tables assign every data-fui-*
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
		// data-fui-layout is emit-only since the chain rewrite (CSS/debug
		// contract); nav's swap decisions read the -key/-slot pair.
		"data-fui-layout",
		"data-fui-layout-key",
		"data-fui-layout-slot",
		"data-fui-screen-group",
		"data-fui-match-prefix",
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
// companion attributes ride along. A module not listed here still loads —
// this is the attribute-ownership map, not the module registry.
//
// Modules that own zero data-fui-* attributes are absent ON PURPOSE:
// compute and sse (their attribute is claimed by the like-named core
// fragment — see fragments note); formrepeater, passwordinput, and
// searchinput (triggered by data-fui-comp="ui-<name>" CSS markers, which
// kernel owns, and otherwise driven by rpc/signals); widgetfocus and
// widgetlinks (triggered by internal JS markers, not data-fui-* at all).
//
// Ownership for an attribute referenced by several module files is resolved
// to the module that implements the behavior — determined by the scanner
// table's primary marker, then by the single module that references a
// companion attribute. The one non-obvious case: the seven general-purpose
// form helpers (charcount-source, clear-on-esc, disable-when-invalid,
// fill-input, persist-storage, submit-on-enter, tick-elapsed) are owned by
// `widgethelpers`, which is a genuine independently-loadable module (it
// self-registers a scanner and widgets.js demand-loads it), NOT by widgets.
var moduleAttrs = map[string][]string{
	"animate": {
		"data-fui-animate-signal",
		"data-fui-animate-class",
	},
	"animatedcounter": {
		"data-fui-animated-counter",
		"data-fui-animated-counter-from",
		"data-fui-animated-counter-ms",
	},
	"backtotop": {
		"data-fui-back-to-top",
		"data-fui-btt-scroll",
		"data-fui-btt-target",
		"data-fui-btt-threshold",
		"data-fui-btt-visible",
	},
	"banner": {
		"data-fui-banner-dismiss",
		"data-fui-banner-dismiss-id",
	},
	"carousel": {
		"data-fui-carousel",
		"data-fui-carousel-autorotate",
		"data-fui-carousel-defer",
		"data-fui-carousel-deferred-for",
		"data-fui-carousel-dot",
		"data-fui-carousel-loop",
		"data-fui-carousel-next",
		"data-fui-carousel-prev",
		"data-fui-carousel-slide",
		"data-fui-carousel-track",
	},
	"combobox": {
		"data-fui-static-options",
	},
	"computed": {
		"data-fui-computed",
		"data-fui-computed-deps",
	},
	"conditionalfield": {
		"data-fui-cond-disabled",
	},
	"copy": {
		"data-fui-copy-text-from",
		"data-fui-copy-announce",
		"data-fui-copy-status",
		"data-fui-copy-toast",
	},
	"disclosure": {
		"data-fui-disclosure",
		"data-fui-disclosure-trap",
		"data-fui-disclosure-persist",
	},
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
	"dropzone": {
		"data-fui-dropzone-preview",
		"data-fui-dropzone-preview-for",
	},
	"fileupload": {
		"data-fui-fileupload",
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
	"lightbox": {
		"data-fui-lightbox",
		"data-fui-lightbox-group",
		"data-fui-lightbox-nav",
		"data-fui-lightbox-next",
		"data-fui-lightbox-prev",
		"data-fui-zoomed",
	},
	"menu": {
		"data-fui-menu",
	},
	"multiselect": {
		"data-fui-multiselect",
		"data-fui-multiselect-chips",
		"data-fui-multiselect-remove",
	},
	"networkretrybanner": {
		"data-fui-network-retry-threshold",
		"data-fui-network-retry-health",
		"data-fui-network-retry-button",
		"data-fui-network-retry-sse-silence",
		"data-fui-network-retry-demo-trigger",
		"data-fui-network-retry-demo-recover",
	},
	"numberinput": {
		"data-fui-number-step",
		"data-fui-number-for",
	},
	"optimisticaction": {
		"data-fui-optimistic-idle",
		"data-fui-optimistic-success",
		"data-fui-optimistic-endpoint",
		"data-fui-optimistic-method",
	},
	"panehost": {
		"data-fui-pane-host",
		"data-fui-pane",
		"data-fui-pane-open",
		"data-fui-pane-close",
		"data-fui-pane-swap",
		"data-fui-pane-host-target",
		"data-fui-pane-mode",
		"data-fui-pane-deeplink",
		"data-fui-pane-key",
	},
	"poll": {
		"data-fui-poll",
		"data-fui-poll-src",
	},
	"popover": {
		"data-fui-popover-anchor",
		"data-fui-popover-side",
		"data-fui-popover-trigger",
	},
	"rangeslider": {
		"data-fui-range-slider",
		"data-fui-range-slider-value",
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
	"scrollspy": {
		"data-fui-scrollspy",
		"data-fui-scrollspy-target",
	},
	"shortcut": {
		"data-fui-shortcut-focus",
		"data-fui-shortcut-click",
		"data-fui-shortcut-target",
	},
	"sidebar": {
		"data-fui-sidebar-collapse",
		"data-fui-sidebar",
		"data-fui-sidebar-storage",
	},
	"slider": {
		"data-fui-slider-mirror",
	},
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
	"taginput": {
		"data-fui-tag-input",
		"data-fui-tag-input-id",
		"data-fui-tag-input-zone",
	},
	"textarea": {
		"data-fui-autogrow",
	},
	"themeswitch": {
		"data-fui-theme-toggle",
		"data-fui-theme-toggle-opt",
	},
	"toasts": {
		"data-fui-toast-stack",
		"data-fui-toast-id",
		"data-fui-toast-dismiss",
		"data-fui-toast-ttl-ms",
	},
	"toc": {
		"data-fui-toc",
		"data-fui-toc-levels",
		"data-fui-toc-for",
	},
	"toggleaction": {
		"data-fui-toggle-endpoint",
		"data-fui-toggle-method",
		"data-fui-toggle-allow-untoggle",
		"data-fui-toggle-untoggle-endpoint",
		"data-fui-toggle-idle",
		"data-fui-toggle-committed",
		"data-fui-toggle-group",
	},
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
// (e.g. "poll"), not a prefixed token — callers that need to distinguish
// the namespace use the kind.
func attrOwner(attr string) (kind ownerKind, name string) {
	for frag, attrs := range fragmentAttrs {
		for _, a := range attrs {
			if a == attr {
				return ownsByFragment, frag
			}
		}
	}
	for mod, attrs := range moduleAttrs {
		for _, a := range attrs {
			if a == attr {
				return ownsByModule, mod
			}
		}
	}
	return "", ""
}
