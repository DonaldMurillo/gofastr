package main

import (
	"os"

	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/suite"
)

// vettoolEnv marks a gofastr process that `go vet` started as its
// -vettool. `gofastr verify` in an app sets it and names its own binary,
// so the app's code gets the same type-aware analyzers this repo's CI
// runs (internal/analyzers/suite), at the version of the CLI the
// developer has, with no second binary to build or install.
const vettoolEnv = "GOFASTR_VETTOOL"

// runAsVettoolIfMarked hands the process to the analyzer driver when
// `go vet` started it as a vettool. The driver speaks vet's protocol
// (-V=full, -flags, a .cfg per package) and exits; it never returns.
func runAsVettoolIfMarked() {
	if os.Getenv(vettoolEnv) == "1" {
		multichecker.Main(suite.Analyzers()...)
	}
}

// selfVettool returns the vet arguments and environment that run the
// suite through this binary, or nothing when the binary cannot name its
// own path (plain `go vet` then runs instead).
func selfVettool() (args, env []string) {
	self, err := os.Executable()
	if err != nil {
		return nil, nil
	}
	return []string{"-vettool=" + self}, append(os.Environ(), vettoolEnv+"=1")
}
