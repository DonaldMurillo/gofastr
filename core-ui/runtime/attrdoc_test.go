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

// privilegedAttrs are runtime-read attributes that do NOT carry the
// data-fui- prefix, so attrPattern cannot see them. Every one of them
// changes what code runs, data-behavior is a <script src> sink,
// data-widget/data-component select hydration behaviour, data-bind
// writes into the state store, which makes them exactly the set Hard
// rule 5 exists for. Naming them explicitly keeps the parity gate from
// being satisfied in letter and defeated in spirit; a plain widening to
// data-[a-z0-9-]+ would instead drag in every presentational data-*
// attribute the runtime happens to touch.
var privilegedAttrs = []string{
	"data-behavior",
	"data-widget",
	"data-component",
	"data-bind",
}

// runtimeJSAttrs returns every data-fui-* attribute literally referenced in
// the runtime sources: the bundled runtime.js, every on-demand src/*.js
// module, and every frag/*.js fragment. Comments are intentionally included:
// several attributes (e.g. data-fui-rpc-after-done) are read through the
// camelCase `dataset` API in code and only appear as a literal hyphenated
// token inside a documenting comment.
//
// frag/*.js is scanned SEPARATELY from runtime.js even though runtime.js is
// composed from fragments. runtime.js is only the `full` composition, so an
// attribute introduced by a fragment that composition omits, boot-embed,
// rpc-stub, widgets-boot-static, appeared in no scanned file and was
// invisible to the ownership and documentation gates below. Permanently: the
// gate could never fail for it. data-fui-embed-state shipped that way.
func runtimeJSAttrs(t *testing.T) []string {
	t.Helper()
	files := []string{"runtime.js"}
	for _, dir := range []string{"src", "frag"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s dir: %v", dir, err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".js") {
				continue
			}
			files = append(files, filepath.Join(dir, e.Name()))
		}
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
// cross-check, false negatives (comment content included) are safe because
// they just add extra names to the checked set.
func stripGoLineComments(src string) string {
	var out strings.Builder
	for line := range strings.SplitSeq(src, "\n") {
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
// New CSS-only additions must be documented here, that's intentional friction
// to keep the list honest.
func TestGoInteractiveAttrsMatchRuntime(t *testing.T) {
	// Attributes emitted by Go but never read by JS logic: they are CSS
	// attribute selectors, SSR-output-only markers, or runtime-written keys
	// that the Go side emits as initial values but JS never getAttribute()s.
	cssOnlyAttrs := map[string]bool{
		// Marks which styled component a DOM node belongs to.
		// The runtime's CSS scanner reads data-fui-comp values (for loadComponentCSS),
		// but the attribute itself is emitted by Go as a plain string label.
		// It IS present in the JS (scanner reads it), so it passes the check,
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
// positive test of the test's sensitivity, if it fails, the cross-check is
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
// the moment a new runtime attribute ships without a doc entry, preventing the
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

// TestPrivilegedAttrsAreDocumented extends hard rule 5 past the
// data-fui- prefix.
//
// The parity gate matched only `data-fui-*`, so the runtime's single
// most privileged attribute, `data-behavior`, which becomes a
// `<script src>`, appeared zero times in ARCHITECTURE.md and nobody
// noticed. Same for data-widget / data-component / data-bind. The rule
// was satisfied in letter and defeated in spirit.
func TestPrivilegedAttrsAreDocumented(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "ARCHITECTURE.md"))
	if err != nil {
		t.Fatalf("read ARCHITECTURE.md: %v", err)
	}
	arch := string(raw)
	for _, a := range privilegedAttrs {
		if !strings.Contains(arch, "`"+a) {
			t.Errorf("privileged runtime attribute %q is not documented in core-ui/ARCHITECTURE.md (hard rule 5)", a)
		}
	}
}

// TestPrivilegedAttrsStillRead guards the other direction: if one of
// these is renamed or dropped, the doc row and this list must follow.
// A stale entry here would keep documenting an attribute nothing reads.
func TestPrivilegedAttrsStillRead(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range privilegedAttrs {
		if !strings.Contains(js, a) {
			t.Errorf("privileged attribute %q is documented + listed but no longer read by runtime.js — drop it from both", a)
		}
	}
}

// --- Runtime composition gate (spec: scratchpad/SPEC-runtime-composer.md) ---
//
// The four tests below extend this file's parity mechanism to the
// attribute→fragment map declared in fragments.go. Together they enforce
// that every data-fui-* attribute in the runtime sources has exactly one
// declared owner (a core fragment or an on-demand module), that the
// declared set never drifts ahead of the source, and that the fragment
// dependency graph is well-formed. This is the build-time half of Hard
// rule 5: a new attribute without an owning fragment fails the build
// before it can ship, and a removed attribute leaves a stale entry that
// also fails.
//
// They reuse runtimeJSAttrs (the same source-of-truth scanner the
// doc-parity tests above use) so there is one definition of "what
// attributes exist", not two.

// TestFragmentMapComplete is gate item 1: every data-fui-* attribute the
// runtime JS references must have a declared owning fragment or module in
// fragments.go. A new attribute that ships without an entry here fails the
// build, which is the point: composition cannot serve an attribute whose
// owner it does not know, and the map is how it knows.
func TestFragmentMapComplete(t *testing.T) {
	var unowned []string
	for _, a := range runtimeJSAttrs(t) {
		kind, _ := attrOwner(a)
		if kind == "" {
			unowned = append(unowned, a)
		}
	}
	if len(unowned) > 0 {
		t.Errorf("%d data-fui-* attribute(s) referenced by the runtime sources have "+
			"no owning fragment or module in fragments.go (runtime-composer gate):\n  %s\n"+
			"Assign each to the fragment (fragmentAttrs) or src/*.js module (moduleAttrs) "+
			"that implements its handler — see the ownership rule in fragments.go.",
			len(unowned), strings.Join(unowned, "\n  "))
	}
}

// TestFragmentMapNoStale is gate item 2: every attribute declared in
// fragments.go must still exist in the runtime sources. A stale entry is
// usually a rename or removal that forgot to update the map; left in place
// it would make the composition serve a fragment for a marker nobody emits.
func TestFragmentMapNoStale(t *testing.T) {
	src := map[string]struct{}{}
	for _, a := range runtimeJSAttrs(t) {
		src[a] = struct{}{}
	}
	check := func(owner, attr string) {
		if _, ok := src[attr]; !ok {
			t.Errorf("attribute %q declared under %q in fragments.go is NOT present in any "+
				"runtime source (runtime.js or src/*.js) — it was likely renamed or removed; "+
				"drop the entry or reassign the new name.", attr, owner)
		}
	}
	for frag, attrs := range fragmentAttrs {
		for _, a := range attrs {
			check("fragment "+frag, a)
		}
	}
	for mod, attrs := range moduleAttrs {
		for _, a := range attrs {
			check("module "+mod, a)
		}
	}
}

// TestFragmentMapNoDuplicate asserts the two tables never assign the same
// attribute twice. A double-assignment would make the completeness gate
// ambiguous about which owner serves the marker, and is almost always a
// transcription error introduced while splitting the map.
func TestFragmentMapNoDuplicate(t *testing.T) {
	seen := make(map[string]string) // attr -> first owner
	record := func(owner, attr string) {
		if prev, ok := seen[attr]; ok {
			t.Errorf("attribute %q is assigned to BOTH %q and %q — each data-fui-* attribute "+
				"has exactly one owner; pick the fragment/module that implements its handler.",
				attr, prev, owner)
		}
		seen[attr] = owner
	}
	for frag, attrs := range fragmentAttrs {
		for _, a := range attrs {
			record("fragment "+frag, a)
		}
	}
	for mod, attrs := range moduleAttrs {
		for _, a := range attrs {
			record("module "+mod, a)
		}
	}
}

// TestFragmentNamesValid is gate item 3: every key in fragmentAttrs must be
// one of the declared fragment names in `fragments`. A typo here
// (e.g. "widget-boot" vs "widgets-boot") would otherwise silently produce a
// fragment that no composition can name.
func TestFragmentNamesValid(t *testing.T) {
	for frag := range fragmentAttrs {
		if _, ok := fragments[frag]; !ok {
			t.Errorf("fragmentAttrs key %q is not in the declared fragment set `fragments` "+
				"(valid names: kernel, signals, nav, widgets-boot, sse, compute, boot-embed) "+
				"— fix the typo or add the fragment to `fragments`.", frag)
		}
	}
}

// TestFragmentDepsAcyclic is gate item 4: the declared dependency edges
// must form a DAG and every named dependency must resolve to a declared
// fragment. Without this the composer's closure could recurse infinitely or
// reference a fragment that does not exist, which is the failure mode that
// turns the map from a gate into mere documentation.
func TestFragmentDepsAcyclic(t *testing.T) {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS path (back-edge ⇒ cycle)
		black = 2 // fully explored
	)
	color := make(map[string]int, len(fragments))
	var visit func(name string, path []string) bool
	visit = func(name string, path []string) bool {
		switch color[name] {
		case gray:
			t.Errorf("fragment dependency cycle: %s -> %s",
				strings.Join(append(path, name), " -> "), name)
			return false
		case black:
			return true
		}
		def, ok := fragments[name]
		if !ok {
			t.Errorf("dependency %q is referenced in a fragment's deps but is not a declared "+
				"fragment — add it to `fragments` or fix the name.", name)
			return false
		}
		color[name] = gray
		for _, d := range def.deps {
			if !visit(d, append(path, name)) {
				return false
			}
		}
		color[name] = black
		return true
	}
	// Visit in sorted order so any failure report is stable across runs.
	names := make([]string, 0, len(fragments))
	for n := range fragments {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		visit(n, nil)
	}
}

// TestFragmentModulesValid extends gate item 3 to module-owned attributes:
// every key in moduleAttrs must be a real embedded runtime module (a file
// under src/*.js served at /__gofastr/runtime/<name>.js). A module name that
// no .js file backs would direct a marker's demand-load at a 404. Named
// TestFragment* so every gate test runs under a single -run filter.
func TestFragmentModulesValid(t *testing.T) {
	real := map[string]struct{}{}
	for _, n := range ModuleNames() {
		real[n] = struct{}{}
	}
	var stale []string
	for mod := range moduleAttrs {
		if _, ok := real[mod]; !ok {
			stale = append(stale, mod)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%d moduleAttrs key(s) are not embedded src/*.js modules:\n  %s\n"+
			"Every moduleAttrs key must match a src/<name>.js file. Drop the entry or "+
			"add the module file.", len(stale), strings.Join(stale, "\n  "))
	}
}

// TestDocumentedAttrsHaveAnOwner is the doc→owner direction the existing
// parity gates deliberately skip (TestRuntimeAttrsAreDocumented asserts only
// runtime→doc, and only for attributes the runtime references). A data-fui-*
// attribute documented in ARCHITECTURE.md must be backed by SOMETHING real:
// either the runtime JS reads it, or Go source emits it as an HTML attribute.
// An attribute that is documented but neither read nor emitted is stale doc,
// the InlineEdit marker (documented for years, no component, no runtime
// handler) fell through exactly this hole.
//
// The reverse direction is intentionally not a strict bijection: many
// documented attributes are read via camelCase dataset, matched by CSS, or
// emitted by Go SSR, so a literal doc↔JS equality would false-positive. This
// test only fails on the narrow "documented, owned by nobody" case.
func TestDocumentedAttrsHaveAnOwner(t *testing.T) {
	// Explicitly-deferred attributes: documented as a planned surface but with
	// no owner yet. Every entry needs a one-line reason; an entry with no real
	// plan is drift, not a deferral, and must not hide here.
	deferredAttrs := map[string]string{
		// Plugin-supplied (wysiwyg editor): emitted via framework/pluginhost
		// MountConfig.Attributes by application/plugin code, not by framework
		// source, so the goEmittedAttrs scan cannot see it. The framework
		// emits the core data-fui-plugin* markers; this namespaced extra is
		// the documented extension point (ARCHITECTURE.md "Plugins may add
		// namespaced data-fui-plugin-* extras").
		"data-fui-plugin-for": "plugin-supplied via MountConfig.Attributes",
	}

	doc := documentedAttrs(t)
	haveRuntime := map[string]struct{}{}
	for _, a := range runtimeJSAttrs(t) {
		haveRuntime[a] = struct{}{}
	}
	haveGo := map[string]struct{}{}
	for _, a := range goEmittedAttrs(t) {
		haveGo[a] = struct{}{}
	}

	var drift []string
	for a := range doc {
		if _, ok := haveRuntime[a]; ok {
			continue
		}
		if _, ok := haveGo[a]; ok {
			continue
		}
		if _, ok := deferredAttrs[a]; ok {
			continue
		}
		drift = append(drift, a)
	}
	sort.Strings(drift)
	if len(drift) > 0 {
		t.Errorf("doc/code drift: %d data-fui-* attribute(s) are documented in "+
			"ARCHITECTURE.md but neither read by the runtime JS nor emitted by any "+
			"Go component — remove the stale doc row or implement the surface:\n  %s",
			len(drift), strings.Join(drift, "\n  "))
	}
}

// goEmittedAttrs scans the Go source that emits data-fui-* HTML attributes,
// core-ui/{interactive,html,widget,app,patterns/**} and framework/ui, for
// literal "data-fui-*" string constants, in non-comment, non-test source. It
// generalizes goInteractiveAttrs (which covers core-ui/interactive alone) so
// the doc→owner gate can see attributes emitted outside the interactive
// package, e.g. framework/ui.TagInput's data-fui-tag-input-id.
func goEmittedAttrs(t *testing.T) []string {
	t.Helper()
	roots := []string{
		filepath.Join("..", "interactive"),
		filepath.Join("..", "html"),
		filepath.Join("..", "widget"),
		filepath.Join("..", "app"),
		filepath.Join("..", "patterns"),
		filepath.Join("..", "..", "framework", "ui"),
		// pluginhost emits the data-fui-plugin* mount markers via string
		// concat (mount.go); it owns its own parity gate too, but the
		// doc→owner gate must still see them as emitted.
		filepath.Join("..", "..", "framework", "pluginhost"),
	}
	set := map[string]struct{}{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			stripped := stripGoLineComments(string(raw))
			// attrPattern over stripped whole source catches valueless boolean
			// attributes (data-fui-menu-panel) and concat-built markers
			// (data-fui-plugin-docid) that the quote-requiring form misses.
			for _, m := range attrPattern.FindAllString(stripped, -1) {
				name := strings.TrimRight(m, "-")
				if name == "data-fui" {
					continue
				}
				set[name] = struct{}{}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	out := make([]string, 0, len(set))
	for a := range set {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}
