package log

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toolFilter's historical arm used to drop tailFile's error on the
// floor (`if err == nil && …`), so a read that failed — a symlinked
// log path, an unreadable file — came back as an empty history with no
// sign anything was wrong (#657 parked bugs).
func TestLogFilterSurfacesHistoricalReadError(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.log")
	if err := os.WriteFile(target, []byte(`{"msg":"x"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	p := &Plugin{ring: NewRingSink(4), resolvedFilePath: link}
	_, err := p.toolFilter(context.Background(), map[string]any{"historical": true})
	if err == nil {
		t.Fatal("log_filter swallowed the historical read error for a symlinked log path")
	}
	if !strings.Contains(err.Error(), "refuses symlink") {
		t.Fatalf("error = %v, want the tail refusal to reach the tool caller", err)
	}
}
