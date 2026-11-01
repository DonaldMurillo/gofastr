package builtins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cancelAfterCtx reports cancellation from its second Err call on, so a
// test can cancel a Grep after it has started on a file.
type cancelAfterCtx struct {
	context.Context
	calls int
}

func (c *cancelAfterCtx) Err() error {
	c.calls++
	if c.calls > 1 {
		return context.Canceled
	}
	return nil
}

// A cancelled turn ends a running Grep instead of leaving it to finish
// the walk.
func TestGrepStopsOnCancel(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Grep{}.Run(ctx, mustCall(t, map[string]any{"pattern": "needle", "path": dir}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError || !strings.Contains(res.Content[0].Text, "cancelled") {
		t.Fatalf("result = %+v, want a cancelled error", res)
	}
	if strings.Contains(res.Content[0].Text, "a.txt:1:") {
		t.Fatalf("searched after the cancel: %s", res.Content[0].Text)
	}
}

// The scan loop checks the context too, so one huge file cannot hold
// the turn after a cancel.
func TestGrepStopsMidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("needle\n", 5000)), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := &cancelAfterCtx{Context: context.Background()}
	res, err := Grep{}.Run(ctx, mustCall(t, map[string]any{"pattern": "needle", "path": path}), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Content[0].Text
	if !res.IsError || !strings.Contains(got, "cancelled") {
		t.Fatalf("result = %.200s, want a cancelled error", got)
	}
	if strings.Contains(got, "big.txt:2:") {
		t.Fatalf("scan went on after the cancel: %.200s", got)
	}
}

// Grep reads at most maxGrepFileBytes of one file and says so.
func TestGrepCapsBytesPerFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	body := strings.Repeat("xxxxxxx\n", maxGrepFileBytes/8) + "needle\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Grep{}.Run(context.Background(), mustCall(t, map[string]any{"pattern": "needle|truncated", "path": path}), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := res.Content[0].Text
	if strings.Contains(got, ": needle") {
		t.Fatalf("read past the cap: %.200s", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("no truncation note: %.200s", got)
	}
}
