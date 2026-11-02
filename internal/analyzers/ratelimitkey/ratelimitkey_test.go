package ratelimitkey_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/ratelimitkey"
)

func TestRateLimitKey(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), ratelimitkey.Analyzer, "ratelimit", "a")
}
