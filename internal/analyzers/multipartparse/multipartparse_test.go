package multipartparse_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/multipartparse"
)

func TestMultipartParse(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), multipartparse.Analyzer, "a", "lib")
}
