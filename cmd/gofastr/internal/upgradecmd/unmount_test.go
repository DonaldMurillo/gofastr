package upgradecmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/core/settings"
)

func TestUpgradeListsUnmountedDiffs(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/app\n")
	clitest.WriteUnmountedSettings(t, dir,
		settings.Unmounted{Source: project.GofastrModule + "/core/yaml", Version: "v0.91.0", Path: "unmounted/core/yaml", Date: "2026-10-19"},
		settings.Unmounted{Source: project.GofastrModule + "/core/textsafe", Version: "dev", Path: "unmounted/core/textsafe", Date: "2026-10-19"})
	out := clitest.CovT_capStdout(t, func() { printUnmountedForUpgrade(dir, "v0.93.0") })
	for _, want := range []string{"upgrade never changes them", project.UnmountedCompareURL("v0.91.0", "v0.93.0"), "copied from a checkout, not a release: compare it with v0.93.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
}
