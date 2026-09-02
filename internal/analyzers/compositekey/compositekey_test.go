package compositekey_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/compositekey"
)

func TestCompositeKey(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), compositekey.Analyzer, "ck", "ckalt")
}
