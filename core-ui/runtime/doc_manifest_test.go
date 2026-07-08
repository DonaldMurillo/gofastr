package runtime

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docManifest extracts the frozen DOC_MANIFEST arrays from runtime.js
// source. The manifest is the single inventory of persistent global
// state the runtime writes on <html>/<body>; this parser is the Go-side
// anchor for the doc-parity check below.
func docManifest(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile("runtime.js")
	if err != nil {
		t.Fatalf("read runtime.js: %v", err)
	}
	out := map[string][]string{}
	for _, key := range []string{"htmlAttrs", "bodyClasses", "singletons"} {
		// Matches the manifest source form: htmlAttrs: Object.freeze('a b c'.split(' ')),
		re := regexp.MustCompile(key + `:\s*Object\.freeze\('([^']*)'\.split`)
		m := re.FindStringSubmatch(string(raw))
		if m == nil {
			t.Fatalf("doc MANIFEST.%s not found in runtime.js — the doc module's frozen manifest is a contract (core-ui/ARCHITECTURE.md, Global document state)", key)
		}
		out[key] = strings.Fields(m[1])
		sort.Strings(out[key])
	}
	return out
}

// architectureDocTable parses the "Global document state" table in
// core-ui/ARCHITECTURE.md and returns name-sets keyed like the JS
// manifest. Rows look like:
//
//	| `<html>` attr | `aria-busy` | ... |
//	| `<body>` class | `fui-sse-up` | ... |
//	| `<body>` singleton | `fui-nav-toast` | ... |
func architectureDocTable(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "ARCHITECTURE.md"))
	if err != nil {
		t.Fatalf("read ARCHITECTURE.md: %v", err)
	}
	sections := strings.Split(string(raw), "### Global document state")
	if len(sections) < 2 {
		t.Fatal(`core-ui/ARCHITECTURE.md has no "Global document state" section — it must document the runtime DOC_MANIFEST`)
	}
	// Stop at the next heading so unrelated tables aren't swept in.
	body := sections[1]
	if i := strings.Index(body, "\n#"); i >= 0 {
		body = body[:i]
	}
	kindKey := map[string]string{
		"attr":      "htmlAttrs",
		"class":     "bodyClasses",
		"singleton": "singletons",
	}
	row := regexp.MustCompile("(?m)^\\|\\s*`<(?:html|body)>`\\s+(attr|class|singleton)\\s*\\|\\s*`([^`]+)`")
	out := map[string][]string{}
	for _, m := range row.FindAllStringSubmatch(body, -1) {
		key := kindKey[m[1]]
		out[key] = append(out[key], m[2])
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// TestDocManifestMatchesArchitectureTable enforces hard rule 5 for
// global document state: the frozen DOC_MANIFEST in runtime.js and the
// "Global document state" table in core-ui/ARCHITECTURE.md must list
// exactly the same html attributes, body classes, and body singleton
// ids — in both directions.
func TestDocManifestMatchesArchitectureTable(t *testing.T) {
	js := docManifest(t)
	md := architectureDocTable(t)
	for _, key := range []string{"htmlAttrs", "bodyClasses", "singletons"} {
		if got, want := strings.Join(md[key], ", "), strings.Join(js[key], ", "); got != want {
			t.Errorf("%s drift between runtime.js DOC_MANIFEST and the ARCHITECTURE.md Global-document-state table:\n  runtime.js:      %s\n  ARCHITECTURE.md: %s",
				key, want, got)
		}
	}
}
