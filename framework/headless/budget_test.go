package headless

import (
	"bytes"
	"compress/gzip"
	"os"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
)

// behaviorGzipSize minifies the module source the way serving does
// and returns its gzip size at the default level.
func behaviorGzipSize(t *testing.T, file string) int {
	t.Helper()
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.DefaultCompression)
	if err != nil {
		t.Fatalf("gzip writer: %v", err)
	}
	if _, err := w.Write([]byte(minify.Minify(string(raw)))); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Len()
}

// TestBehaviorModuleSizeBudgets holds headless modules to their tighter ceilings.
// The runtime treegate owns the default goal and shared module overrides.
func TestBehaviorModuleSizeBudgets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		file   string
		budget int
	}{
		{TreeBehaviorName, "tree.js", 1536},
		{MultiSelectBehaviorName, "multiselect.js", 1024},
		{CopyBehaviorName, "copy.js", 1024},
		// The grid keyboard, range selection and focus restore share state.
		{DatePickerBehaviorName, "datepicker.js", 2944},
		// TestCancelledEnterKeepsCombobox pins prior Enter prevention: 2524 measured + 2.
		{ComboboxBehaviorName, "combobox.js", 2526},
	} {
		got := behaviorGzipSize(t, tc.file)
		t.Logf("%s (%s) gzip = %d bytes (budget %d)", tc.name, tc.file, got, tc.budget)
		if got > tc.budget {
			t.Errorf("%s gzip = %d bytes — exceeds %d byte budget: split or shrink the module", tc.name, got, tc.budget)
		}
	}
}
