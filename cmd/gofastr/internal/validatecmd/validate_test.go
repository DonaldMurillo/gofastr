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
	out := clitest.CovT_capStdout(t, func() {
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

func TestValidateCmdNoArgsExits1(t *testing.T) {
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate(nil) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d", code)
	}
	if !strings.Contains(out, "Usage") {
		t.Fatalf("expected usage text, got:\n%s", out)
	}
}

func TestValidateCmdModuleMismatchExits1(t *testing.T) {
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module example.com/app\n\ngo 1.24\n")
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteFileAll(t, path, "app:\n  name: Demo\n  module: example.com/other\n")
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunValidate([]string{path}) })
	})
	if code != 1 {
		t.Fatalf("want exit 1, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "example.com/app") {
		t.Fatalf("output should name the enclosing module:\n%s", out)
	}
}
