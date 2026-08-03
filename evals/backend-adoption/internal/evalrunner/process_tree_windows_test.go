//go:build windows

package evalrunner

import (
	"path/filepath"
	"testing"
)

func TestTaskkillExecutableUsesSystemRoot(t *testing.T) {
	t.Setenv("SystemRoot", `C:\Windows`)
	if got, want := taskkillExecutable(), filepath.Join(`C:\Windows`, "System32", "taskkill.exe"); got != want {
		t.Fatalf("taskkill executable = %q, want %q", got, want)
	}
}
