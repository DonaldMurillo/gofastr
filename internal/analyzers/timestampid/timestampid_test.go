package timestampid_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/timestampid"
)

func TestTimestampIDFiresOnWallClockMints(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), timestampid.Analyzer, "a")
}
