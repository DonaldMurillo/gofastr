package mcpclient

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// writePeerStub writes a stub MCP peer from its two implementations: the
// Go program goSource compiled to <goName>.exe on Windows, the /bin/sh
// script written as shName on Unix (mirroring the stub_unix_test.go /
// stub_windows_test.go split). label names the stub in build diagnostics.
func writePeerStub(t *testing.T, goName, shName, label, goSource, script string) string {
	t.Helper()
	dir := t.TempDir()
	if runtime.GOOS == "windows" {
		src := filepath.Join(dir, goName+".go")
		bin := filepath.Join(dir, goName+".exe")
		if err := os.WriteFile(src, []byte(goSource), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("go", "build", "-o", bin, src).CombinedOutput(); err != nil {
			t.Fatalf("setup broken: build %s: %v\n%s", label, err, out)
		}
		return bin
	}
	path := filepath.Join(dir, shName)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
