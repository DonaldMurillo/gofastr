package scan

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// A line-anchored text pattern still matches line 1 of a file saved with a
// byte order mark, at the column the reader sees.
func TestTextMatchIgnoresBOM(t *testing.T) {
	n := &upgrade.Note{Find: upgrade.Find{Text: []upgrade.TextMatch{{
		Glob:  "**/*.js",
		Match: regexp.MustCompile(`^data-cui-signal`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"a.js":    "\xEF\xBB\xBFdata-cui-signal\n",
	}), n)
	wantHits(t, res, n, "a.js:1:1 text ^data-cui-signal")
}
