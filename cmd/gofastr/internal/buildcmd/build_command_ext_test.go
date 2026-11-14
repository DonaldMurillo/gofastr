package buildcmd

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
)

// build takes no --config, so its refusal of a discovered command
// extension names remedies build and generate actually accept.
func TestBuildExtRefusalOffersRealRemedy(t *testing.T) {
	// The opt-in bypasses the refusal this test exercises: a runner that
	// exports it must not turn the test vacuous.
	t.Setenv("GOFASTR_CODEGEN_ALLOW_COMMANDS", "")
	dir := covT_tinyModule(t)
	clitest.WriteFileAll(t, filepath.Join(dir, "gofastr.settings.yml"), `version: 1
codegen:
  output: gen
  extensions:
    - name: report
      command: ["./tools/report-generator"]
`)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() { RunBuild([]string{"--no-a11y"}) })
	})
	if code != 1 {
		t.Fatalf("exit = %d, want 1:\n%s", code, out)
	}
	if strings.Contains(out, "Run it with --config") {
		t.Errorf("the refusal tells build to take --config, which it refuses:\n%s", out)
	}
	for _, want := range []string{"GOFASTR_CODEGEN_ALLOW_COMMANDS=1", "gofastr generate --config", "--no-generate", "report: ./tools/report-generator"} {
		if !strings.Contains(out, want) {
			t.Errorf("the refusal lacks %q:\n%s", want, out)
		}
	}
}

// The remedy is a command to paste: a config path with a space or a
// shell metacharacter is quoted so the shell passes it as one word.
func TestBuildExtRefusalQuotesPath(t *testing.T) {
	// The remedy is quoted for a POSIX shell, and the test reads it back
	// through one.
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no POSIX sh on PATH")
	}
	for _, path := range []string{"/tmp/my app/gofastr.settings.yml", "/tmp/it's/gofastr.settings.yml", "/tmp/$HOME;x/gofastr.settings.yml", "/tmp/plain/gofastr.settings.yml"} {
		var d codegen.Discovery
		d.Path = path
		d.Config.Codegen.Extensions = []codegen.ExtensionConfig{{Name: "report", Command: []string{"./tools/report"}}}
		msg := buildCommandExtRefusal(d)
		_, rest, ok := strings.Cut(msg, "gofastr generate --config ")
		if !ok {
			t.Fatalf("no generate remedy in:\n%s", msg)
		}
		word, _, _ := strings.Cut(rest, " and then gofastr build")
		out, err := exec.Command("sh", "-c", "printf %s "+word).Output()
		if err != nil || string(out) != path {
			t.Errorf("remedy path %s reads back as %q (%v), want %q", word, out, err, path)
		}
	}
}
