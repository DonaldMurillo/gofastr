package chromedptest

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandBootedChromeKeepsLaunchAllowance: a test that boots Chrome
// itself instead of through Context must still raise chromedp's 20s
// DevTools websocket-URL wait. Under parallel package load a cold Chrome
// on a CI runner misses 20s, and the test fails with "websocket url
// timeout reached" while asserting nothing (core-ui/ownstyle, PR #482).
func TestHandBootedChromeKeepsLaunchAllowance(t *testing.T) {
	root := filepath.Join("..", "..")
	var bad []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules", "dist", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if lacksLaunchAllowance(string(src)) {
			bad = append(bad, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range bad {
		t.Errorf("%s boots Chrome with chromedp's 20s launch wait: use chromedptest.Context, or pass chromedp.WSURLReadTimeout", p)
	}
}

// lacksLaunchAllowance reports whether a file boots Chrome more times
// than it raises the launch wait. Counting per call, not per file: a
// file with two allocators and one allowance still has a call on the
// 20s default.
func lacksLaunchAllowance(src string) bool {
	return strings.Count(src, "chromedp.NewExecAllocator(") > strings.Count(src, "chromedp.WSURLReadTimeout(")
}

func TestLaunchAllowanceCountsEachAllocator(t *testing.T) {
	const boot = "chromedp.NewExecAllocator(ctx, opts...)\n"
	const allow = "chromedp.WSURLReadTimeout(90*time.Second),\n"
	for _, tc := range []struct {
		name string
		src  string
		want bool
	}{
		{"no browser", "package x\n", false},
		{"one boot, no allowance", boot, true},
		{"one boot, allowance", allow + boot, false},
		{"two boots, one allowance", allow + boot + boot, true},
		{"two boots, two allowances", allow + boot + allow + boot, false},
	} {
		if got := lacksLaunchAllowance(tc.src); got != tc.want {
			t.Errorf("%s: lacksLaunchAllowance = %v, want %v", tc.name, got, tc.want)
		}
	}
}
