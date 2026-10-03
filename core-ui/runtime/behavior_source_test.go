package runtime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
)

// Source-level contract gates for registered behaviours, over the
// packages that own them rather than the registrations this test binary
// happens to link (docs/spec-behavior-registry.md "The module
// contract"). framework/ui's behavior_test.go covers the two action
// adapters through the registry; this one walks the tree, so a module
// registered by a package core-ui/runtime cannot import is held to the
// same rule.

// loadedFlagAssign matches the loadedModules ASSIGNMENT (an equals
// sign follows the identifier), never the identifier alone: every
// module's early-return guard READS loadedModules before any listener,
// so a bare identifier match is satisfied by the guard and holds
// nothing — the first mutation run of this gate proved that the hard
// way (flag moved to the end of toggleaction.js, gate still green).
// It matches the per-module WRITE (`loadedModules[NAME] = true`,
// `(… loadedModules … {}).name = true`, minified `!0` included), not
// the table initialisation `loadedModules = loadedModules || {}`,
// which a module may put at the top while its own flag stays at the
// end; the fixture test below proves the difference.
var loadedFlagAssign = regexp.MustCompile(`loadedModules[^;]*?(\[[^\]]*\]|\.[A-Za-z_$][\w$]*)\s*=\s*(true|!0)`)

// flagBeforeInstall reports whether src writes its loaded flag before
// its first addEventListener; "" when it does, otherwise the reason.
func flagBeforeInstall(src string) string {
	am := loadedFlagAssign.FindStringIndex(src)
	if am == nil {
		return "never sets its loaded flag"
	}
	if inst := strings.Index(src, "addEventListener"); inst != -1 && inst < am[0] {
		return "installs a listener before setting its loaded flag"
	}
	return ""
}

// The gate's own fixtures: an init of the table at the top with the
// flag at the end must fail, the flag first must pass, and a
// minified `!0` counts as true.
func TestFlagBeforeInstallReadsTheWriteNotTheInit(t *testing.T) {
	cases := []struct {
		name, src string
		ok        bool
	}{
		{"init at top, flag at end", `NS.loadedModules = NS.loadedModules || {}; document.addEventListener('click', f); NS.loadedModules[NAME] = true;`, false},
		{"flag first", `NS.loadedModules = NS.loadedModules || {}; NS.loadedModules[NAME] = true; document.addEventListener('click', f);`, true},
		{"one-liner adapter form", `(NS.loadedModules = NS.loadedModules || {})[NAME] = true; document.addEventListener('click', f);`, true},
		{"kernel dotted form, minified", `(window.__gofastr.loadedModules||={}).reveal=!0;document.addEventListener("click",f)`, true},
		{"no flag at all", `document.addEventListener('click', f);`, false},
	}
	for _, c := range cases {
		if got := flagBeforeInstall(c.src) == ""; got != c.ok {
			t.Errorf("%s: pass=%v, want %v", c.name, got, c.ok)
		}
	}
}

// A registered module sets its loadedModules flag before it installs
// anything: the loader resolves on registration, and a script that
// failed halfway with its flag unset has its cached promise dropped, so
// a later loadModule re-executes the file and installs every listener
// of the first pass a second time. The check runs on the minified
// source so a comment naming either token cannot satisfy it.
func TestRegisteredBehaviorsSetLoadedFlagBeforeInstalling(t *testing.T) {
	files, err := check.RegisteredBehaviorSources(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("registered behaviours: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no registered behaviour sources found under the repo root: the walk is broken, not the tree empty")
	}
	for _, f := range files {
		// The walk returns paths relative to this package's directory
		// (../../examples/...); strip the walk root so the report reads
		// as a tree-shaped path.
		rel := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(f)), "../../")
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		src := string(raw)
		if !nominify() {
			src = minify.Minify(src)
		}
		if why := flagBeforeInstall(src); why != "" {
			t.Errorf("%s %s: a retry re-executes the file and would install it twice", rel, why)
		}
	}
}
