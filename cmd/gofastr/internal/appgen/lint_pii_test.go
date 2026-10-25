package appgen

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// --- gofastr generate: same condition is a prominent warning, not a block.

func TestGeneratePIIWarnsNotFatal(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.WriteFileAll(t, filepath.Join(dir, "bp.yml"), clitest.PiiTripYML)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			GenerateFromAppSpec(GenerateOptions{Spec: filepath.Join(dir, "bp.yml"), outputDir: "gen", dryRun: true})
		})
	})
	if code != -1 {
		t.Fatalf("PII warning must not block generate, got exit %d\n%s", code, out)
	}
	for _, want := range []string{"patients", "owner_field", "Would generate"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output should contain %q:\n%s", want, out)
		}
	}
}
