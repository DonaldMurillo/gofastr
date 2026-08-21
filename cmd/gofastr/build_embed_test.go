package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func embedGateModule(t *testing.T) (dir, output string) {
	t.Helper()
	dir = t.TempDir()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// Track the repo's own `go` directive rather than hardcoding one. This
	// module replaces gofastr with the checkout, and Go refuses to build a
	// module whose directive is below a dependency's.
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	devPkgWrite(t, filepath.Join(dir, "go.mod"), fmt.Sprintf(`module embedgatetest

go %s

require github.com/DonaldMurillo/gofastr v0.0.0

replace github.com/DonaldMurillo/gofastr => %s
`, goVersion, repoRoot))
	devPkgWrite(t, filepath.Join(dir, "main.go"), `package main

import (
	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

type badComp struct{}

func (badComp) Render() render.HTML { return render.HTML("<p>save</p>") }

func (*badComp) Actions() {
	component.On("save", func(_ *component.ComponentContext) {},
		component.WithClientJS("G.serverAction(\"save\")"))
}

var surfaces = []fembed.Surface{{
	Name:   "reports",
	Screen: app.NewScreen("/reports", &badComp{}),
}}

func main() { _ = surfaces }
`)
	output = filepath.Join(dir, "server")
	return dir, output
}

func TestBuildEmbedGateFailsOnFinding(t *testing.T) {
	dir, _ := embedGateModule(t)
	covT_chdir(t, dir)
	if buildEmbedGate("./...", false) {
		t.Fatal("buildEmbedGate returned success for an embeddable server action")
	}
}

// --allow-unverified-embeds downgrades the unresolved notes, never a provable
// violation: the escape hatch exists for surfaces static analysis cannot read,
// not for ones it read and found guilty.
func TestAllowUnverifiedStillFailsOnFinding(t *testing.T) {
	dir, _ := embedGateModule(t)
	covT_chdir(t, dir)
	if buildEmbedGate("./...", true) {
		t.Fatal("--allow-unverified-embeds let a proven server action through")
	}
}

func TestRunBuildCallsEmbedGate(t *testing.T) {
	dir, output := embedGateModule(t)
	covT_chdir(t, dir)
	code := covT_capExit(t, func() {
		covT_capStdout(t, func() {
			runBuild([]string{"--no-generate", "--no-a11y", "--output=" + output})
		})
	})
	if code != 1 {
		t.Fatalf("runBuild exit = %d, want 1 from embed gate", code)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("runBuild produced a binary after embed gate failure: %v", err)
	}
}
