package nullarray_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/nullarray"
)

func TestNullArray(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(nullarray.Analyzer), "mcp", "a2a", "user")
}
