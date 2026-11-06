package contracts

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPassPreservesChangedRealPathBeforeDirectoryAlias(t *testing.T) {
	for _, alias := range []string{"a", "a/nested"} {
		t.Run(alias, func(t *testing.T) {
			root := t.TempDir()
			writeTemp(t, root, "pkg/bad.go", "package app\n")
			for _, args := range [][]string{{"init", "-q"}, {"add", "pkg/bad.go"}, {"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-qm", "base"}} {
				command := exec.Command("git", args...)
				command.Dir = root
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v\n%s", args, err, output)
				}
			}
			if err := os.MkdirAll(filepath.Dir(filepath.Join(root, alias)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(root, "pkg"), filepath.Join(root, alias)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			writeTemp(t, root, "pkg/bad.go", "package app\nvar changed = true\n")
			pass, err := NewPass(root, nil)
			if err != nil {
				t.Fatal(err)
			}
			report := &Report{}
			for _, source := range pass.Files() {
				report.Diagnostics = append(report.Diagnostics, diag("GOFASTR1002", source.Rel))
			}
			changed, err := ChangedFiles(root, "")
			if err != nil {
				t.Fatal(err)
			}
			if dropped := report.RestrictTo(changed); dropped != 0 || len(report.Diagnostics) != 1 || report.Diagnostics[0].File != "pkg/bad.go" {
				t.Fatalf("files=%v changed=%v report=%+v dropped=%d", pass.Files(), changed, report.Diagnostics, dropped)
			}
		})
	}
}
