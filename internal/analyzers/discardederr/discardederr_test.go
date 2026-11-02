package discardederr_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/DonaldMurillo/gofastr/internal/analyzers/discardederr"
)

func TestDiscardedErr(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), discardederr.Analyzer, "a", "b", "c", "d")
}

// TestDiscardedErrWin32Call pins that a Win32 proc call's always-non-nil
// error (built from GetLastError) is not a swallowed refusal: the
// windows vet run measured 9 such hits in battery/desktop's com and caps
// files. The package type-checks for windows, where syscall.LazyProc
// lives.
func TestDiscardedErrWin32Call(t *testing.T) {
	t.Setenv("GOOS", "windows")
	t.Setenv("CGO_ENABLED", "0")
	analysistest.Run(t, analysistest.TestData(), discardederr.Analyzer, "winproc")
}
