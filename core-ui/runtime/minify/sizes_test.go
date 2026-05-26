package minify

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestReportSizes prints raw + gz sizes for the runtime corpus before
// and after minification. Not an assertion — it exists so a `go test
// -run TestReportSizes -v` invocation surfaces the actual shrink we're
// shipping. Useful when tightening budget_test.go overrides.
func TestReportSizes(t *testing.T) {
	if testing.Short() {
		t.Skip("size report skipped in short mode")
	}
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("repoRoot: %v", err)
	}
	paths := []string{filepath.Join(root, "core-ui/runtime/runtime.js")}
	srcDir := filepath.Join(root, "core-ui/runtime/src")
	entries, _ := os.ReadDir(srcDir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".js" {
			paths = append(paths, filepath.Join(srcDir, e.Name()))
		}
	}

	var totalRaw, totalMinRaw, totalGz, totalMinGz int
	t.Logf("%-30s  %8s -> %8s  (%5s)   %8s -> %8s  (%5s)",
		"file", "raw", "min", "Δ", "gz", "min-gz", "Δ")
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		min := Minify(string(raw))
		rgz := gz(raw)
		mgz := gz([]byte(min))
		totalRaw += len(raw)
		totalMinRaw += len(min)
		totalGz += rgz
		totalMinGz += mgz
		t.Logf("%-30s  %8d -> %8d  (%4.1f%%)   %8d -> %8d  (%4.1f%%)",
			filepath.Base(p),
			len(raw), len(min), pct(len(raw), len(min)),
			rgz, mgz, pct(rgz, mgz))
	}
	t.Logf("%-30s  %8d -> %8d  (%4.1f%%)   %8d -> %8d  (%4.1f%%)",
		"TOTAL",
		totalRaw, totalMinRaw, pct(totalRaw, totalMinRaw),
		totalGz, totalMinGz, pct(totalGz, totalMinGz))
	if totalMinRaw >= totalRaw {
		t.Errorf("corpus did not shrink in aggregate")
	}
	_ = fmt.Sprintf // keep fmt referenced if logging changes
}

func pct(before, after int) float64 {
	if before == 0 {
		return 0
	}
	return 100 * float64(before-after) / float64(before)
}

func gz(b []byte) int {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Len()
}
