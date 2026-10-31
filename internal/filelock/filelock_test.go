//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly || windows

package filelock

import (
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// A second open file cannot take the lock until the first is closed.
func TestSecondHolderRefusedUntilClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	a := open(t, path)
	if held, err := TryLock(a); !held || err != nil {
		t.Fatalf("first TryLock = %v, %v", held, err)
	}
	b := open(t, path)
	if held, err := TryLock(b); held || err != nil {
		t.Fatalf("second TryLock = %v, %v; want refused", held, err)
	}
	a.Close()
	if held, err := TryLock(b); !held || err != nil {
		t.Fatalf("TryLock after close = %v, %v", held, err)
	}
}
