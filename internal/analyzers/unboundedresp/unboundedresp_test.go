package unboundedresp

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestUnboundedResp(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "a", "helper")
}
