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

// goInteractiveAttrs scans the Go source files in core-ui/interactive for
// literal "data-fui-*" string constants that the package emits as HTML
// attributes. These are the names the Go side promises to the runtime contract
// and must match exactly what the runtime JS reads.
//
// Only non-comment, non-test-file Go source is scanned: comment lines (//…)
// and block comments are stripped so stale doc-comments referencing
// hypothetical future attributes don't pollute the set.
func goInteractiveAttrs(t *testing.T) []string {
	t.Helper()
	interactiveDir := filepath.Join("..", "interactive")
	entries, err := os.ReadDir(interactiveDir)
	if err != nil {
		t.Fatalf("read interactive dir: %v", err)
	}

	// goStringLiteral matches a double-quoted Go string containing a data-fui-* attr name.
	goStringLiteral := regexp.MustCompile(`"(data-fui-[a-z0-9-]+)"`)

	set := map[string]struct{}{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(interactiveDir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		// Strip single-line comments before scanning so stale "// (data-fui-rpc-signals)"
		// references in package doc-comments don't appear as emitted attributes.
		stripped := stripGoLineComments(string(raw))
		for _, m := range goStringLiteral.FindAllStringSubmatch(stripped, -1) {
			name := strings.TrimRight(m[1], "-")
			if name == "data-fui" {
				continue
			}
			set[name] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// stripGoLineComments removes // line comments from Go source text.
// Block comments (/* … */) are left in place; they are rare in attribute
// declarations and handling them correctly requires a full lexer. The
// goal is only to exclude stale doc-comment references from the attribute
// cross-check — false negatives (comment content included) are safe because
// they just add extra names to the checked set.
func stripGoLineComments(src string) string {
	var out strings.Builder
	for _, line := range strings.Split(src, "\n") {
		// Locate // that isn't inside a string. Simple heuristic: find the
		// first // that appears before any unpaired " on the line.
		trimmed := line
		quote := byte(0)
		for i := 0; i < len(line)-1; i++ {
			c := line[i]
			if quote != 0 {
				if c == '\\' {
					i++ // skip escaped char
					continue
				}
				if c == quote {
					quote = 0
				}
				continue
			}
			if c == '"' || c == '\'' || c == '`' {
				quote = c
				continue
			}
			if c == '/' && line[i+1] == '/' {
				trimmed = line[:i]
				break
			}
		}
		out.WriteString(trimmed)
		out.WriteByte('\n')
	}
	return out.String()
}

// TestGoInteractiveAttrsMatchRuntime is the M4 cross-check: every data-fui-*
// attribute emitted by the Go core-ui/interactive package must be literally
// present somewhere in runtime.js or a src/*.js module.
//
// This permanently catches the F3 class of bug (Go emitted
// "data-fui-rpc-debounce", runtime read "data-fui-rpc-debounce-ms") by
// failing CI the moment a Go-side attribute name diverges from the JS side.
//
// CSS-only attributes (comp markers, CSS-selector-only targets) that are never
// read by JS logic are listed in cssOnlyAttrs and excluded from the check.
// New CSS-only additions must be documented here — that's intentional friction
// to keep the list honest.
func TestGoInteractiveAttrsMatchRuntime(t *testing.T) {
	// Attributes emitted by Go but never read by JS logic: they are CSS
	// attribute selectors, SSR-output-only markers, or runtime-written keys
	// that the Go side emits as initial values but JS never getAttribute()s.
	cssOnlyAttrs := map[string]bool{
		// Marks which styled component a DOM node belongs to.
		// The runtime's CSS scanner reads data-fui-comp values (for loadComponentCSS),
		// but the attribute itself is emitted by Go as a plain string label.
		// It IS present in the JS (scanner reads it), so it passes the check —
		// listed here only for documentation.
	}

	jsAttrs := map[string]struct{}{}
	for _, a := range runtimeJSAttrs(t) {
		jsAttrs[a] = struct{}{}
	}

	var missing []string
	for _, a := range goInteractiveAttrs(t) {
		if cssOnlyAttrs[a] {
			continue
		}
		if _, ok := jsAttrs[a]; !ok {
			missing = append(missing, a)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d data-fui-* attribute(s) emitted by core-ui/interactive Go source "+
			"but NOT present anywhere in the runtime JS — this is the F3-class bug "+
			"(Go name ≠ runtime-read name).\nCheck attribute spelling against runtime.js:\n  %s\n"+
			"If the attribute is CSS-only (never getAttribute'd by JS), add it to cssOnlyAttrs.",
			len(missing), strings.Join(missing, "\n  "))
	}
}

// TestGoInteractiveAttrsMatchRuntime_FailsOnMismatch verifies the test
// mechanism itself: it would catch a reintroduced name mismatch. This is a
// positive test of the test's sensitivity — if it fails, the cross-check is
// broken.
//
// We don't actually reintroduce a mismatched name here (that would be
// circular). Instead we assert directly: "data-fui-rpc-debounce" (the old
// wrong name from F3) must NOT appear in the Go interactive source.
func TestGoInteractiveAttrs_F3NameAbsent(t *testing.T) {
	for _, a := range goInteractiveAttrs(t) {
		if a == "data-fui-rpc-debounce" {
			t.Error(`data-fui-rpc-debounce (F3 wrong name) found in core-ui/interactive — ` +
				`must be data-fui-rpc-debounce-ms to match the runtime`)
		}
	}
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
