package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
)

// Drift checks for the UI/runtime contract documented in
// core-ui/ARCHITECTURE.md. These tests run on every CI build so the
// failure modes the doc explicitly names can't sneak back in.
//
// Each subtest is paired with the exact failure-mode language from the
// architecture doc so a future-reader sees "this test exists because
// of failure mode X" without leaving the file.

// pagesToScan returns every server-rendered page on the example site.
// Drift checks fetch each and grep the response body.
func pagesToScan() []string {
	return []string{
		"/",
		"/components/",
		"/components/accordion",
		"/components/tabs",
		"/components/progress",
		"/components/skeleton",
		"/components/breadcrumbs",
		"/components/pagination",
		"/framework-ui/",
		"/framework-ui/datatable",
		"/framework-ui/form",
		"/framework-ui/notification",
		"/framework-ui/theme",
		"/framework-ui/css-loading",
		"/customers",
		"/customers/new",
		"/customers/1",
		"/about",
	}
}

func getHTML(t *testing.T, app http.Handler, path string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s: status %d, want 200", path, rec.Code)
	}
	return rec.Body.String()
}

// TestDrift_NoInlineStyles enforces the CSP contract: rendered HTML
// must not carry `style="…"` attributes. (The runtime swaps partials
// via innerHTML; an inline style snuck into the markup would violate
// `default-src 'self'` and silently fail to apply.)
//
// Failure mode this guards against: a contributor adding
// `render.Tag("div", map[string]string{"style": "display:grid"}, …)`
// instead of a utility class.
// TestDrift_NoWebsiteComponentDefaults enforces the "components own
// their defaults" contract: the example website's theme.go must not
// register CSS rules that look like component-default selectors
// (`.ui-*` without further compositional scoping). Component visual
// rules belong in framework/ui/styles_components.go or the owning
// pattern's RegisterStyle — the website may only override under a
// `.demo-*` / `.site-*` ancestor.
//
// Failure mode this guards against: a contributor patching a missing
// component knob (e.g., a "small" Button) by writing a top-level
// `.ui-button--small` rule in the website CSS instead of adding a
// `Size` field to the component. That hides the gap; the website
// looks fine, but every other app using the framework still sees the
// raw default.
func TestDrift_NoWebsiteComponentDefaults(t *testing.T) {
	data, err := os.ReadFile("theme.go")
	if err != nil {
		t.Fatalf("read theme.go: %v", err)
	}
	// Match `ss.Rule(".ui-…")` where the very next char after .ui- is
	// a letter and the rule string does NOT contain a `.demo-`/`.site-`
	// ancestor. Contextual scoped overrides like
	// `.demo-source [data-fui-comp="ui-code-block"]` are allowed because
	// they live behind a `.demo-…` ancestor.
	rx := regexp.MustCompile(`ss\.Rule\(["\x60]([^"\x60]*\.ui-[a-z][^"\x60]*)["\x60]\)`)
	for _, m := range rx.FindAllStringSubmatch(string(data), -1) {
		sel := m[1]
		// Allowed if the selector is scoped behind a page-chrome ancestor.
		if strings.Contains(sel, ".demo-") || strings.Contains(sel, ".site-") ||
			strings.Contains(sel, ".themed-demo") || strings.Contains(sel, ".popover-demo") ||
			strings.Contains(sel, ".theme-swap") {
			continue
		}
		t.Errorf("website theme.go registers a component-default selector %q — "+
			"`.ui-*` rules belong in the component's own RegisterStyle (e.g., "+
			"framework/ui/styles_components.go or the pattern's css.go), not in "+
			"the website. If you need a new variant, add it to the component "+
			"and use the typed field at the call site.", sel)
	}
}

func TestDrift_NoInlineStyles(t *testing.T) {
	app, _ := setupServer()
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	// `style="…"` not preceded by a non-alphanumeric — catches
	// attribute usage but NOT substrings like "lifestyle" or values
	// containing the word "style".
	rx := regexp.MustCompile(`\sstyle="`)

	for _, p := range pagesToScan() {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Errorf("%s: %v", p, err)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if matches := rx.FindAllStringIndex(string(body), -1); len(matches) > 0 {
			// Emit a snippet around the first match for diagnosis.
			for _, m := range matches[:min(3, len(matches))] {
				start, end := m[0], m[1]
				ctxStart, ctxEnd := start-32, end+96
				if ctxStart < 0 {
					ctxStart = 0
				}
				if ctxEnd > len(body) {
					ctxEnd = len(body)
				}
				t.Errorf("%s: inline style=\"…\" attribute found:\n  …%s…", p, string(body[ctxStart:ctxEnd]))
			}
		}
	}
}

// TestDrift_NoLocationHrefAssign enforces the no-hard-refresh rule.
// `location.href = …` in JS or in a server response is the explicit
// failure mode the doc names: "Never introduce a hard refresh as a
// fix. If you find yourself doing `location.href = …`, stop."
//
// One exception is the runtime's own SPA-router fallback that triggers
// when a partial fetch fails — that's a recovery path, not a feature.
// We allow occurrences inside core-ui/runtime/runtime.js (audited at
// review time) and assert against everything else.
func TestDrift_NoLocationHrefAssignOutsideRuntime(t *testing.T) {
	roots := []string{
		"../../examples/website",
		"../../framework",
		"../../core-ui/widget",
		"../../core-ui/app",
		"../../core-ui/html",
	}
	rx := regexp.MustCompile(`location\.href\s*=`)
	for _, root := range roots {
		if err := walkGoFiles(root, func(path string, data []byte) {
			if rx.Match(data) {
				t.Errorf("%s: contains `location.href = …`; the architecture forbids hard refreshes outside the runtime's fallback path", path)
			}
		}); err != nil {
			// Allow paths that don't exist in this layout.
			if !os.IsNotExist(err) {
				t.Errorf("walk %s: %v", root, err)
			}
		}
	}
}

// TestDrift_RuntimeDataFuiAttributesDocumented enforces that every
// `data-fui-*` attribute the runtime reads is also documented in
// core-ui/ARCHITECTURE.md. The runtime's data-attributes ARE the
// public contract — adding one without doc-ing it is a silent API
// expansion.
//
// We parse the architecture doc's runtime-primitives table and the
// runtime source, diff, and fail on any attribute the runtime reads
// but the doc doesn't mention.
//
// Whitelist for attributes the runtime uses internally but doesn't
// intend as public contract.
var dataFuiWhitelist = map[string]bool{
	// kiln-side primitives kept for kiln's specific UX; they belong to
	// the kiln chat surface and aren't framework-level contract yet.
	"data-fui-charcount-source":          true,
	"data-fui-copy-text-from":            true,
	"data-fui-scroll-bottom-on-update":   true,
	"data-fui-flash-on-update":           true,
	"data-fui-flash-duration-ms":         true,
	"data-fui-tick-elapsed":              true,
	"data-fui-fill-text":                 true,
	"data-fui-fill-input":                true,
	"data-fui-style":                     true,
	"data-fui-clear-on-esc":              true,
	"data-fui-persist-storage":           true,
	"data-fui-shortcut-focus":            true,
	"data-fui-shortcut-click":            true,
	"data-fui-autogrow":                  true,
	"data-fui-disable-when-invalid":      true,
	"data-fui-submit-on-enter":           true,
	"data-fui-signal-attr":               true,
	"data-fui-signal-mode":               true,
	"data-fui-rpc-method":                true,
	"data-fui-rpc-body":                  true,
	"data-fui-rpc-close":                 true,
	"data-fui-rpc-reset":                 true,
	"data-fui-rpc-signal":                true,
	"data-fui-rpc-after-text":            true,
	"data-fui-rpc-after-disable":         true,
	"data-fui-rpc-after-done":            true,
	"data-fui-rpc-scroll-to":             true,
	"data-fui-action":                    true,
	"data-fui-widget":                    true,
	"data-fui-style-href":                true,
	"data-fui-backdrop":                  true,
}

func TestDrift_RuntimeDataFuiAttributesDocumented(t *testing.T) {
	// Scan the bundled core runtime AND every demand-loaded module under
	// core-ui/runtime/src/. After the runtime code-split, new attributes
	// frequently land inside per-module files (popover.js, widgets.js,
	// toasts.js, …); a drift test that reads only runtime.js would let
	// them ship undocumented.
	sources := []string{"../../core-ui/runtime/runtime.js"}
	moduleEntries, mErr := os.ReadDir("../../core-ui/runtime/src")
	if mErr == nil {
		for _, e := range moduleEntries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".js") {
				sources = append(sources, "../../core-ui/runtime/src/"+e.Name())
			}
		}
	}
	var allBytes []byte
	for _, p := range sources {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Skipf("can't read %s: %v", p, err)
		}
		allBytes = append(allBytes, data...)
		allBytes = append(allBytes, '\n')
	}
	doc, err := os.ReadFile("../../core-ui/ARCHITECTURE.md")
	if err != nil {
		t.Skipf("can't read ARCHITECTURE.md: %v", err)
	}

	// Match every data-fui-* attribute name appearing in the runtime
	// (in string literals or DOM queries).
	attrRx := regexp.MustCompile(`data-fui-[a-z][a-z0-9-]*`)
	runtimeAttrs := uniqStrings(attrRx.FindAllString(string(allBytes), -1))
	docText := string(doc)

	var undocumented []string
	for _, a := range runtimeAttrs {
		if dataFuiWhitelist[a] {
			continue
		}
		if !strings.Contains(docText, a) {
			undocumented = append(undocumented, a)
		}
	}
	if len(undocumented) > 0 {
		t.Errorf("runtime reads data-fui-* attributes not mentioned in core-ui/ARCHITECTURE.md: %v\n"+
			"Add them to the runtime-primitives table, or add to the whitelist in drift_test.go if they are internal-only.",
			undocumented)
	}
}

// TestDrift_PackagesUnderTestableRootsHaveTests enforces that every
// package under the directories below carries at least one *_test.go.
// Packages that ship a public API without tests are how regressions
// sneak in — the drift test fails loudly the moment a new package
// lands without a paired test file, prompting the author to add one
// before merge.
//
// Roots scanned (relative to this test file):
//
//   - ../../framework/ui          — every styled component
//   - ../../core-ui/patterns      — every composed pattern
//   - ../../core-ui/widget/preset — every widget preset
//
// Allowlisted package paths (intentional no-test packages) live in
// testablePackagesAllowlist below.
var testablePackagesAllowlist = map[string]bool{
	// Add explicit allowlist entries with a justification comment if
	// a package legitimately ships zero testable surface (e.g. it's
	// a pure type-alias shim). Empty by design — keep it strict.
}

func TestDrift_PackagesUnderTestableRootsHaveTests(t *testing.T) {
	roots := []string{
		"../../framework/ui",
		"../../core-ui/patterns",
		"../../core-ui/widget/preset",
	}

	type pkg struct {
		dir     string
		hasGo   bool
		hasTest bool
	}
	pkgs := map[string]*pkg{}

	for _, root := range roots {
		err := walkDir(root, func(path string, isDir bool) {
			if isDir {
				return
			}
			if !strings.HasSuffix(path, ".go") {
				return
			}
			dir := dirOf(path)
			p, ok := pkgs[dir]
			if !ok {
				p = &pkg{dir: dir}
				pkgs[dir] = p
			}
			if strings.HasSuffix(path, "_test.go") {
				p.hasTest = true
			} else {
				p.hasGo = true
			}
		})
		if err != nil && !os.IsNotExist(err) {
			t.Errorf("walk %s: %v", root, err)
		}
	}

	var missing []string
	for _, p := range pkgs {
		if !p.hasGo {
			continue
		}
		if p.hasTest {
			continue
		}
		// Allowlist key: the path relative to the repo root.
		rel := strings.TrimPrefix(p.dir, "../../")
		if testablePackagesAllowlist[rel] {
			continue
		}
		missing = append(missing, rel)
	}
	if len(missing) > 0 {
		// Sort for stable error output.
		sortStrings(missing)
		t.Errorf("packages under testable roots have NO *_test.go: %v\n"+
			"Every package shipping a public API must carry at least one test. "+
			"Add a <pkg>_test.go (smoke tests, panic assertions, ARIA-shape "+
			"checks — all welcome), or — if zero testable surface is intentional "+
			"— add the package path to testablePackagesAllowlist in drift_test.go "+
			"with a justification comment.", missing)
	}
}

// walkDir is a minimal recursive walker that calls fn(path, isDir)
// for every entry rooted at root.
func walkDir(root string, fn func(path string, isDir bool)) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		full := root + "/" + e.Name()
		if e.IsDir() {
			fn(full, true)
			if err := walkDir(full, fn); err != nil {
				return err
			}
			continue
		}
		fn(full, false)
	}
	return nil
}

// TestDrift_EveryComponentPageHasE2ETest enforces that every
// /components/<slug> route registered in main.go is referenced by at
// least one chromedp e2e test in this directory. Unit tests prove
// the render math; e2e tests prove the page actually loads + the
// runtime hydrates the interactive parts. The drift gate fails the
// moment a new component page lands without a paired e2e test.
//
// Allowlisted slugs (intentionally e2e-less pages) live in
// componentE2EAllowlist below.
var componentE2EAllowlist = map[string]bool{
	// "" is the components index page — its content is the list of
	// other component cards and doesn't need its own behavioural test.
	"": true,
}

func TestDrift_EveryComponentPageHasE2ETest(t *testing.T) {
	// 1. Parse main.go for every site.Register("/components/<slug>", …).
	mainGo, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	rx := regexp.MustCompile(`site\.Register\("/components/([a-z0-9-]*)"`)
	matches := rx.FindAllStringSubmatch(string(mainGo), -1)
	slugs := map[string]bool{}
	for _, m := range matches {
		slugs[m[1]] = true
	}
	if len(slugs) == 0 {
		t.Fatal("no /components/<slug> routes found in main.go — drift test broken")
	}

	// 2. Read every *_test.go in this directory and concat.
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	var allTests []byte
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		data, err := os.ReadFile(e.Name())
		if err != nil {
			t.Errorf("read %s: %v", e.Name(), err)
			continue
		}
		allTests = append(allTests, data...)
		allTests = append(allTests, '\n')
	}

	// 3. For each slug, check the test corpus references
	// "/components/<slug>" somewhere.
	var missing []string
	for slug := range slugs {
		if componentE2EAllowlist[slug] {
			continue
		}
		needle := `/components/` + slug
		// Guard against substring matches (e.g. /components/menu vs
		// /components/menubar): require the slug to be followed by a
		// non-letter / non-digit / non-dash character (or end-of-string).
		idx := 0
		found := false
		for {
			i := strings.Index(string(allTests[idx:]), needle)
			if i < 0 {
				break
			}
			pos := idx + i + len(needle)
			if pos >= len(allTests) {
				found = true
				break
			}
			c := allTests[pos]
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-') {
				found = true
				break
			}
			idx = pos
		}
		if !found {
			missing = append(missing, slug)
		}
	}
	if len(missing) > 0 {
		sortStrings(missing)
		t.Errorf("component pages have NO e2e test referencing their route: %v\n"+
			"Every /components/<slug> route registered in main.go must be "+
			"navigated to by at least one chromedp test in this directory "+
			"(either ARIA-shape or interaction). Add a TestE2E_… that hits "+
			"the page, or — if zero behavioural surface is intentional — "+
			"add the slug to componentE2EAllowlist in drift_test.go with "+
			"a justification comment.", missing)
	}
}

// TestDrift_DocsIndexListsEveryComponent enforces that every
// /components/<slug> route registered in main.go is mentioned in
// framework/docs/content/ui-new-components.md. The doc is a one-page catalog meant
// to point readers at the live demo + Go docs; if a new component
// lands without an index entry, readers can't find it without
// grepping main.go.
var docsIndexAllowlist = map[string]bool{}

func TestDrift_DocsIndexListsEveryComponent(t *testing.T) {
	mainGo, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	rx := regexp.MustCompile(`site\.Register\("/components/([a-z0-9-]+)"`)
	matches := rx.FindAllStringSubmatch(string(mainGo), -1)
	slugs := map[string]bool{}
	for _, m := range matches {
		slugs[m[1]] = true
	}
	if len(slugs) == 0 {
		t.Fatal("no /components/<slug> routes found in main.go — drift test broken")
	}

	doc, err := os.ReadFile("../../framework/docs/content/ui-new-components.md")
	if err != nil {
		t.Fatalf("read framework/docs/content/ui-new-components.md: %v", err)
	}
	docText := string(doc)

	var missing []string
	for slug := range slugs {
		if docsIndexAllowlist[slug] {
			continue
		}
		// Look for `**<slug>**` (the bullet anchor) — guarantees the
		// match is a real list entry, not an incidental mention.
		needle := "**" + slug + "**"
		if !strings.Contains(docText, needle) {
			missing = append(missing, slug)
		}
	}
	if len(missing) > 0 {
		sortStrings(missing)
		t.Errorf("framework/docs/content/ui-new-components.md is missing entries for: %v\n"+
			"Each slug registered in main.go must appear as `**<slug>**` "+
			"in the catalog section of the index doc. Add a one-line "+
			"bullet referencing the constructor + a short description.",
			missing)
	}
}

func dirOf(path string) string {
	if i := strings.LastIndex(path, "/"); i >= 0 {
		return path[:i]
	}
	return "."
}

func sortStrings(s []string) {
	// minimal insertion sort — list sizes here are tiny.
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func walkGoFiles(root string, fn func(path string, data []byte)) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := root + "/" + e.Name()
		if e.IsDir() {
			if err := walkGoFiles(path, fn); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			continue // tests can reference forbidden patterns in fixtures
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		fn(path, data)
	}
	return nil
}

func uniqStrings(in []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
