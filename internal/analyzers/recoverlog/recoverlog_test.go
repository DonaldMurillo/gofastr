package recoverlog_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/recoverlog"
)

func TestRecoverLog(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), recoverlog.Analyzer, "a")
}
