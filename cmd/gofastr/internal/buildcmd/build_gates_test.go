package buildcmd

//gofastr:test-reads cmd/check-embed/testdata/

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestNoA11yStillRunsTheSandboxGate(t *testing.T) {
	dir := covT_tinyModule(t)
	if err := os.WriteFile(filepath.Join(dir, "view.ui.go"), []byte("package main\n\nfunc view() { go func() {}() }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "server")
	var code int
	stdout := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunBuild([]string{"--no-generate", "--no-a11y", "-o=" + out}) })
	})
	if code != 1 || !strings.Contains(stdout, ".ui.go sandbox lint") {
		t.Fatalf("--no-a11y skipped the sandbox gate: exit %d\n%s", code, stdout)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("a build that failed its sandbox gate wrote a binary: %v", err)
	}
}

func TestBuildEmbedGateFailsOnlyOnBlockingNotes(t *testing.T) {
	clitest.CovT_chdir(t, filepath.Join("..", "..", "..", "check-embed", "testdata"))
	for _, c := range []struct {
		pattern         string
		allowUnverified bool
		pass            bool
	}{
		{"./src/unresolved", false, true},
		{"./src/renderbuiltxpkg", false, false},
		{"./src/renderbuiltxpkg", true, true},
	} {
		var pass bool
		stderr := clitest.CovT_capStderr(t, func() { pass = buildEmbedGate(c.pattern, c.allowUnverified) })
		if pass != c.pass {
			t.Errorf("%s allowUnverified=%v: pass=%v, want %v\n%s", c.pattern, c.allowUnverified, pass, c.pass, stderr)
		}
		if !strings.Contains(stderr, "check-embed: ") {
			t.Errorf("%s: the gate did not print its notes\n%s", c.pattern, stderr)
		}
	}
}

func TestBuildContractsGatePassesWarnings(t *testing.T) {
	dir := writeContractFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "gofastr.settings.yml"), []byte("contracts:\n  rules:\n    GOFASTR1002: warn\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var pass bool
	stdout := clitest.CovT_capStdout(t, func() { pass = buildContractsGate(dir) })
	if !pass {
		t.Fatalf("a warning stopped the build\n%s", stdout)
	}
	if !strings.Contains(stdout, "GOFASTR1002") {
		t.Fatalf("the gate passed a warning without printing it\n%s", stdout)
	}
}
