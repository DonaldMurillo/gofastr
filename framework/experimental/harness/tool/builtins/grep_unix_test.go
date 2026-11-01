//go:build unix

package builtins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Grep skips FIFOs and devices: opening a FIFO blocks until a writer
// shows up, and a quiet-mode Grep over /dev wedged the turn that way.
func TestGrepSkipsFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o600); err != nil {
		t.Skip("mkfifo:", err)
	}
	done := make(chan *tool.ToolResult, 1)
	go func() {
		res, _ := Grep{}.Run(context.Background(), mustCall(t, map[string]any{"pattern": "needle", "path": dir}), nil)
		done <- res
	}()
	select {
	case res := <-done:
		if res == nil || !strings.Contains(res.Content[0].Text, "a.txt:1: needle") {
			t.Fatalf("result = %+v", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Grep blocked on a FIFO")
	}
}

// A FIFO named as the search path is refused, not opened.
func TestGrepRefusesFIFOPath(t *testing.T) {
	p := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Skip("mkfifo:", err)
	}
	done := make(chan *tool.ToolResult, 1)
	go func() {
		res, _ := Grep{}.Run(context.Background(), mustCall(t, map[string]any{"pattern": "x", "path": p}), nil)
		done <- res
	}()
	select {
	case res := <-done:
		if res == nil || !res.IsError {
			t.Fatalf("result = %+v, want an error", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Grep blocked on a FIFO path")
	}
}
