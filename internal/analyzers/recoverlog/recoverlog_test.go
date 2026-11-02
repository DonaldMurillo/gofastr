package recoverlog_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/recoverlog"
)

func TestRecoverLog(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), recoverlog.Analyzer, "a")
}

// TestRecoverLogEverySink pins the sinks the doc names that no fixture
// held (package-level log.Printf, slog.Log), the std log Fatal/Panic
// families, and a direct os.Stderr.WriteString.
func TestRecoverLogEverySink(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), recoverlog.Analyzer, "spellings")
}
