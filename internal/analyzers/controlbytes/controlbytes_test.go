package controlbytes_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/controlbytes"
)

func TestControlBytes(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), controlbytes.Analyzer,
		"logsink", "otelsink", "httpsink", "stdiosink", "gateway", "traceprinter",
		"rangesink", "srcapi", "outboundhdr", "pathnorm", "seenguard")
}
