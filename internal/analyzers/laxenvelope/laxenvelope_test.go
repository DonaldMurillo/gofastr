package laxenvelope_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/laxenvelope"
)

func TestLaxEnvelope(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), laxenvelope.Analyzer,
		"lx",     // round-4 same-package arm
		"xlax",   // round-5 arm a: cross-package via facts (import edge)
		"xraw",   // round-5 arm b: envelope RawMessage one level down
		"xrawok", // round-5 arm b: chokepoint credit, quiet
		"xtop",   // round-5 arm c: CheckTopLevelKeys one level short
	)
}
