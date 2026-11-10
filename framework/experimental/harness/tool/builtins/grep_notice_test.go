package builtins

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Grep's path is "File or directory to search". A path that is a symlink
// to a directory passes the os.Stat check as a directory, but
// filepath.WalkDir Lstats its root and does not descend a symlink, so
// the walk visited nothing and the caller was told "(no matches)". Grep
// now resolves a linked root before it walks.
func TestGrepSearchesASymlinkedDirectory(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "a.txt"), []byte("needle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	// Control: the real directory finds the line.
	res, err := Grep{}.Run(context.Background(), mustCall(t, map[string]any{"pattern": "needle", "path": real}), nil)
	if err != nil || !strings.Contains(res.Content[0].Text, "a.txt:1: needle") {
		t.Fatalf("control over the real dir: %+v, %v", res, err)
	}
	res, err = Grep{}.Run(context.Background(), mustCall(t, map[string]any{"pattern": "needle", "path": link}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Content[0].Text; !strings.Contains(got, "a.txt:1: needle") && !res.IsError {
		t.Fatalf("Grep over a symlink to a directory holding the match answered %q: neither the match nor an error", got)
	}
}

// Grep reads the first maxGrepFileBytes of a file and says so when it
// stopped there ("results truncated"), so the caller never reads a cut
// search as a complete one. The notice was decided by counting each
// scanned line as len+1 bytes; a CRLF line is len+2, so a CRLF file past
// the cap undercounted by one byte per line and the notice never fired.
// It is now read off the reader.
func TestGrepCRLFFileSaysItWasTruncated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.log")
	build := func(eol string) []byte {
		line := []byte(strings.Repeat("x", 78) + eol)
		var buf bytes.Buffer
		for buf.Len() < maxGrepFileBytes+(1<<20) {
			buf.Write(line)
		}
		buf.WriteString("needle" + eol) // past the cap
		return buf.Bytes()
	}
	grep := func(data []byte) string {
		t.Helper()
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		res, err := Grep{}.Run(context.Background(), mustCall(t, map[string]any{"pattern": "needle", "path": path}), nil)
		if err != nil {
			t.Fatal(err)
		}
		return res.Content[0].Text
	}
	// Control: an LF file past the cap carries the notice.
	if got := grep(build("\n")); !strings.Contains(got, "truncated") {
		t.Fatalf("control (LF file) has no truncation notice: %q", got)
	}
	crlf := build("\r\n")
	if got := grep(crlf); !strings.Contains(got, "needle") && !strings.Contains(got, "truncated") {
		t.Fatalf("a %d-byte CRLF file with the match past the %d-byte cap answered %q: no match and no truncation notice", len(crlf), maxGrepFileBytes, got)
	}
}
