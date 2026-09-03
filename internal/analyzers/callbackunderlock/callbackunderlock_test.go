package callbackunderlock_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/callbackunderlock"
)

func TestCallbackUnderLock(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), callbackunderlock.Analyzer,
		"registry", "subhub", "pipeline", "golaunch", "wnparam", "logfield")
}
