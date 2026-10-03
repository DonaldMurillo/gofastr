package scan

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func TestTextGlobCrossesDirs(t *testing.T) {
	top := "// data-fui-signal\n"
	deep := "var x = 'data-fui-signal'\n"
	n := &upgrade.Note{Find: upgrade.Find{Text: []upgrade.TextMatch{{
		Glob:  "**/*.js",
		Match: regexp.MustCompile(`data-fui-signal`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go":           "package main\n\nfunc main() {}\n",
		"top.js":            top,
		"scripts/deep/r.js": deep,
	}), n)
	// Line-start column: the matcher matches a line, not a position.
	wantHits(t, res, n,
		"scripts/deep/r.js:1:1 text data-fui-signal",
		"top.js:1:1 text data-fui-signal")
}

func TestTextNeverGoOrCSS(t *testing.T) {
	// The pattern text sits in a Go comment and a CSS rule; the text
	// matcher never scans either kind, whatever the glob says.
	n := &upgrade.Note{Find: upgrade.Find{Text: []upgrade.TextMatch{{
		Glob:  "**/*",
		Match: regexp.MustCompile(`data-fui-signal`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go": "package main\n\n// data-fui-signal\nfunc main() {}\n",
		"s.css":   "/* data-fui-signal */\n",
	}), n)
	wantHits(t, res, n)
}
