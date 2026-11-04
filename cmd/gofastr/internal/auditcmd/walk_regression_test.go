package auditcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyAuditFollowsLinkedPackages(t *testing.T) {
	root := t.TempDir()
	shared := t.TempDir()
	if err := os.WriteFile(filepath.Join(shared, "register.go"), []byte("package ui\nimport \"github.com/DonaldMurillo/gofastr/core-ui/style\"\nfunc init() { style.Contribute() }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(shared, filepath.Join(root, "ui")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, filepath.Join(shared, "cycle")); err != nil {
		t.Fatal(err)
	}
	findings, err := auditDeps(root)
	if err != nil || len(findings) != 1 {
		t.Fatalf("linked package lost: findings=%v err=%v", findings, err)
	}
	if findings[0].File != filepath.Join("ui", "register.go") {
		t.Fatalf("link's logical package path lost: %+v", findings[0])
	}
}
