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
		s := string(src)
		if strings.Contains(s, "chromedp.NewExecAllocator(") && !strings.Contains(s, "WSURLReadTimeout") {
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
