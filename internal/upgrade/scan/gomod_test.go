package scan

import (
	"strconv"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

// goModApp writes an app whose go.mod carries the given directives.
func goModApp(t *testing.T, directives string) string {
	return newWorkspace(t, defaultKit, map[string]string{
		"main.go": "package main\n\nfunc main() {}\n",
		"go.mod":  "module example.com/app\n\n" + directives + "\n\nrequire example.com/kit v0.1.0\n\nreplace example.com/kit => ../kit\n",
	})
}

func gomodNote(below string) *upgrade.Note {
	return &upgrade.Note{Find: upgrade.Find{GoMod: &upgrade.GoModMatch{GoBelow: below}}}
}

func TestGoModBelow(t *testing.T) {
	gomod := "go 1.26.3"
	n := gomodNote("1.27")
	res := mustRun(t, goModApp(t, gomod), n)
	wantHits(t, res, n, "go.mod:"+strconv.Itoa(lineOf("module example.com/app\n\n"+gomod+"\n", gomod))+":0 gomod go 1.26.3 < 1.27")
}

func TestGoModEqual(t *testing.T) {
	for _, gomod := range []string{"go 1.27", "go 1.27.0"} {
		n := gomodNote("1.27")
		res := mustRun(t, goModApp(t, gomod), n)
		wantHits(t, res, n)
	}
}

func TestGoModAbove(t *testing.T) {
	n := gomodNote("1.27")
	res := mustRun(t, goModApp(t, "go 1.27.1"), n)
	wantHits(t, res, n)
}

func TestGoModToolchainIgnored(t *testing.T) {
	gomod := "go 1.26.3\n\ntoolchain go1.27.0"
	n := gomodNote("1.27")
	res := mustRun(t, goModApp(t, gomod), n)
	// The go directive, not the toolchain line, decides.
	wantHits(t, res, n, "go.mod:"+strconv.Itoa(lineOf("module example.com/app\n\n"+gomod+"\n", "go 1.26.3"))+":0 gomod go 1.26.3 < 1.27")
}
