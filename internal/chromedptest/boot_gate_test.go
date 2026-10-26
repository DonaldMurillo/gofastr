package chromedptest

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHandBootedChromeKeepsLaunchAllowance: code that boots Chrome itself
// instead of through Context must still raise chromedp's two start waits,
// the 20s DevTools websocket-URL read and the 10s websocket dial after it.
// Under parallel package load a cold Chrome on a CI runner misses either:
// the test fails with "websocket url timeout reached" (core-ui/ownstyle,
// PR #482) or "could not dial …: context deadline exceeded"
// (core-ui-runtime-hosts on main, run 40344295152) while asserting
// nothing. It reads every .go file, not only tests: the axe gate, the eval
// runner and the PDF battery boot Chrome from library code. This
// package's own launcher is left out; its tests cover it directly.
//
//gofastr:test-reads *_test.go importers:github.com/chromedp/chromedp
func TestHandBootedChromeKeepsLaunchAllowance(t *testing.T) {
	root := filepath.Join("..", "..")
	var bad []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Hidden and "_" directories are tooling state and scratch
			// packages; Go skips the same ones.
			if path != root && (strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_")) {
				return filepath.SkipDir
			}
			switch d.Name() {
			case "node_modules", "dist", "testdata":
				return filepath.SkipDir
			}
			if path == filepath.Join(root, "internal", "chromedptest") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
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
		t.Errorf("%s boots Chrome on chromedp's short start waits: use chromedptest.Context, or pass chromedp.WSURLReadTimeout to the allocator and chromedp.WithBrowserOption(chromedp.WithDialTimeout(…)) to the context that starts Chrome", p)
	}
}

// lacksLaunchAllowance reports whether a file boots Chrome more times
// than it raises either start wait. Counting per call, not per file: a
// file with two allocators and one allowance still has a call on the
// default.
func lacksLaunchAllowance(src string) bool {
	boots := strings.Count(src, "chromedp.NewExecAllocator(")
	return boots > strings.Count(src, "chromedp.WSURLReadTimeout(") ||
		boots > strings.Count(src, "chromedp.WithDialTimeout(")
}

func TestLaunchAllowanceCountsEachAllocator(t *testing.T) {
	const boot = "chromedp.NewExecAllocator(ctx, opts...)\n"
	const allow = "chromedp.WSURLReadTimeout(90*time.Second),\nchromedp.WithDialTimeout(90*time.Second),\n"
	const urlOnly = "chromedp.WSURLReadTimeout(90*time.Second),\n"
	const dialOnly = "chromedp.WithDialTimeout(90*time.Second),\n"
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
		{"url wait only", urlOnly + boot, true},
		{"dial wait only", dialOnly + boot, true},
	} {
		if got := lacksLaunchAllowance(tc.src); got != tc.want {
			t.Errorf("%s: lacksLaunchAllowance = %v, want %v", tc.name, got, tc.want)
		}
	}
}
