package validatecmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestValidateCmdParseErrorHasLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, "entities:\n  - name: posts\n    bogus_key: 1\n")
	var code int
	out := clitest.CovT_capOutput(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate([]string{path}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	for _, want := range []string{path, "line 3", "bogus_key"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output should contain %q:\n%s", want, out)
		}
	}
}

func TestValidateCmdMissingOrFlagOperandExits2(t *testing.T) {
	for _, args := range [][]string{nil, {"--bogus"}, {"app.yml", "extra.yml"}, {"app.yml", "--bogus"}} {
		var code int
		out := clitest.CovT_capOutput(t, func() {
			code = clitest.CovT_capExit(t, func() { RunValidate(args) })
		})
		if code != 2 || !strings.Contains(out, "Usage") {
			t.Errorf("validate %q: exit %d, want usage error 2:\n%s", args, code, out)
		}
	}
	help := clitest.CovT_capStdout(t, func() { RunValidate([]string{"--help"}) })
	want := "Exit status 2 indicates a missing app spec path, a first argument starting with --, or an extra path or flag."
	if !strings.Contains(strings.Join(strings.Fields(help), " "), want) {
		t.Errorf("help should describe the usage errors observed above:\n%s", help)
	}
}

// `validate gofastr.yml --json` printed text as if --json meant
// something: validate reads no flags. A flag after the path is a usage
// error, exit 2.
func TestValidateCmdRefusesExtraArguments(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yml")
	clitest.WriteFileAll(t, good, "app:\n  name: Demo\n  module: example.com/demo\n")
	for _, args := range [][]string{{good, "--json"}, {good, "--strict"}} {
		var code int
		out := clitest.CovT_capOutput(t, func() {
			code = clitest.CovT_capExit(t, func() { RunValidate(args) })
		})
		if code != 2 {
			t.Errorf("validate %q exited %d, want 2\n%s", args, code, out)
		}
		if strings.Contains(out, "is valid") {
			t.Errorf("validate %q reported valid:\n%s", args, out)
		}
	}
}

func TestValidateCmdModuleMismatchExits1(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.24\n")
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, "app:\n  name: Demo\n  module: example.com/other\n")
	var code int
	out := clitest.CovT_capOutput(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate([]string{path}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "example.com/app") {
		t.Fatalf("output should name the enclosing module:\n%s", out)
	}
}

// `gofastr validate a.yml b.yml` checked a.yml and reported success
// while b.yml was never read. One app spec per run; a second path is a
// usage error (exit 2), not a silent skip.
func TestValidateRefusesSecondPath(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "a.yml")
	clitest.WriteFileAll(t, good, "app:\n  name: Demo\n")
	bad := filepath.Join(dir, "b.yml")
	clitest.WriteFileAll(t, bad, "entities:\n  - name: posts\n    bogus_key: 1\n")
	var code int
	out := clitest.CovT_capOutput(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate([]string{good, bad}) })
	})
	if code != 2 {
		t.Fatalf("validate a.yml b.yml exited %d, want 2\n%s", code, out)
	}
	if !strings.Contains(out, "Usage") || !strings.Contains(out, "b.yml") {
		t.Fatalf("want a usage error naming the extra path, got:\n%s", out)
	}
}
