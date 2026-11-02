//go:build unix

package builtins

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/tool"
)

// Read and Edit refuse a path that is not a regular file, the rule
// Grep's openRegular already applies. Opening a FIFO blocks until a
// writer appears and ignores the turn's context, so a quiet-mode Read
// of one (no prompt: Read is auto-allowed) wedged the turn past
// CancelTurn, and the session's turn slot with it. /dev/tty never
// ends either, and Edit's whole-file read of /dev/zero grows without
// bound.
func TestReadAndEditRefuseNonRegularFiles(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Skip("mkfifo:", err)
	}
	// A symlink to the FIFO is refused too: its target decides.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(fifo, link); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		t    tool.Tool
		args map[string]any
	}{
		{"Read FIFO", Read{}, map[string]any{"path": fifo}},
		{"Read FIFO line-limited", Read{}, map[string]any{"path": fifo, "limit": 1}},
		{"Read symlink to FIFO", Read{}, map[string]any{"path": link}},
		{"Edit FIFO", Edit{}, map[string]any{"path": fifo, "old_string": "a", "new_string": "b"}},
		{"Read directory", Read{}, map[string]any{"path": t.TempDir()}},
	}
	if _, err := os.Stat("/dev/zero"); err == nil {
		cases = append(cases, struct {
			name string
			t    tool.Tool
			args map[string]any
		}{"Edit /dev/zero", Edit{}, map[string]any{"path": "/dev/zero", "old_string": "a", "new_string": "b"}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan *tool.ToolResult, 1)
			go func() {
				res, _ := tc.t.Run(ctx, mustCall(t, tc.args), nil)
				done <- res
			}()
			select {
			case res := <-done:
				if res == nil || !res.IsError {
					t.Fatalf("result = %+v, want an error result", res)
				}
			case <-time.After(3 * time.Second):
				cancel()
				// Unblock a FIFO open so the goroutine can exit.
				if w, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
					_ = w.Close()
				}
				t.Fatalf("%s blocked on a non-regular file", tc.name)
			}
		})
	}
}
