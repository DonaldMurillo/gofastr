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
func TestDrift_NoInlineStyles(t *testing.T) {
	app, _ := setupServer()
	srv := httptest.NewServer(app.Router)
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
