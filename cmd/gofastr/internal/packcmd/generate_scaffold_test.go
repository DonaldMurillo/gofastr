package packcmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// generateBaseProject writes baseYml as an app spec, generates the full base
// project from it, and returns the temp dir + app spec path.
func generateBaseProject(t *testing.T, baseYml string) (dir, bp string) {
	t.Helper()
	dir, bp = clitest.AddSetup(t, baseYml)
	clitest.CovT_capStdout(t, func() { appgen.GenerateFromAppSpec(appgen.GenerateOptions{Spec: bp}) })
	return dir, bp
}
