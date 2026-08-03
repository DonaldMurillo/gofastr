package uihost

import (
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/browserpath"
)

func requireBrowser(t *testing.T) {
	t.Helper()
	_ = browserExecutable(t)
}

// browserExecutable resolves the browser chromedp launches. See
// internal/browserpath for why resolution is centralised.
func browserExecutable(t *testing.T) string {
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

func TestBrowserExecutableIsResolvedExplicitly(t *testing.T) {
	path := browserExecutable(t)
	if path == "" {
		t.Fatal("browser executable path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("browser executable %q is not accessible: %v", path, err)
	}
}
