package rootread_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/rootread"
)

func TestRootRead(t *testing.T) {
	// The O_NOFOLLOW fixture targets Unix; run every case on all hosts.
	t.Setenv("GOOS", "linux")
	t.Setenv("CGO_ENABLED", "0")
	analysistest.Run(t, analysistest.TestData(), rootread.Analyzer, "a", "b")
}
