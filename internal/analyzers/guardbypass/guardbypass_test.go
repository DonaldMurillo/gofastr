package guardbypass_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/guardbypass"
)

// a imports lib, so lib's exported guards reach a only as facts.
func TestGuardBypass(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), guardbypass.Analyzer, "lib", "a")
}
