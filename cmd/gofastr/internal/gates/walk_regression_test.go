package gates

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestGatesExcludedRootStillScanned(t *testing.T) {
	for _, name := range []string{"build", "dist", "vendor", ".source"} {
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), name)
			if err := os.Mkdir(root, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "screen.go"), []byte(clitest.BadScreen), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "screen.ui.go"), []byte("package app\nfunc render() { go func(){}() }\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			findings, err := AuditA11y(root)
			if err != nil || len(findings) == 0 {
				t.Errorf("a11y root skipped: findings=%v err=%v", findings, err)
			}
			sandbox, err := auditSandbox(root)
			if err != nil || len(sandbox) == 0 {
				t.Errorf("sandbox root skipped: findings=%v err=%v", sandbox, err)
			}
		})
	}
}

func TestGatesIgnoreDisappearedFiles(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"gone.go", "gone.ui.go"} {
		if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AuditA11y(root); err != nil {
		t.Errorf("a11y failed on vanished source: %v", err)
	}
	if _, err := auditSandbox(root); err != nil {
		t.Errorf("sandbox failed on vanished source: %v", err)
	}
	missing := filepath.Join(root, "not-a-root")
	if _, err := AuditA11y(missing); err == nil {
		t.Error("missing a11y root accepted")
	}
	if _, err := auditSandbox(missing); err == nil {
		t.Error("missing sandbox root accepted")
	}
}
