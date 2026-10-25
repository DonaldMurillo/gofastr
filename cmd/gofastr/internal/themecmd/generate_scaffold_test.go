package themecmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// ---- `gofastr theme init`: the scaffolded theme.go must compile ----

// TestRunThemeInitScaffoldCompiles: the theme starter was only
// contains-linted (theme_test.go walks the const and touches symbols);
// a field rename inside the literal was caught by the hand-maintained
// required-list, not by a compiler. Generate the file for real and
// build it against the repo.
func TestRunThemeInitScaffoldCompiles(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteAddGoMod(t, dir, "example.com/themegen")
	clitest.CovT_chdir(t, dir)
	clitest.CovT_capStdout(t, func() { runThemeInit(nil) })
	clitest.BuildGenerated(t, dir)
}
