package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/codegen"
)

func TestEnterCodegenProjectDir(t *testing.T) {
	// "." project dir → no-op restore.
	restore, err := enterCodegenProjectDir(codegen.Discovery{ProjectDir: "."})
	if err != nil {
		t.Fatal(err)
	}
	restore()
	// Real subdir.
	dir := t.TempDir()
	sub := filepath.Join(dir, "proj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	restore, err = enterCodegenProjectDir(codegen.Discovery{ProjectDir: sub})
	if err != nil {
		t.Fatalf("enter: %v", err)
	}
	restore()
}

// An extension's error diagnostic must fail the run.
//
// The runner collected codegen.Context.Diagnostics and nothing read them, so an
// extension answering {"diagnostics":[{"severity":"error",...}]} had its
// refusal discarded and `gofastr generate` reported success. A diagnostic is
// the protocol's only way to say "the input is wrong" — a non-zero exit means
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
	// stderr does — an extension does not get to rewrite the operator's window
	// title through a JSON field either.
	if got := scrubTerminalOutput("a\x1b]0;pwned\x07b\x7fc"); got != "a]0;pwnedbc" {
		t.Errorf("scrubTerminalOutput left escape bytes: %q", got)
	}
}
