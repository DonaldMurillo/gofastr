package timestampid_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/timestampid"
)

func TestTimestampIDFiresOnWallClockMints(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), timestampid.Analyzer, "a")
}

// TestTimestampIDReadsEveryMintSpelling: Session{ID: …}, var token = …,
// fmt.Sprint and time.Now().Format are the same wall-clock mint; a
// make([]byte, 16) rand buffer silences it and make([]byte, 8) does not.
func TestTimestampIDReadsEveryMintSpelling(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), timestampid.Analyzer, "spellings")
}
