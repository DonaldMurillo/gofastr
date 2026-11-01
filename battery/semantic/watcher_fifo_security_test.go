//go:build unix

package semantic

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A regular file swapped for a FIFO between the walk's stat and the open
// blocked the read-only open until a writer appeared, and a cancelled
// context did not unblock it. The open does not wait, and the
// regular-file check after it skips the pipe.
func TestReadCappedSkipsSwappedFIFO(t *testing.T) {
	dir := t.TempDir()
	pipe := filepath.Join(dir, "pipe.md")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	w := NewWatcher(nil, WatchOptions{PollInterval: -1})
	type result struct {
		skip bool
		err  error
	}
	done := make(chan result, 1)
	go func() {
		_, skip, err := w.readCapped(r, "pipe.md")
		done <- result{skip, err}
	}()
	select {
	case res := <-done:
		if res.err != nil || !res.skip {
			t.Fatalf("skip %v err %v; want the pipe skipped", res.skip, res.err)
		}
	case <-time.After(2 * time.Second):
		// Release the blocked open so the goroutine ends.
		if f, err := os.OpenFile(pipe, os.O_WRONLY, 0); err == nil {
			_ = f.Close()
		}
		<-done
		t.Fatal("the open blocked on a FIFO until a writer appeared")
	}
}
