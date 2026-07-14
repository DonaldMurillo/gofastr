package main

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/evals/ui-quality/internal/evalrunner"
)

func TestOutcomeTextKeepsNoncompetitiveTieDiagnostic(t *testing.T) {
	text := outcomeText(evalrunner.Summary{Competitive: false, TiedLeaders: []string{"a", "b"}})
	for _, want := range []string{"diagnostic", "noncompetitive", "cannot establish a passing winner"} {
		if !strings.Contains(text, want) {
			t.Fatalf("noncompetitive tie output missing %q: %s", want, text)
		}
	}
}
