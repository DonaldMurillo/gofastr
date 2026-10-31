package semcov

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/filelock"
)

// flushChildEnv names the coverage root a child test process flushes into.
const flushChildEnv = "SEMCOV_FLUSH_CHILD_ROOT"

// TestFlushChild is the child half of TestFlushWaitsForOtherProcess: it
// records one route and flushes, announcing the flush on stdout first.
func TestFlushChild(t *testing.T) {
	root := os.Getenv(flushChildEnv)
	if root == "" {
		t.Skip("child process of TestFlushWaitsForOtherProcess")
	}
	Reset()
	Enable(root)
	RecordRoute("GET", "/from-child")
	fmt.Println("flushing")
	if err := Flush(); err != nil {
		t.Fatal(err)
	}
}

// Two test binaries flushing together each read the manifest, merge,
// and rename over it. Without a cross-process lock the second rename
// drops whatever the first wrote. Here the parent plays the other
// process: it reads the manifest, lets the child flush, then writes.
func TestFlushWaitsForOtherProcess(t *testing.T) {
	Reset()
	t.Cleanup(Reset)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gofastr"), 0o700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(root, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := filelock.Lock(lock); err != nil {
		t.Fatal(err)
	}
	base := newManifest() // what the parent read: nothing on disk yet

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestFlushChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), flushChildEnv+"="+root)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := bufio.NewScanner(out)
	for lines.Scan() && lines.Text() != "flushing" {
	}
	if err := lines.Err(); err != nil {
		t.Fatal(err)
	}
	go io.Copy(io.Discard, out)
	time.Sleep(200 * time.Millisecond) // an unguarded child finishes its flush in this window

	addTo(base.Routes, "/from-holder", "GET")
	if err := writeTo(root, base); err != nil {
		t.Fatal(err)
	}
	lock.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child: %v", err)
	}

	m, err := Read(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"/from-holder", "/from-child"} {
		if _, ok := m.Routes[want]; !ok {
			t.Errorf("%q lost: %v", want, m.Routes)
		}
	}
}
