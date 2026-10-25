package verifycmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestMain(m *testing.M) {
	// `go vet` starts this binary as its vettool in the verify tests
	// (runGoVet names os.Executable): hand off before any setup.
	RunAsVettoolIfMarked()
	clitest.Main(m)
}
