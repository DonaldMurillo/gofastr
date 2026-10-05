package scan

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func TestTextGlobCrossesDirs(t *testing.T) {
	top := "// data-cui-signal\n"
	deep := "var x = 'data-cui-signal'\n"
	n := &upgrade.Note{Find: upgrade.Find{Text: []upgrade.TextMatch{{
		Glob:  "**/*.js",
		Match: regexp.MustCompile(`data-cui-signal`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":           "package main\n\nfunc main() {}\n",
		"top.js":            top,
		"scripts/deep/r.js": deep,
	}), n)
	// Line-start column: the matcher matches a line, not a position.
	wantHits(t, res, n,
		"scripts/deep/r.js:1:1 text data-cui-signal",
		"top.js:1:1 text data-cui-signal")
}

func TestTextNeverGoOrCSS(t *testing.T) {
	// The pattern text sits in a Go comment and a CSS rule; the text
	// matcher never scans either kind, whatever the glob says.
	n := &upgrade.Note{Find: upgrade.Find{Text: []upgrade.TextMatch{{
		Glob:  "**/*",
		Match: regexp.MustCompile(`data-cui-signal`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go": "package main\n\n// data-cui-signal\nfunc main() {}\n",
		"s.css":   "/* data-cui-signal */\n",
	}), n)
	wantHits(t, res, n)
}

func TestTextSkipsMinifiedBundles(t *testing.T) {
	// A vendored bundle packs a library onto one enormous line, and a
	// .min.js name says the same thing up front. A hit in either is
	// someone else's build: the host cannot act on it, so the matcher
	// skips both and keeps the hand-written hit.
	long := "var a=1;" + strings.Repeat("x", minifiedLineBytes) + "o.observe(document.body,{});\n"
	n := &upgrade.Note{Find: upgrade.Find{Text: []upgrade.TextMatch{{
		Glob:  "**/*.js",
		Match: regexp.MustCompile(`\.observe\(\s*document`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":           "package main\n\nfunc main() {}\n",
		"assets/map.js":     long,
		"assets/lib.min.js": "o.observe(document.body,{});\n",
		"assets/widget.js":  "const mo = new MutationObserver(f);\nmo.observe(document.body, {childList: true});\n",
	}), n)
	wantHits(t, res, n, "assets/widget.js:2:1 text \\.observe\\(\\s*document")
}

func TestTextLongLinesCountOutsideScripts(t *testing.T) {
	// A kiln journal line carries a whole entity snapshot and runs far
	// past the minified-line bound; it is the host's own record, so the
	// JSONL notes must still hit it. Only script files get the skip, and
	// a .min.<data ext> name is not a minified script either.
	longLine := `{"kind":"update_entity","entity":{"pad":"` + strings.Repeat("x", minifiedLineBytes) + `"}}` + "\n"
	n := &upgrade.Note{Find: upgrade.Find{Text: []upgrade.TextMatch{{
		Glob:  "**/*.jsonl",
		Match: regexp.MustCompile(`update_entity`),
	}, {
		Glob:  "**/*.json",
		Match: regexp.MustCompile(`update_entity`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":                "package main\n\nfunc main() {}\n",
		"journal/session.jsonl":  longLine,
		"fixtures/seed.min.json": `{"kind":"update_entity"}` + "\n",
	}), n)
	wantHits(t, res, n,
		"fixtures/seed.min.json:1:1 text update_entity",
		"journal/session.jsonl:1:1 text update_entity")
}
