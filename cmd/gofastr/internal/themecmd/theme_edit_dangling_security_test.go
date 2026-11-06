package themecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestThemeEditRefusesDanglingOutput(t *testing.T) {
	for _, name := range []string{"theme.go", "palette.go"} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			output := filepath.Join(directory, name)
			if err := os.Symlink("missing.go", output); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			var code int
			message := clitest.CovT_capStdout(t, func() {
				code = clitest.CovT_capExit(t, func() {
					runThemeEdit([]string{"--out=" + output, "--no-open", "--addr=no-port-here"})
				})
			})
			if code != 1 || !strings.Contains(message, "already exists") || !strings.Contains(message, "--force") {
				t.Fatalf("exit %d, want overwrite refusal before binding:\n%s", code, message)
			}
		})
	}
}
