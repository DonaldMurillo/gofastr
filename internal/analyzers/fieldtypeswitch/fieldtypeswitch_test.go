package fieldtypeswitch_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/fieldtypeswitch"
)

func TestFieldTypeSwitch(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), fieldtypeswitch.Analyzer, "c")
}
