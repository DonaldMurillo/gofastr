package scan

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// A Config Value regexp matches the scalar's PARSED value as scalarText
// prints it, never the source token: core/yaml keeps parsed values, not
// spellings, so theme: 2.0 is float64(2) and prints "2", ~ is nil and
// prints "<nil>" (#669, R8-NC-SF-03). upgrading.md lists these forms; this
// test pins them so neither the printing nor the doc drifts. A note
// written against the source spelling ("^2\\.0$") reports a false clean —
// the negative case below is the hazard, pinned.
func TestConfigScalarValueMatchesParsedForm(t *testing.T) {
	yml := `app:
  theme: 2.0
  ratio: 1.10
  kilo: 1e3
  price: 3.250
  count: 42
  flag: true
  plain: hello
  nothing: ~
`
	files := map[string]string{
		"main.go":     "package main\n\nfunc main() {}\n",
		"gofastr.yml": yml,
	}
	for _, tc := range []struct{ key, value string }{
		{"app.theme", `^2$`},    // 2.0 → float64(2)
		{"app.ratio", `^1\.1$`}, // 1.10 → float64(1.1)
		{"app.kilo", `^1000$`},  // 1e3 → float64(1000)
		{"app.price", `^3\.25$`},
		{"app.count", `^42$`},
		{"app.flag", `^true$`},
		{"app.plain", `^hello$`}, // strings are the one verbatim kind
		{"app.nothing", `^<nil>$`},
	} {
		n := &upgrade.Note{Find: upgrade.Find{Config: []upgrade.ConfigMatch{{
			Key:   tc.key,
			Value: regexp.MustCompile(tc.value),
		}}}}
		res := mustRun(t, newWorkspace(t, defaultKit, files), n)
		wantHits(t, res, n,
			fmt.Sprintf("gofastr.yml:%d:0 config %s", lineOf(yml, strings.Split(tc.key, ".")[1]), tc.key))
	}

	// The source spelling does not match: a note written as `^2\.0$`
	// reads "clean" although theme holds the value the note is about.
	n := &upgrade.Note{Find: upgrade.Find{Config: []upgrade.ConfigMatch{{
		Key:   "app.theme",
		Value: regexp.MustCompile(`^2\.0$`),
	}}}}
	res := mustRun(t, newWorkspace(t, defaultKit, files), n)
	if len(res.Hits) != 0 {
		t.Fatalf("source spelling matched: %v", res.Hits)
	}
}
