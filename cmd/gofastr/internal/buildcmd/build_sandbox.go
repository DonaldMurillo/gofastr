package buildcmd

import (
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/gates"
)

// buildSandboxGate runs the .ui.go hydration-sandbox lint for `gofastr build`
// and `gofastr dev`, and reports whether the build may proceed. The sandbox
// rules (check.LintFile: no goroutines, channels, type switches, or imports
// outside the safe allow-list) exist because client-hydrated .ui.go files
// cannot use them: a `go func(){}` or `import "os"` in a .ui.go file breaks
// hydration at runtime. Unlike the accessibility gate this is a correctness
// floor, so it is NOT bound to --no-a11y and runs on every build/dev rebuild.
func buildSandboxGate(root string) bool { return gates.BuildSandboxGateTo(root, os.Stdout, os.Stderr) }
