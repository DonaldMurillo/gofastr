package embedskip_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/embedskip"
)

func TestEmbedSkip(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(embedskip.Analyzer), "a")
}
