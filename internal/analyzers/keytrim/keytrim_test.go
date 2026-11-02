package keytrim_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/keytrim"
)

func TestKeyTrim(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), keytrim.Analyzer, "a", "spellings")
}
