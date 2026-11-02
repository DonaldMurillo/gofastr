package iounderlock_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/iounderlock"
)

func TestIOUnderLock(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), iounderlock.Analyzer, "a", "iolib")
}
