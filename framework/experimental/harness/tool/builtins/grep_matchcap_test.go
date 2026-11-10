package builtins

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Grep stops at 1000 matching lines ("cap output") and returns them as
// the whole answer. Every other way it cuts a search short says so in
// the result (a long line, the per-file byte cap, a cancel), because, in
// grep.go's own words, "silently returning the matches found so far
// tells the caller this file held nothing more". At the match cap the
// model got exactly 1000 lines and nothing that said more exist: the
// last 500 matches here, and every file after this one in a walk, were
// dropped without a word. The result now ends with a truncation line.
func TestGrepSaysWhenItStoppedAtTheMatchCap(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte(strings.Repeat("needle\n", 1500)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("needle in the second file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := Grep{}.Run(context.Background(), mustCall(t, map[string]any{"pattern": "needle", "path": dir}), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Paths out, so a word in the temp dir's name cannot pass for a notice.
	got := strings.ReplaceAll(res.Content[0].Text, dir, "")
	lines := strings.Count(got, "needle")
	if lines >= 1501 {
		return // everything came back
	}
	last := got[strings.LastIndex(strings.TrimRight(got, "\n"), "\n")+1:]
	if !strings.Contains(got, "truncated") && !strings.Contains(got, "more") && !strings.Contains(got, "cap") {
		t.Fatalf("Grep returned %d of 1501 matching lines (none from b.txt: %v) with no notice that it stopped; last line: %q",
			lines, !strings.Contains(got, "b.txt"), last)
	}
}
