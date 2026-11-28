package buildcmd

import (
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/gates"
)

// buildA11yGate runs the static accessibility lint for `gofastr build`
// and reports whether the build may proceed. On violations it prints
// the guided report so the developer knows exactly what to fix.
func buildA11yGate(root string) bool { return gates.BuildA11yGateTo(root, os.Stdout, os.Stderr) }
