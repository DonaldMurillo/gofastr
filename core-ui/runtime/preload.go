package runtime

import (
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
)

type demandLoadMarker struct {
	Marker string
	Module string
}

var demandLoadMarkers = []demandLoadMarker{
	{"data-cui-rpc", "rpc"},
	{"data-kiln-tool", "rpc"},
	{"data-cui-fill-input", "widgethelpers"},
	{"data-cui-tick-elapsed", "widgethelpers"},
	{"data-cui-persist-storage", "widgethelpers"},
	{"data-cui-charcount-source", "widgethelpers"},
	{"data-cui-clear-on-esc", "widgethelpers"},
	{"data-cui-submit-on-enter", "widgethelpers"},
	{"data-cui-disable-when-invalid", "widgethelpers"},
	{"data-cui-computed", "computed"},
	{"data-cui-compute", "compute"},
	{"data-cui-popover-anchor", "popover"},
	{"data-island", "sse"},
	{"data-hui-system-offline", "sse"},
	{"data-cui-widget", "widgets"},
	{"data-cui-open", "widgets"},
	{"data-cui-autogrow", "textarea"},
	{"data-cui-dropdown-wrap", "dropdown"},
	{"data-cui-reveal", "reveal"},
	{"data-cui-animate-signal", "animate"},
	{"data-cui-drag-dismiss", "dragdismiss"},
	{"data-cui-poll", "poll"},
	{"data-cui-vt", "transition"},
	{"data-cui-vt-kinds", "transition"},
}

// NeededModules returns the sorted demand modules whose markers appear in pageHTML, with their requirements.
func NeededModules(pageHTML string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(demandLoadMarkers))
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
	for _, name := range neededBehaviors(pageHTML) {
		add(name)
	}
	sort.Strings(out)
	return out
}

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

// DemandLoadModuleNames returns the sorted modules the demand-load table names.
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
