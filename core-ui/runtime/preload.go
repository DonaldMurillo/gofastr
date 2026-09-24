package runtime

import (
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

// demandLoadMarker is one entry in the server-side mirror of the
// runtime.js demand-load scanner table. The framework scans the
// rendered page HTML for the literal marker substring and emits
// <link rel="preload" as="script"> for the named module when matched, so
// the browser parallel-fetches modules during the initial render.
type demandLoadMarker struct {
	Marker string
	Module string
}

// demandLoadMarkers is the canonical Go-side mirror of the table at
// the bottom of core-ui/runtime/runtime.js (search for "DEMAND-LOAD
// SCANNERS"). The drift test enforces both sides stay aligned.
var demandLoadMarkers = []demandLoadMarker{
	{"data-fui-rpc", "rpc"},
	{"data-kiln-tool", "rpc"},
	{"data-fui-computed", "computed"},
	{"data-fui-compute", "compute"},
	{"data-fui-popover-anchor", "popover"},
	{`name="gofastr-sse"`, "sse"},
	{"data-fui-widget", "widgets"},
	{"data-fui-open", "widgets"},
	{"data-fui-autogrow", "textarea"},
	{`data-fui-comp="ui-search-input"`, "searchinput"},
	{"data-fui-dropdown-wrap", "dropdown"},
	{"data-fui-reveal", "reveal"},
	{"data-fui-animate-signal", "animate"},
	{"data-fui-drag-dismiss", "dragdismiss"},
	{"data-fui-poll", "poll"},
}

// NeededModules returns the deduplicated, sorted list of demand-load
// runtime modules whose marker substring appears in pageHTML, plus
// every such module's requirements, transitively: a needed behaviour
// arrives with the primitive it binds through. Used by the framework's
// UI host to emit <link rel="preload" as="script"> tags in <head> per
// page, kicking off module fetches in parallel with the initial paint.
//
// Matches are substring containment with an attribute-name boundary
// check, not a real HTML parse. The boundary check keeps one marker
// from matching inside a longer attribute name (data-fui-compute must
// not fire on data-fui-computed). The cost of a residual false positive
// is one wasted module fetch (no correctness impact). The list is
// sorted, not dependency-ordered: a preload link only warms a cache,
// and loadModule in the kernel is what orders the loads, requirements
// before dependents, when the marker actually appears.
func NeededModules(pageHTML string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(demandLoadMarkers))
	// add walks a module's requirements before the module itself, so
	// a behaviour's primitive is always in the list beside it. Only a
	// registered behaviour carries requirements; an embedded module
	// has nowhere to declare one, and LookupBehavior says no.
	var add func(name string)
	add = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		if e, ok := registry.LookupBehavior(name); ok {
			for _, r := range e.Requires {
				add(r)
			}
		}
		out = append(out, name)
	}
	for _, m := range demandLoadMarkers {
		if markerPresent(pageHTML, m.Marker) {
			add(m.Module)
		}
	}
	// Registered behaviours preload by the same rule, from the markers
	// they declared rather than from the table.
	for _, name := range neededBehaviors(pageHTML) {
		add(name)
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
