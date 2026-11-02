package authpresence_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/authpresence"
)

func TestAuthPresence(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), authpresence.Analyzer, "a", "lib")
}
