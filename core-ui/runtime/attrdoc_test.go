package runtime

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// attrPattern matches a full data-fui-* attribute token. It is greedy on
// the name body, so a comment that wraps mid-name (`data-fui-foo-` at the
// end of a line) yields a trailing-dash token; such artifacts are dropped
// below. Real attributes never end in a dash.
var attrPattern = regexp.MustCompile(`data-fui-[a-z0-9-]+`)

// runtimeJSAttrs returns every data-fui-* attribute literally referenced in
// the bundled runtime.js and every on-demand src/*.js module. Comments are
// intentionally included: several attributes (e.g. data-fui-rpc-after-done)
// are read through the camelCase `dataset` API in code and only appear as a
// literal hyphenated token inside a documenting comment.
func runtimeJSAttrs(t *testing.T) []string {
	t.Helper()
	files := []string{"runtime.js"}
	entries, err := os.ReadDir("src")
	if err != nil {
		t.Fatalf("read src dir: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
			continue
		}
		files = append(files, filepath.Join("src", e.Name()))
	}

	set := map[string]struct{}{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, m := range attrPattern.FindAllString(string(raw), -1) {
			m = strings.TrimRight(m, "-") // drop comment line-wrap artifacts
			if m == "data-fui" {
				continue
			}
			set[m] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// documentedAttrs returns the set of data-fui-* attributes named anywhere in
// core-ui/ARCHITECTURE.md (the attribute table is the source of truth).
func documentedAttrs(t *testing.T) map[string]struct{} {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "ARCHITECTURE.md"))
	if err != nil {
		t.Fatalf("read ARCHITECTURE.md: %v", err)
	}
	set := map[string]struct{}{}
	for _, m := range attrPattern.FindAllString(string(raw), -1) {
		set[strings.TrimRight(m, "-")] = struct{}{}
	}
	return set
}

// TestRuntimeAttrsAreDocumented enforces hard rule 5: every data-fui-*
// attribute the runtime JS references must appear in the core-ui/ARCHITECTURE.md
// attribute table. This makes the doc the source of truth and fails the build
// the moment a new runtime attribute ships without a doc entry — preventing the
// drift that previously left animate/dropdown/reveal/multiselect markers
// undocumented.
//
// Only the JS→doc direction is asserted: many documented attributes are read
// via the camelCase `dataset` API, matched by CSS, or emitted by Go SSR, so a
// literal doc→JS check would false-positive on legitimate markers.
func TestRuntimeAttrsAreDocumented(t *testing.T) {
	doc := documentedAttrs(t)
	var missing []string
	for _, a := range runtimeJSAttrs(t) {
		if _, ok := doc[a]; !ok {
			missing = append(missing, a)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d data-fui-* attribute(s) used by the runtime JS but missing "+
			"from the core-ui/ARCHITECTURE.md attribute table (hard rule 5):\n  %s\n"+
			"Add a row for each to the attribute table.",
			len(missing), strings.Join(missing, "\n  "))
	}
}
