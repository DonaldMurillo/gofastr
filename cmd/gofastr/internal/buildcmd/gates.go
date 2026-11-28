package buildcmd

import (
	"os"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/gates"
)

func buildA11yGate(root string) bool { return gates.BuildA11yGateTo(root, os.Stdout, os.Stderr) }

func buildSandboxGate(root string) bool { return gates.BuildSandboxGateTo(root, os.Stdout, os.Stderr) }
