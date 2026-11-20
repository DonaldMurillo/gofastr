package runtime

import "slices"

type fragmentClass string

const (
	markerClass fragmentClass = "marker"
	bootClass   fragmentClass = "boot"
)

type fragmentDef struct {
	name  string
	class fragmentClass
	deps  []string
}

var fragments = map[string]fragmentDef{
	"kernel":              {name: "kernel", class: bootClass, deps: nil},
	"signals":             {name: "signals", class: markerClass, deps: []string{"kernel"}},
	"nav":                 {name: "nav", class: bootClass, deps: []string{"kernel", "signals"}},
	"widgets-boot":        {name: "widgets-boot", class: bootClass, deps: []string{"kernel"}},
	"widgets-boot-static": {name: "widgets-boot-static", class: bootClass, deps: []string{"kernel"}},
	"sse":                 {name: "sse", class: bootClass, deps: []string{"kernel"}},
	"compute":             {name: "compute", class: markerClass, deps: []string{"kernel"}},
	"boot-embed":          {name: "boot-embed", class: bootClass, deps: []string{"kernel"}},
}

var fragmentAttrs = map[string][]string{
	"kernel": {
		"data-cui-os",
		"data-cui-bundle",
		"data-cui-bundle-href",
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
		"data-cui-nav",
		"data-cui-layout",
		"data-cui-layout-key",
		"data-cui-layout-slot",
		"data-cui-doc",
		"data-cui-lang",
		"data-cui-skip-label",
		"data-cui-page-head",
		"data-cui-strings",
		"data-cui-page-meta",
		"data-cui-screen-group",
	},
	"widgets-boot": {
		"data-cui-open",
		"data-cui-toast",
		"data-cui-deeplink",
	},
	"boot-embed": {
		"data-cui-embed-state",
	},
	"compute": {
		"data-cui-compute",
	},
}

var moduleAttrs = map[string][]string{
	"desktop": {
		"data-cui-window-drag",
		"data-cui-window-no-drag",
	},
	"activelink": {
		"data-cui-activelink",
		"data-cui-match-prefix",
		"data-cui-activelink-skip",
	},
	"animate": {
		"data-cui-animate-signal",
		"data-cui-animate-class",
	},
	"computed": {
		"data-cui-computed",
		"data-cui-computed-deps",
	},
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
		"data-cui-intercept-page",
		"data-cui-intercept-swap",
		"data-cui-intercept-panel",
	},
	"poll": {
		"data-cui-poll",
		"data-cui-poll-src",
	},
	"envelope": {
		"data-cui-fill",
		"data-cui-outlet",
		"data-cui-area",
	},
	"loading": {
		"data-cui-loading",
		"data-cui-after",
		"data-cui-min",
		"data-cui-loadstate",
	},
	"transition": {
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
	"confirm": {
		"data-cui-confirm-title",
		"data-cui-confirm-accept",
		"data-cui-confirm-tone",
		"data-cui-confirm-dialog",
		"data-cui-confirm-part",
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
		"data-cui-rpc-success-toast",
		"data-cui-rpc-success-action",
		"data-cui-confirm",
		"data-cui-push-state",
	},
	"reveal": {
		"data-cui-reveal",
	},
	"textarea": {
		"data-cui-autogrow",
	},
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
		"data-cui-ctx",
	},
}

type ownerKind string

const (
	ownsByFragment ownerKind = "fragment"
	ownsByModule   ownerKind = "module"
)

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
