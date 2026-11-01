//go:build unix

package context

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A FIFO named AGENTS.md is skipped; opening it would block the turn
// until something writes to it.
func TestReadSkipsFIFO(t *testing.T) {
	dir := repoDir(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "AGENTS.md"), 0o600); err != nil {
		t.Skip("mkfifo:", err)
	}
	done := make(chan []Section, 1)
	go func() {
		r := &Reader{WorkingDir: dir, Sources: []string{"AGENTS.md"}}
		s, _ := r.Read()
		done <- s
	}()
	select {
	case s := <-done:
		if len(s) != 0 {
			t.Fatalf("FIFO read as a section: %+v", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Read blocked on a FIFO")
	}
}
