package scan

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// cgoFile imports "C" beside the kit: the go command compiles cgo's
// rewrite of it, which lives in the build cache.
const cgoFile = `package main

// #include <stdlib.h>
import "C"

import (
	"example.com/kit/patterns/accordion"
	"example.com/kit/ui"
)

var _ = accordion.Render
var _ = ui.SiteHeader("x")
var _ = ui.FormConfig{Action: "/x"}
var _ = "ui-old"
`

// A file that imports "C" is scanned by every matcher, whether cgo is on
// for the main load (its compiled copy sits in the build cache and names
// the app's file only through //line directives) or off (go/build then
// ignores the file, so a configuration load with cgo on must reach it).
// Before, the imports, fields and strings matchers dropped every hit in
// it and the report listed it as not compiled.
func TestScanCgoFileHit(t *testing.T) {
	if _, err := exec.LookPath("cc"); err != nil {
		if _, err := exec.LookPath("gcc"); err != nil {
			t.Skip("no C compiler: cgo cannot preprocess the file")
		}
	}
	accordion := "example.com/kit/patterns/accordion"
	formAction := upgrade.Symbol{Pkg: "example.com/kit/ui", Name: "FormConfig", Member: "Action"}
	n := &upgrade.Note{Find: upgrade.Find{
		Uses:    []upgrade.Symbol{siteHeaderSym},
		Imports: []string{accordion},
		Fields:  []upgrade.FieldMatch{{Field: formAction}},
		Strings: upgrade.StringMatch{Classes: []string{"ui-old"}},
	}}
	want := []string{
		hitAt(cgoFile, `"example.com/kit/patterns/accordion"`, "cg.go", "import "+accordion),
		hitAt(cgoFile, "SiteHeader", "cg.go", siteHeaderSym.String()),
		hitAt(cgoFile, "Action", "cg.go", "field "+formAction.String()),
		hitAt(cgoFile, `"ui-old"`, "cg.go", "class ui-old"),
	}
	for _, cgo := range []string{"1", "0"} {
		t.Run("CGO_ENABLED="+cgo, func(t *testing.T) {
			t.Setenv("CGO_ENABLED", cgo)
			res := mustRun(t, newWorkspace(t, defaultKit, map[string]string{
				"main.go": plainMain(),
				"cg.go":   cgoFile,
			}), n)
			if len(res.Unscanned) > 0 {
				t.Errorf("Unscanned = %s, want none", strings.Join(res.Unscanned, "; "))
			}
			wantHits(t, res, n, want...)
		})
	}
}
