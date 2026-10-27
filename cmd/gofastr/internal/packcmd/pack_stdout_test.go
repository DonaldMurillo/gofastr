package packcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestPackStdoutIsOnlyYAML: `gofastr pack > gofastr.yml` is the
// documented way to pipe an app spec, and an app whose .env carries
// secrets is the case where the output matters most. The secrets
// warning used to print to stdout ahead of the YAML, so the captured
// file began with "  ⚠ pack: ..." and failed to parse.
func TestPackStdoutIsOnlyYAML(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{
			Name:     "Packout",
			Module:   "example.com/packout",
			DBDriver: "postgres",
			DBURL:    "postgres://user:pw@db:5432/packout",
			Auth:     appspec.AppSpecAuth{Enabled: true, JWTSecret: "shhh"},
		},
		Entities: []framework.EntityDeclaration{{
			Name:   "notes",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
	}
	dir := materializeAppSpec(t, bp)
	if _, err := os.Stat(filepath.Join(dir, ".env")); err != nil {
		t.Fatalf("generated app has no .env, so pack recovers no secrets and the warning never fires: %v", err)
	}
	if !secretsInAppSpec(bp) {
		t.Fatal("fixture app spec carries no secrets; the warning path is untested")
	}

	exitCode := 0
	origExit := cli.OsExit
	cli.OsExit = func(c int) { exitCode = c }
	defer func() { cli.OsExit = origExit }()
	out := clitest.CaptureStdout(t, func() { RunPack([]string{dir}) })
	if exitCode != 0 {
		t.Fatalf("runPack exited %d", exitCode)
	}
	if strings.Contains(out, "⚠") || strings.Contains(out, "pack: output contains secrets") {
		t.Fatalf("stdout carries the warning, not just the app spec:\n%s", out)
	}
	captured := filepath.Join(t.TempDir(), "gofastr.yml")
	if err := os.WriteFile(captured, []byte(out), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := appspec.LoadAppSpec(captured); err != nil {
		t.Fatalf("stdout does not parse as an app spec: %v\n%s", err, out)
	}
}

// A caller who meant to write a file and mistyped the flag gets an error,
// not the YAML on stdout and exit 0.
func TestPackRefusesBadOutFlag(t *testing.T) {
	dir := materializeAppSpec(t, appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "Packflag", Module: "example.com/packflag"},
		Entities: []framework.EntityDeclaration{{
			Name:   "notes",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
		}},
	})
	// The app dir comes last in the --output case: a skipped flag would
	// otherwise leave x.yml as the dir and fail for the wrong reason.
	for _, args := range [][]string{{dir, "-o"}, {dir, "--out"}, {"--output", "x.yml", dir}} {
		exitCode := 0
		origExit := cli.OsExit
		cli.OsExit = func(c int) { exitCode = c }
		out := clitest.CaptureStdout(t, func() { RunPack(args) })
		cli.OsExit = origExit
		if exitCode != 1 || strings.Contains(out, "entities:") {
			t.Errorf("pack %q exited %d with stdout:\n%s", args, exitCode, out)
		}
	}
}
