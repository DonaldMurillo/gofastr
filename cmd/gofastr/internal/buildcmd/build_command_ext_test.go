package buildcmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// build takes no --config, so its refusal of a discovered command
// extension names remedies build and generate actually accept.
func TestBuildExtRefusalOffersRealRemedy(t *testing.T) {
	dir := covT_tinyModule(t)
	clitest.WriteFileAll(t, filepath.Join(dir, "gofastr.settings.yml"), `version: 1
codegen:
  output: gen
  extensions:
    - name: report
      command: ["./tools/report-generator"]
`)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunBuild([]string{"--no-a11y"}) })
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s", code, out)
	}
	if strings.Contains(out, "Run it with --config") {
		t.Errorf("the refusal tells build to take --config, which it refuses:\n%s", out)
	}
	for _, want := range []string{"GOFASTR_CODEGEN_ALLOW_COMMANDS=1", "gofastr generate --config", "--no-generate", "report: ./tools/report-generator"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal lacks %q:\n%s", want, out)
		}
	}
}
