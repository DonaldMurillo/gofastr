//go:build darwin || linux || freebsd || netbsd || openbsd || dragonfly || windows

package filelock

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

// Lock waits for the holder to let go, then takes the lock.
func TestLockWaitsForTheHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	a := open(t, path)
	if held, err := TryLock(a); !held || err != nil {
		t.Fatalf("first TryLock = %v, %v", held, err)
	}
	b := open(t, path)
	done := make(chan error, 1)
	go func() { done <- Lock(b) }()
	select {
	case err := <-done:
		t.Fatalf("Lock returned %v while another file held the lock", err)
	case <-time.After(150 * time.Millisecond):
	}
	a.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Lock = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Lock still waiting after the holder closed")
	}
	if held, err := TryLock(open(t, path)); held || err != nil {
		t.Fatalf("TryLock after Lock = %v, %v; want refused", held, err)
	}
}
