package service

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// `logs -f` follows the log path, not the file it first opened: after
// uninstall --purge removes the log and a reinstall writes a new one, it
// prints the new file's lines, not the deleted one's again.
func TestFollowReopensRecreatedLog(t *testing.T) {
	path := filepath.Join(shortTempDir(t), "service.log")
	if err := os.WriteFile(path, []byte("old one\nold two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out lockedBuf
	go func() { _ = tailFile(&out, path, 10, true) }() // follows until the test binary exits
	time.Sleep(700 * time.Millisecond)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if err := os.WriteFile(path, []byte("new line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(out.String(), "new line") {
		if time.Now().After(deadline) {
			t.Fatalf("follow never printed the recreated log; got:\n%s", out.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	if n := strings.Count(out.String(), "old one"); n != 1 {
		t.Fatalf("the deleted log's lines printed %d times, want once:\n%s", n, out.String())
	}
}
