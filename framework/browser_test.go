package framework_test

import (
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/browserpath"
)

// browserExecutable returns the exact executable that chromedp must launch.
// Detection and launch must share this path: chromedp's Windows fallback
// searches Chrome/Chromium but not a discovered Edge installation.
// Resolution lives in internal/browserpath so every OS is covered by one
// list — a PATH-only lookup skipped these suites on macOS, where Chrome
// installs to /Applications and is not on $PATH.
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

func requireBrowser(t *testing.T) {
	t.Helper()
	_ = browserExecutable(t)
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
