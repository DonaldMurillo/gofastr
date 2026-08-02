package static

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func requireBrowser(t *testing.T) {
	t.Helper()
	_ = browserExecutable(t)
}

func browserExecutable(t *testing.T) string {
	t.Helper()
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

func TestBrowserExecutableIsResolvedExplicitly(t *testing.T) {
	path := browserExecutable(t)
	if path == "" {
		t.Fatal("browser executable path is empty")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("browser executable %q is not accessible: %v", path, err)
	}
}
