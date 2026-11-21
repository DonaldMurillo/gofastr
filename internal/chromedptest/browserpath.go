package chromedptest

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/browserpath"
)

// BrowserPath resolves the executable the test Chrome launches, skipping
// the test under -short or when no browser is installed. Detection and
// launch must share one path on every OS; see internal/browserpath for
// why chromedp's own discovery is not enough. It replaces the
// browserExecutable helpers framework/uie2e, framework/uihost and
// framework/static each carried.
func BrowserPath(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	path, ok := browserpath.Find()
	if !ok {
		t.Skip("browser E2E requires Chrome, Chromium, or Edge")
	}
	return path
}
