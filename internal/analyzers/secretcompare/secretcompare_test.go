package secretcompare_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/secretcompare"
)

func TestSecretCompareRawEqualsOnCredentialFires(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), secretcompare.Analyzer, "a")
}

// TestSecretCompareEquivalentSpellingsFire pins the spellings of the
// same short-circuit compare the 2026-11-02 guard bash found the rule
// blind to: bytes.Equal / strings.Compare on a credential pair (the
// classic HMAC timing bug), a switch whose tag is the credential, a
// string(sig) conversion, a parenthesized operand, and a type parameter
// constrained to ~string.
func TestSecretCompareEquivalentSpellingsFire(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), secretcompare.Analyzer, "variants")
}
