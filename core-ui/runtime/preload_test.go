package runtime

import (
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestNeededModules_EmptyPage(t *testing.T) {
	if got := NeededModules(""); len(got) != 0 {
		t.Errorf("empty HTML should yield no modules, got %v", got)
	}
	if got := NeededModules("<html><body><h1>hi</h1></body></html>"); len(got) != 0 {
		t.Errorf("marker-free HTML should yield no modules, got %v", got)
	}
}

func TestNeededModules_SingleMarker(t *testing.T) {
	html := `<button data-fui-popover-anchor="bottom">Help</button>`
	got := NeededModules(html)
	want := []string{"popover"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NeededModules(%q) = %v, want %v", html, got, want)
	}
}

func TestNeededModules_SidebarCollapse(t *testing.T) {
	html := `<button data-fui-sidebar-collapse>Collapse</button>`
	got := NeededModules(html)
	want := []string{"sidebar"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NeededModules(%q) = %v, want %v", html, got, want)
	}
}

func TestNeededModules_MultipleMarkersDedupSorted(t *testing.T) {
	// popover, widgets (twice), toasts, lightbox
	html := `
		<button data-fui-open="m1">open</button>
		<div data-fui-widget="m1"></div>
		<button data-fui-toast='{"title":"hi"}'>toast</button>
		<button data-fui-popover-anchor="auto">pop</button>
		<div data-fui-comp="ui-lightbox" data-fui-lightbox="lb"></div>
	`
	got := NeededModules(html)
	want := []string{"lightbox", "popover", "toasts", "widgets"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestDemandLoadModuleNamesMatchEmbeddedModules enforces that every
// module declared in the Go-side demand-load table has a corresponding
// src/<name>.js file embedded. Catches typos and rename drift.
func TestDemandLoadModuleNamesMatchEmbeddedModules(t *testing.T) {
	embedded := map[string]bool{}
	for _, n := range ModuleNames() {
		embedded[n] = true
	}
	for _, name := range DemandLoadModuleNames() {
		if !embedded[name] {
			t.Errorf("demand-load table references module %q but core-ui/runtime/src/%s.js doesn't exist", name, name)
		}
	}
}

// TestDemandLoadMarkersMatchRuntimeJS enforces the Go-side and
// JS-side demand-load tables stay aligned. The runtime.js file is the
// source of truth for the runtime behaviour; the Go table mirrors it
// for server-side preload emission. Drift here would mean some
// modules get preloaded but never executed (or vice-versa).
func TestDemandLoadMarkersMatchRuntimeJS(t *testing.T) {
	src, err := os.ReadFile("runtime.js")
	if err != nil {
		t.Skipf("can't read runtime.js: %v", err)
	}
	// Extract module names from the `{ name: 'foo', selector: '...' }`
	// entries inside the DEMAND-LOAD SCANNERS block.
	idx := strings.Index(string(src), "DEMAND-LOAD SCANNERS")
	if idx < 0 {
		t.Fatal("could not locate DEMAND-LOAD SCANNERS section in runtime.js")
	}
	block := string(src)[idx:]
	endIdx := strings.Index(block, "for (const m of")
	if endIdx > 0 {
		block = block[:endIdx]
	}
	re := regexp.MustCompile(`\{\s*name:\s*'([a-z0-9_-]+)'`)
	matches := re.FindAllStringSubmatch(block, -1)
	jsModules := map[string]bool{}
	for _, m := range matches {
		if len(m) >= 2 {
			jsModules[m[1]] = true
		}
	}
	if len(jsModules) == 0 {
		t.Fatal("no module names extracted from runtime.js demand-load block")
	}
	goModules := map[string]bool{}
	for _, n := range DemandLoadModuleNames() {
		goModules[n] = true
	}
	for n := range jsModules {
		if !goModules[n] {
			t.Errorf("runtime.js demand-loads %q but the Go table doesn't — preload won't fire for pages that use it", n)
		}
	}
	for n := range goModules {
		if !jsModules[n] {
			t.Errorf("Go demand-load table lists %q but runtime.js doesn't load it on demand — preload may waste bandwidth", n)
		}
	}
}

func TestNeededModules_StableSort(t *testing.T) {
	// Same input → same output ordering, regardless of map iteration.
	html := `<div data-fui-carousel><div data-fui-toast></div><div data-fui-widget></div></div>`
	for i := 0; i < 50; i++ {
		got := NeededModules(html)
		if !sort.StringsAreSorted(got) {
			t.Fatalf("NeededModules returned unsorted slice: %v", got)
		}
	}
}

func TestComputedDoesNotPreloadCompute(t *testing.T) {
	got := NeededModules(`<div data-fui-computed="a+b"></div>`)
	for _, m := range got {
		if m == "compute" {
			t.Fatalf("computed-only page preloaded compute: %v", got)
		}
	}
}
