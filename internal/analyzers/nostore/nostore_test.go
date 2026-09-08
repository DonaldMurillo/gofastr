package nostore_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/nostore"
)

func TestNoStore(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), nostore.Analyzer, "ns")
}
