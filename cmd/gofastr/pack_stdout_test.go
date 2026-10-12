package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// TestPackStdoutIsOnlyYAML: `gofastr pack > gofastr.yml` is the
// documented way to pipe a blueprint, and an app whose .env carries
// secrets is the case where the output matters most. The secrets
// warning used to print to stdout ahead of the YAML, so the captured
// file began with "  ⚠ pack: ..." and failed to parse.
func TestPackStdoutIsOnlyYAML(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{
			Name:     "Packout",
			Module:   "example.com/packout",
			DBDriver: "postgres",
			DBURL:    "postgres://user:pw@db:5432/packout",
			Auth:     BlueprintAuth{Enabled: true, JWTSecret: "shhh"},
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "notes",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
	}
	dir := materializeBlueprint(t, bp)
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Fatalf("generated app has no .env, so pack recovers no secrets and the warning never fires: %v", err)
	}
	if !secretsInBlueprint(bp) {
		t.Fatal("fixture blueprint carries no secrets; the warning path is untested")
	}

	exitCode := 0
	origExit := osExit
	osExit = func(c int) { exitCode = c }
	defer func() { osExit = origExit }()
	out := captureStdout(t, func() { runPack([]string{dir}) })
	if exitCode != 0 {
		t.Fatalf("runPack exited %d", exitCode)
	}
	if strings.Contains(out, "⚠") || strings.Contains(out, "pack: output contains secrets") {
		t.Fatalf("stdout carries the warning, not just the blueprint:\n%s", out)
	}
	captured := filepath.Join(t.TempDir(), "gofastr.yml")
	if err := os.WriteFile(captured, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadBlueprint(captured); err != nil {
		t.Fatalf("stdout does not parse as a blueprint: %v\n%s", err, out)
	}
}
