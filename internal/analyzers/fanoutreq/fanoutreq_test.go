package fanoutreq_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/fanoutreq"
)

func TestFanoutReq(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), fanoutreq.Analyzer, "a")
}
