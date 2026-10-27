package quoteident_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/quoteident"
)

func TestQuoteIdent(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(quoteident.Analyzer),
		"a", "github.com/DonaldMurillo/gofastr/core/query")
}
