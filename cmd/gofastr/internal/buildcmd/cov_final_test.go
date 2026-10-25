package buildcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// ── runBuild with a discovered codegen config ─────────────────────────

func TestRunBuildWithCodegenConfig(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module bcfg\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entDir := filepath.Join(dir, "entities")
	if err := os.MkdirAll(entDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entDir, "posts.json"), []byte(`{"name":"posts","fields":[{"name":"t","type":"string"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// A discovered config triggers the discovery.Found branch in runBuild.
	cfg := "version: 1\ncodegen:\n  output: .gen\n  generators:\n    - name: go/entities\n      source:\n        type: json_dir\n        path: entities\n"
	if err := os.WriteFile(filepath.Join(dir, "gofastr.settings.yml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	out := clitest.CovT_capStdout(t, func() {
		_ = clitest.CovT_capExit(t, func() { RunBuild(nil) })
	})
	if !strings.Contains(out, "Generating") {
		t.Fatalf("expected codegen-config step: %s", out)
	}
}
