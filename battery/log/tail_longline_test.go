package log

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTailFileSkipsOverlongLine pins that one log line past the reader's
// 1 MiB cap is skipped, not fatal. The fanout does not cap an entry's
// size (an app can log a large attr), and bufio.Scanner stops the whole
// scan with ErrTooLong on such a line, so log_filter's historical=true
// read returned nothing at all, the error swallowed, for as long as that
// line sat inside the tail window.
func TestTailFileSkipsOverlongLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server.log")
	huge := `{"msg":"huge","blob":"` + strings.Repeat("x", 2<<20) + `"}`
	content := `{"msg":"before"}` + "\n" + huge + "\n" + `{"msg":"after1"}` + "\n" + `{"msg":"after2"}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := tailFileWithWindow(path, 10, int64(len(content)))
	if err != nil {
		t.Fatalf("tail: %v", err)
	}
	var msgs []string
	for _, e := range entries {
		m, _ := e["msg"].(string)
		msgs = append(msgs, m)
	}
	if got := strings.Join(msgs, ","); got != "before,after1,after2" {
		t.Errorf("entries = %q, want before,after1,after2 (the overlong line skipped)", got)
	}
}
