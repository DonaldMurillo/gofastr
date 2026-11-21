package auditcmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyAuditReportsUnreadableSource(t *testing.T) {
	root := t.TempDir()
	brokenLink := filepath.Join(root, "missing.go")
	if err := os.Symlink(filepath.Join(root, "missing-target"), brokenLink); err != nil {
		t.Fatal(err)
	}
	_, err := auditDeps(root)
	var pathErr *os.PathError
	if !errors.Is(err, os.ErrNotExist) || !errors.As(err, &pathErr) || pathErr.Path != brokenLink {
		t.Fatalf("auditDeps error = %v, want read failure for %s", err, brokenLink)
	}
}

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
