package service

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type logErrorWriter struct{ err error }

func (w logErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestTailFilePropagatesOutputError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(path, []byte("service ready\n"), 0600); err != nil {
		t.Fatal(err)
	}
	want := errors.New("output unavailable")
	if err := tailFile(logErrorWriter{want}, path, 10, false); !errors.Is(err, want) {
		t.Fatalf("tailFile error = %v, want %v", err, want)
	}
}

func TestTailFilePreservesTrailingBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "service.log")
	if err := os.WriteFile(path, []byte("service ready\n\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tailFile(&out, path, 1, false); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "\n" {
		t.Fatalf("last line = %q, want a blank line", got)
	}
}

func TestTrimLogReportsStatFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log")
	if err := os.Symlink(path, path); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}
	if err := trimLog(path); err == nil {
		t.Fatal("trimLog returned success for an unreadable path")
	}
}
