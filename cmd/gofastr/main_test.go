package main

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/verifycmd"
)

// Booting a framework app in-process reads the project root's
// gofastr.settings.yml, found by walking up from the package directory.
//
//gofastr:test-reads /gofastr.settings.yml
func TestMain(m *testing.M) {
	// `go vet` starts this binary as its vettool in the verify tests
	// (runGoVet names os.Executable): hand off before any setup.
	verifycmd.RunAsVettoolIfMarked()
	clitest.Main(m)
}
