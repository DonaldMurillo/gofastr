package browserpath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// knownInstallLocations is deliberately hardcoded rather than derived from
// candidates(): the oracle must be independent of the code under test, or
// deleting a branch from candidates() would silently turn this test into a
// skip instead of a failure.
func knownInstallLocations(t *testing.T) []string {
	t.Helper()
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "windows":
		var out []string
		for _, root := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LOCALAPPDATA")} {
			if root == "" {
				continue
			}
			out = append(out,
				filepath.Join(root, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(root, `Microsoft\Edge\Application\msedge.exe`),
			)
		}
		return out
	default:
		return []string{
			"/usr/bin/google-chrome",
			"/usr/bin/chromium",
			"/usr/bin/chromium-browser",
		}
	}
}

// TestFindsInstalledBrowser is the regression guard for the macOS gap: a
// PATH-only lookup skipped every browser E2E suite on a stock Mac, because
// Chrome installs to /Applications and is not on $PATH. If a browser is
// installed at a standard location for this OS, Find must return it.
func TestFindsInstalledBrowser(t *testing.T) {
	var installed string
	for _, path := range knownInstallLocations(t) {
		if _, err := os.Stat(path); err == nil {
			installed = path
			break
		}
	}
	if installed == "" {
		t.Skipf("no browser installed at a standard %s location", runtime.GOOS)
	}
	got, ok := Find()
	if !ok {
		t.Fatalf("Find reported no browser, but one is installed at %q", installed)
	}
	if _, err := os.Stat(got); err != nil {
		t.Fatalf("Find returned %q which is not accessible: %v", got, err)
	}
}

func TestEnvOverrideWins(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "browser")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFASTR_BROWSER_PATH", exe)
	got, ok := Find()
	if !ok || got != exe {
		t.Fatalf("Find() = %q, %v; want %q, true", got, ok, exe)
	}
}

func TestEnvOverrideIgnoredWhenMissing(t *testing.T) {
	t.Setenv("GOFASTR_BROWSER_PATH", filepath.Join(t.TempDir(), "nope"))
	// Must not return the bogus path; either a real browser or nothing.
	if got, ok := Find(); ok {
		if _, err := os.Stat(got); err != nil {
			t.Fatalf("Find returned inaccessible path %q", got)
		}
	}
}

func TestCandidatesAreAbsolute(t *testing.T) {
	for _, path := range candidates() {
		if path != "" && !filepath.IsAbs(path) {
			t.Errorf("candidate %q is not absolute", path)
		}
	}
}
