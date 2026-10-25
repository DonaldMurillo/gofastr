package packcmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// TestMain gives the package a run-scoped temp root, which holds the
// shared generated app (packTree) and is removed after the run.
func TestMain(m *testing.M) { clitest.Main(m) }
