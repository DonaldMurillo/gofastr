// Package browserpath resolves the Chrome/Chromium/Edge executable that
// chromedp should launch.
//
// chromedp can discover a browser on its own, but its search is not uniform
// across platforms: on Windows it looks for Chrome and Chromium but never a
// discovered Edge installation. Suites that need Edge — the only browser
// present on a stock Windows box — therefore have to resolve the executable
// themselves and pass it via chromedp.ExecPath.
//
// Resolving explicitly on one platform and letting chromedp guess on the
// others is how the macOS gap appeared: a PATH-only lookup finds nothing,
// because Chrome on macOS installs to /Applications/Google Chrome.app and is
// not on PATH. The whole point of this package is that detection and launch
// share one code path on every OS.
package browserpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// pathNames are executables that a browser install may put on $PATH. This is
// the common case on Linux and inside CI containers.
var pathNames = []string{"chrome", "chromium", "chromium-browser", "google-chrome", "google-chrome-stable", "msedge"}

// Find returns the browser executable to launch and whether one was found.
func Find() (string, bool) {
	if p := os.Getenv("GOFASTR_BROWSER_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	for _, name := range pathNames {
		if path, err := exec.LookPath(name); err == nil {
			return path, true
		}
	}
	for _, path := range candidates() {
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	return "", false
}

// candidates returns the standard install locations for the current OS, in
// preference order.
func candidates() []string {
	switch runtime.GOOS {
	case "windows":
		var out []string
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if root == "" {
				continue
			}
			for _, rel := range []string{
				`Google\Chrome\Application\chrome.exe`,
				`Microsoft\Edge\Application\msedge.exe`,
				`Chromium\Application\chrome.exe`,
			} {
				out = append(out, filepath.Join(root, rel))
			}
		}
		return out
	case "darwin":
		// Chrome on macOS ships as an .app bundle and is not on $PATH. Both
		// the system-wide and per-user install locations are checked.
		rels := []string{
			"Google Chrome.app/Contents/MacOS/Google Chrome",
			"Chromium.app/Contents/MacOS/Chromium",
			"Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
		roots := []string{"/Applications"}
		if home, err := os.UserHomeDir(); err == nil {
			roots = append(roots, filepath.Join(home, "Applications"))
		}
		var out []string
		for _, root := range roots {
			for _, rel := range rels {
				out = append(out, filepath.Join(root, rel))
			}
		}
		return out
	default:
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
			"/usr/bin/microsoft-edge",
			"/snap/bin/chromium",
			"/opt/google/chrome/chrome",
		}
	}
}
