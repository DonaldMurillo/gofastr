package framework_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// browserExecutable returns the exact executable that chromedp must launch.
// Detection and launch must share this path: chromedp's Windows fallback
// searches Chrome/Chromium but not a discovered Edge installation.
func browserExecutable(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	for _, name := range []string{"chrome", "chromium", "chromium-browser", "google-chrome", "msedge"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	if runtime.GOOS == "windows" {
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			for _, rel := range []string{
				`Google\Chrome\Application\chrome.exe`,
				`Microsoft\Edge\Application\msedge.exe`,
				`Chromium\Application\chrome.exe`,
			} {
				if root != "" {
					path := filepath.Join(root, rel)
					if _, err := os.Stat(path); err == nil {
						return path
					}
				}
			}
		}
	}
	t.Skip("browser E2E requires Chrome, Chromium, or Edge")
	return ""
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
