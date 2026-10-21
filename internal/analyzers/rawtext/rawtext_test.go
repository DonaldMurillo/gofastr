package rawtext_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/allow"
	"github.com/DonaldMurillo/gofastr/internal/analyzers/rawtext"
)

func TestRawTextFiresInFrameworkRoots(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(rawtext.Analyzer),
		"github.com/DonaldMurillo/gofastr/core/rt",
		"github.com/DonaldMurillo/gofastr/core/i18n",
		"github.com/DonaldMurillo/gofastr/core/handler",
		"github.com/DonaldMurillo/gofastr/battery/desktop/shellfx",
	)
}

func TestRawTextQuietOutsideRoots(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), allow.Guard(rawtext.Analyzer),
		"github.com/DonaldMurillo/gofastr/examples/app",
	)
}
