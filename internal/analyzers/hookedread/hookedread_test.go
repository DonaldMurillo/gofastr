package hookedread_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/hookedread"
)

func TestHookedRead(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), hookedread.Analyzer,
		"github.com/DonaldMurillo/gofastr/framework/entityui", "seeds")
}
