package poisonedconn_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/poisonedconn"
)

func TestPoisonedConn(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), poisonedconn.Analyzer, "a")
}
