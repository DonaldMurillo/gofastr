package unboundedbody_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/unboundedbody"
)

func TestUnboundedBody(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), unboundedbody.Analyzer, "a", "capped", "helper", "parseform")
}
