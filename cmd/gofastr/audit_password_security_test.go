package main

import (
	"os"
	"testing"
)

// Pins: an operator-supplied login credential consumed by the CLI never sits in argv.
// CONTRACT-QUESTION: the usage text advertises `--password PASS`
// (cmd/gofastr/audit_a11y.go runAuditA11y doc) — a documented interface;
// this test asserts the local-observer property needs a stdin/env
// channel (GOFASTR_AUDIT_PASSWORD) rather than argv-only entry. Delete
// this test if the maintainer decides argv entry stays.
// Property: an operator-supplied login credential consumed by the CLI
// never sits in argv.
// Surfaces: cmd/gofastr/audit_a11y.go::parseA11yArgs L108-109 →
// a11yCredentials → chromedp.SetValue (audit_a11y_axe.go:153); no
// stdin/env/prompt path exists.
// Finding (verified by execution): the password is only expressible as
// an argv value, so `gofastr audit a11y --url ... --password` puts the
// login credential in world-readable process state for the whole scan.
// Fix direction: resolve the conventional "-" marker from
// GOFASTR_AUDIT_PASSWORD first, then piped stdin, before falling back
// to the literal argv value (and prefer env/stdin in the usage text).

func TestAuditA11yRedPasswordEnv(t *testing.T) {
	t.Setenv("GOFASTR_AUDIT_PASSWORD", "env-secret")
	opts := parseA11yArgs([]string{
		"--url=http://x",
		"--email=a@b.c",
		"--password=-",
	})
	if opts.badFlag != "" {
		t.Fatalf("setup broken: args rejected as %q", opts.badFlag)
	}
	if opts.password != "env-secret" {
		t.Errorf("SECURITY: [a11y-password-argv] --password resolved to %q with GOFASTR_AUDIT_PASSWORD=env-secret set: the login credential is only expressible as a literal argv value, so ps/procfs expose it for the whole scan. The \"-\" marker must resolve from GOFASTR_AUDIT_PASSWORD (or the command must refuse it).", opts.password)
	}
}

func TestAuditA11yRedPasswordStdin(t *testing.T) {
	t.Setenv("GOFASTR_AUDIT_PASSWORD", "")

	// Piped stdin carries the secret; argv carries only the marker.
	stdinSave := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal("setup broken: pipe:", err)
	}
	if _, err := w.WriteString("piped-secret\n"); err != nil {
		t.Fatal("setup broken: write stdin:", err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() { os.Stdin = stdinSave }()

	opts := parseA11yArgs([]string{
		"--url=http://x",
		"--email=a@b.c",
		"--password=-",
	})
	if opts.badFlag != "" {
		t.Fatalf("setup broken: args rejected as %q", opts.badFlag)
	}
	if opts.password != "piped-secret" {
		t.Errorf("SECURITY: [a11y-password-argv] --password resolved to %q with \"piped-secret\" on stdin and no env override: the login credential is only expressible as a literal argv value (ps/procfs world-readable for the whole scan). The \"-\" marker must resolve from piped stdin (or the command must refuse it).", opts.password)
	}
}
