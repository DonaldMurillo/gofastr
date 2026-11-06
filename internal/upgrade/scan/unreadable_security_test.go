package scan

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
)

func TestUnreadableFilesAreReported(t *testing.T) {
	root := newWorkspace(t, defaultKit, map[string]string{"main.go": "package main\nfunc main() {}\n"})
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("retired"), 0o600); err != nil {
		t.Fatal(err)
	}
	names := []string{"app.css", "locale.json", "widget.js", "gofastr.yml", "dev.gofastr.yml"}
	for _, name := range names {
		if err := os.Symlink(outside, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	note := &upgrade.Note{Find: upgrade.Find{
		CSS:    upgrade.CSSMatch{Classes: []string{"retired"}},
		Text:   []upgrade.TextMatch{{Glob: "**/*", Match: regexp.MustCompile("retired")}},
		Config: []upgrade.ConfigMatch{{Key: "retired"}},
	}}
	result := mustRun(t, root, note)
	for _, name := range names {
		found := false
		for _, entry := range result.Unscanned {
			found = found || strings.HasPrefix(entry, name+" (unreadable: ")
		}
		if !found {
			t.Errorf("unreadable %s missing from Unscanned: %v", name, result.Unscanned)
		}
	}
	if len(result.Hits[note]) != 0 {
		t.Fatalf("read outside root: %+v", result.Hits[note])
	}
}

func TestMissingConfigIsNotUnscanned(t *testing.T) {
	note := &upgrade.Note{Find: upgrade.Find{Config: []upgrade.ConfigMatch{{Key: "retired"}}}}
	result := mustRun(t, newWorkspace(t, defaultKit, map[string]string{"main.go": "package main\nfunc main() {}\n"}), note)
	if len(result.Unscanned) != 0 {
		t.Fatalf("missing optional config: %v", result.Unscanned)
	}
}
