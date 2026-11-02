package recoverwrite_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/recoverwrite"
)

func TestRecoverWrite(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(recoverwrite.Analyzer), "lib", "a")
}
