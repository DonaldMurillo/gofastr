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
	// The app's go directive must not exceed the toolchain running the
	// test: a newer one makes the load fetch a toolchain, which the
	// hermetic GOPROXY=off refuses. So the threshold moves down instead.
	n := gomodNote("1.26")
	res := mustRun(t, goModApp(t, "go 1.26.1"), n)
	wantHits(t, res, n)
}

func TestGoModToolchainIgnored(t *testing.T) {
	gomod := "go 1.26.3\n\ntoolchain go1.27.0"
	n := gomodNote("1.27")
	res := mustRun(t, goModApp(t, gomod), n)
	// The go directive, not the toolchain line, decides.
	wantHits(t, res, n, "go.mod:"+strconv.Itoa(lineOf("module example.com/app\n\n"+gomod+"\n", "go 1.26.3"))+":0 gomod go 1.26.3 < 1.27")
}

func TestGoVersionPrereleaseBelowRelease(t *testing.T) {
	for _, c := range []struct {
		a, b string
		less bool
	}{
		{"1.26rc1", "1.27", true},
		// Go's own order: a bare language version sits below every
		// release of its minor, prereleases included (1.26 < 1.26rc1
		// < 1.26.0), so a go 1.27rc1 directive is not below 1.27.
		{"1.26", "1.26rc1", true},
		{"1.26rc1", "1.26", false},
		{"1.26beta1", "1.26rc1", true},
		{"1.26rc1", "1.26rc2", true},
		{"1.27rc1", "1.27", false},
		{"1.27rc1", "1.27.0", true},
		{"1.27beta2", "1.27", false},
		{"1.26", "1.27", true},
		{"1.26.3", "1.27", true},
		{"1.27", "1.27", false},
		{"1.27.0", "1.27", false},
		{"1.27", "1.27.0", true},
		{"1.27", "1.27rc1", true},
		{"1.28rc1", "1.27", false},
		{"1.26rc", "1.27", false},
		{"1.26gamma1", "1.27", false},
	} {
		if got := goVersionLess(c.a, c.b); got != c.less {
			t.Errorf("goVersionLess(%q, %q) = %v, want %v", c.a, c.b, got, c.less)
		}
	}
}

func TestGoModPrereleaseBelow(t *testing.T) {
	gomod := "go 1.26rc1"
	n := gomodNote("1.27")
	res := mustRun(t, goModApp(t, gomod), n)
	wantHits(t, res, n, "go.mod:"+strconv.Itoa(lineOf("module example.com/app\n\n"+gomod+"\n", gomod))+":0 gomod go 1.26rc1 < 1.27")
}
