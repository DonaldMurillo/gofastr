package appgen

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestMain runs the package under clitest.Main's temp root, which also
// holds the vettool clitest.RepoVettool builds once for the package.
func TestMain(m *testing.M) {
	clitest.Main(m)
}
