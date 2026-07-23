package runtime

import (
	"sort"
	"strings"
)

// demandLoadMarker is one entry in the server-side mirror of the
// runtime.js demand-load scanner table. The framework scans the
// rendered page HTML for the literal marker substring and emits
// <link rel="modulepreload"> for the named module when matched, so
// the browser parallel-fetches modules during the initial render.
type demandLoadMarker struct {
	Marker string
	Module string
}

// demandLoadMarkers is the canonical Go-side mirror of the table at
// the bottom of core-ui/runtime/runtime.js (search for "DEMAND-LOAD
// SCANNERS"). The drift test enforces both sides stay aligned.
var demandLoadMarkers = []demandLoadMarker{
	{"data-fui-copy-text-from", "copy"},
	{"data-fui-computed", "computed"},
	{"data-fui-compute", "compute"},
	{"data-fui-fileupload", "fileupload"},
	{"data-fui-popover-anchor", "popover"},
	{"data-fui-menu", "menu"},
	{"data-fui-toast-stack", "toasts"},
	{"data-fui-toast", "toasts"},
	{`name="gofastr-sse"`, "sse"},
	{"data-fui-widget", "widgets"},
	{"data-fui-open", "widgets"},
	{`role="combobox"`, "combobox"},
	{`role="tree"`, "tree"},
	{"data-fui-infinite-scroll", "infinitescroll"},
	{"data-fui-banner-dismiss", "banner"},
	{"data-fui-slider-mirror", "slider"},
	{"data-fui-number-step", "numberinput"},
	{"data-fui-autogrow", "textarea"},
	{"data-fui-multiselect-chips", "multiselect"},
	{`data-fui-comp="ui-dropzone"`, "dropzone"},
	{"data-fui-range-slider", "rangeslider"},
	{"data-fui-tag-input", "taginput"},
	{"data-fui-animated-counter", "animatedcounter"},
	{"data-fui-toc", "toc"},
	{"data-fui-scrollspy", "scrollspy"},
	{`data-fui-comp="ui-optimistic-action"`, "optimisticaction"},
	{`data-fui-comp="ui-toggle-action"`, "toggleaction"},
	{`data-fui-comp="ui-network-retry-banner"`, "networkretrybanner"},
	{"data-fui-sortable", "sortablelist"},
	{"data-fui-shortcut-focus", "shortcut"},
	{"data-fui-shortcut-click", "shortcut"},
	{`data-fui-comp="ui-lightbox"`, "lightbox"},
	{"data-fui-carousel", "carousel"},
	{"data-fui-theme-toggle", "themeswitch"},
	{"data-fui-sidebar-collapse", "sidebar"},
	{"data-fui-back-to-top", "backtotop"},
	{`data-fui-comp="ui-conditional-field"`, "conditionalfield"},
	{`data-fui-comp="ui-password-input"`, "passwordinput"},
	{`data-fui-comp="ui-search-input"`, "searchinput"},
	{`data-fui-comp="ui-form-repeater"`, "formrepeater"},
	{"data-fui-dropdown-wrap", "dropdown"},
	{"data-fui-reveal", "reveal"},
	{"data-fui-animate-signal", "animate"},
	{"data-fui-drag-dismiss", "dragdismiss"},
	{"data-fui-pane-host", "panehost"},
	{"data-fui-poll", "poll"},
}

// NeededModules returns the deduplicated, sorted list of demand-load
// runtime modules whose marker substring appears in pageHTML. Used
// by the framework's UI host to emit <link rel="modulepreload"> tags
// in <head> per page, kicking off module fetches in parallel with the
// initial paint.
//
// Matches are substring containment with an attribute-name boundary
// check — not a real HTML parse. The boundary check keeps one marker
// from matching inside a longer attribute name (data-fui-compute must
// not fire on data-fui-computed). The cost of a residual false positive
// is one wasted module fetch (no correctness impact).
func NeededModules(pageHTML string) []string {
	seen := map[string]bool{}
	for _, m := range demandLoadMarkers {
		if seen[m.Module] {
			continue
		}
		if markerPresent(pageHTML, m.Marker) {
			seen[m.Module] = true
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// markerPresent reports whether marker occurs in pageHTML as a complete
// attribute name: the byte after the match must end an attribute name
// ('=', '>', '/', a quote, or whitespace) or be the end of input, so a
// marker never fires as a prefix of a longer data-fui-* attribute.
func markerPresent(pageHTML, marker string) bool {
	for start := 0; ; {
		i := strings.Index(pageHTML[start:], marker)
		if i < 0 {
			return false
		}
		end := start + i + len(marker)
		if end >= len(pageHTML) {
			return true
		}
		switch pageHTML[end] {
		case '=', '>', '/', '"', '\'', ' ', '\t', '\n', '\r':
			return true
		}
		start = end
	}
}

// DemandLoadModuleNames returns the unique sorted list of every module
// referenced by the demand-load table. Used by tests to verify every
// declared module actually has a corresponding src/<name>.js file.
func DemandLoadModuleNames() []string {
	seen := map[string]bool{}
	for _, m := range demandLoadMarkers {
		seen[m.Module] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
