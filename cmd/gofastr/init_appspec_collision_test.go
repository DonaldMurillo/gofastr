package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestInitDoesNotCollideWithAppSpecFilename pins that `gofastr init` writes
// its worktree-isolation config under a dedicated filename, NOT gofastr.yml.
// The app generator (loadAppSpec / `gofastr validate`) claims
// gofastr.yml, so an isolation config written there made the help's own
// example sequence, `gofastr init myapp` then `gofastr validate gofastr.yml`,
// fail with `unknown key "version"`: the isolation config's top-level
// `version: 1` is not an app spec key.
func TestInitDoesNotCollideWithAppSpecFilename(t *testing.T) {
	dir := t.TempDir()
	covT_chdir(t, dir)
	covT_capStdout(t, func() { runInit([]string{"myapp"}) })

	settingsPath := filepath.Join(dir, "myapp", "gofastr.settings.yml")
	bpPath := filepath.Join(dir, "myapp", "gofastr.yml")

	if _, err := os.Stat(bpPath); err == nil {
		t.Fatalf("init wrote gofastr.yml — that filename belongs to the app spec; " +
			"`gofastr validate gofastr.yml` then fails with unknown key \"version\"")
	}
	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("init did not write gofastr.settings.yml: %v", err)
	}

	// A user-authored app spec at gofastr.yml must validate cleanly once init
	// stops claiming the filename.
	if err := os.WriteFile(bpPath, []byte("app:\n  name: Myapp\n  module: example.com/myapp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadAppSpec(bpPath); err != nil {
		t.Fatalf("user app spec at gofastr.yml did not validate after init: %v", err)
	}
}
