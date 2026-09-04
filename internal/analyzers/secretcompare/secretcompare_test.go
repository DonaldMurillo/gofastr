package secretcompare_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/secretcompare"
)

func TestSecretCompareRawEqualsOnCredentialFires(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), secretcompare.Analyzer, "a")
}
