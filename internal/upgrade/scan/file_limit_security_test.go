package scan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func TestOversizedFilesAreUnscanned(t *testing.T) {
	root := newWorkspace(t, defaultKit, map[string]string{"main.go": "package main\nfunc main() {}\n"})
	names := []string{"archive.jsonl", "app.css", "gofastr.yml"}
	for _, name := range names {
		file, err := os.OpenFile(filepath.Join(root, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := file.Truncate(8<<20 + 1); err != nil {
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	note := &upgrade.Note{Find: upgrade.Find{
		CSS:    upgrade.CSSMatch{Classes: []string{"retired"}},
		Text:   []upgrade.TextMatch{{Glob: "**/*.jsonl", Match: regexp.MustCompile("retired")}},
		Config: []upgrade.ConfigMatch{{Key: "retired"}},
	}}
	result := mustRun(t, root, note)
	for _, name := range names {
		found := false
		for _, entry := range result.Unscanned {
			found = found || strings.HasPrefix(entry, name+" (") && strings.Contains(entry, "scan limit")
		}
		if !found {
			t.Errorf("oversized %s missing from Unscanned: %v", name, result.Unscanned)
		}
	}
}

func TestTextAtScanLimitIsRead(t *testing.T) {
	note := &upgrade.Note{Find: upgrade.Find{Text: []upgrade.TextMatch{{Glob: "**/*.jsonl", Match: regexp.MustCompile("retired")}}}}
	text := strings.Repeat(" ", 8<<20-len("retired")) + "retired"
	root := newWorkspace(t, defaultKit, map[string]string{"main.go": "package main\nfunc main() {}\n", "archive.jsonl": text})
	result := mustRun(t, root, note)
	wantHits(t, result, note, "archive.jsonl:1:1 text retired")
	if len(result.Unscanned) != 0 {
		t.Fatal(result.Unscanned)
	}
}
