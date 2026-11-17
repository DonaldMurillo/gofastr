package appgen

import (
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
)

func TestEnterCodegenProjectDir(t *testing.T) {
	start, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	clitest.CovT_chdir(t, start)
	wd := func() string { d, _ := os.Getwd(); return d }
	restore, err := enterCodegenProjectDir(codegen.Discovery{ProjectDir: "."})
	if err != nil || wd() != start {
		t.Fatalf(`"." must not move cwd: err %v, cwd %s`, err, wd())
	}
	restore()
	sub := filepath.Join(start, "proj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	restore, err = enterCodegenProjectDir(codegen.Discovery{ProjectDir: sub})
	if err != nil || wd() != sub {
		t.Fatalf("enter: err %v, cwd %s, want %s", err, wd(), sub)
	}
	restore()
	if wd() != start {
		t.Fatalf("restore left cwd at %s, want %s", wd(), start)
	}
	if _, err := enterCodegenProjectDir(codegen.Discovery{ProjectDir: filepath.Join(start, "missing")}); err == nil || wd() != start {
		t.Fatalf("missing dir: err %v, cwd %s", err, wd())
	}
}

// An extension's error diagnostic must fail the run.
//
// The runner collected codegen.Context.Diagnostics and nothing read them, so an
// extension answering {"diagnostics":[{"severity":"error",...}]} had its
// refusal discarded and `gofastr generate` reported success. A diagnostic is
// the protocol's only way to say "the input is wrong"; a non-zero exit means
// something else ("the process broke"), so dropping it converted a refusal into
// a silent pass.
func TestErrorDiagnosticFailsGeneration(t *testing.T) {
	if reportCodegenDiagnostics(nil) {
		t.Error("no diagnostics must not fail the run")
	}
	warnOnly := []codegen.Diagnostic{{Severity: "warning", Message: "heads up"}}
	if reportCodegenDiagnostics(warnOnly) {
		t.Error("a warning diagnostic must not fail the run")
	}
	withError := []codegen.Diagnostic{
		{Severity: "warning", Message: "heads up"},
		{Severity: "error", Message: "schema is invalid"},
	}
	if !reportCodegenDiagnostics(withError) {
		t.Error("an error diagnostic must fail the run")
	}
	// Diagnostics reach a terminal, so they get the same scrub the child's raw
	// stderr does; an extension does not get to rewrite the operator's window
	// title through a JSON field either.
	if got := textsafe.ScrubTerminal("a\x1b]0;pwned\x07b\x7fc"); got != "a]0;pwnedbc" {
		t.Errorf("scrubTerminalOutput left escape bytes: %q", got)
	}
	// The scrub happens in the cli helpers the diagnostics print through.
	hostile := []codegen.Diagnostic{
		{Severity: "error", Message: "a\x1b]0;pwned\x07b"},
		{Severity: "warning", Message: "c‮d"},
		{Severity: "info", Message: "e\u009b2Jf"},
	}
	out := clitest.CovT_capStdout(t, func() { reportCodegenDiagnostics(hostile) })
	if !strings.Contains(out, "a]0;pwnedb") || strings.ContainsAny(out, "\x1b\x07‮\u009b") {
		t.Errorf("diagnostics printed terminal escapes live: %q", out)
	}
}
