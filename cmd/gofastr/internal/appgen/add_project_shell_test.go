package appgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// addShellProjectSpec adds to addProjectSpec what app.go declares for
// screens to mount through: a nav (appLayout), an app home, a gated
// screen (authPolicy) and the login screen (guestPolicy).
var addShellProjectSpec = strings.Replace(addProjectSpec, "screens:\n",
	"nav:\n  - label: Home\n    href: /home\nscreens:\n  - name: home\n    route: /home\n    layout: app\n  - name: dash\n    route: /dash\n    access:\n      auth: true\n", 1)

// An added screen's file is the one a full generate of the project
// with the screen in it writes: same layout, login route, guest
// redirect and home.
func TestAddedScreenMatchesFullGenerate(t *testing.T) {
	for _, tc := range []struct{ name, screen string }{
		{"join", "  - name: join\n    route: /join\n    body:\n      - kind: signup_form\n"},
		{"secret", "  - name: secret\n    route: /secret\n    access:\n      auth: true\n"},
		{"plain", "  - name: plain\n    route: /plain\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := "screen_" + tc.name + ".go"
			dir, spec := clitest.AddSetup(t, addShellProjectSpec+tc.screen)
			clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: spec}) })
			full := clitest.ReadFile(t, dir, file)
			if added := addFragmentScreen(t, addShellProjectSpec, tc.screen, file); added != full {
				t.Errorf("added %s differs from a full generate.\nadded:\n%s\nfull:\n%s", file, added, full)
			}
		})
	}
}

// A screen added to a project whose app.go declares no authPolicy (no
// gated screen yet) or no marketingLayout (no marketing screen yet) is
// refused: the file would not compile.
func TestAddRefusesUndeclaredShellHelper(t *testing.T) {
	for _, tc := range []struct{ helper, screen string }{
		{"authPolicy", "  - name: secret\n    route: /secret\n    access:\n      auth: true\n"},
		{"marketingLayout", "  - name: secret\n    route: /secret\n    layout: marketing\n"},
	} {
		t.Run(tc.helper, func(t *testing.T) {
			dir, spec := clitest.AddSetup(t, addProjectSpec)
			clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: spec}) })
			fragment := filepath.Join(dir, "fragment.yml")
			clitest.WriteTestFile(t, fragment, "screens:\n"+tc.screen)
			var code int
			out := clitest.CovT_capOutput(t, func() {
				code = clitest.CovT_capExit(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: fragment, add: true}) })
			})
			if code != 1 || !strings.Contains(out, "mounts through "+tc.helper) {
				t.Errorf("add without %s exited %d:\n%s", tc.helper, code, out)
			}
			if clitest.FileExists(dir, "screen_secret.go") {
				t.Error("refused add still wrote screen_secret.go")
			}
		})
	}
}
