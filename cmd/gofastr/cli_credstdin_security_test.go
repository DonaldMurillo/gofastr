package main

import (
	"os"
	"testing"
)

// Pins: a credential accepted for encrypted at-rest storage never sits in process argv (ps/procfs world-readable).
// CONTRACT-QUESTION: the doc documents argv entry
// (`gofastr harness creds add <provider> <account> <secret>`), so the
// interface is documented — this test asserts the store's own
// local-observer threat model (the one cli_credstore_security_test.go
// argues for the at-rest key) requires a non-argv channel for the live
// secret. Delete this test if the maintainer decides argv entry stays.
// Property: a credential accepted for encrypted at-rest storage never
// sits in process argv (ps/procfs world-readable).
// Surfaces: cmd/gofastr/harness_creds.go::runHarnessCredsAdd L56-76 —
// argv positionals only, no stdin/env path; `echo REAL | gofastr
// harness creds add prov acct -` succeeds and stores literal "-".
// Finding (verified by execution): the only accepted entry shape is the
// plaintext secret in argv, so every stored credential transits
// world-readable process state on its way into the encrypted store.
// Fix direction: resolve the conventional "-" marker from stdin (and/or
// accept GOFASTR_HARNESS_SECRET), refusing rather than storing the
// literal marker.

func TestHarnessCredsRedStdinSecret(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "red-round5-passphrase")
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")

	// The real secret rides stdin; argv carries only the conventional
	// "-" marker (the shape every credential-aware CLI supports).
	stdinSave := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal("setup broken: pipe:", err)
	}
	if _, err := w.WriteString("REAL-SECRET\n"); err != nil {
		t.Fatal("setup broken: write stdin:", err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() { os.Stdin = stdinSave }()

	// runHarnessCredsAdd exits via osExit on failure; keep a sentinel
	// panic from escaping the call (same pattern as
	// TestHarnessMCPRefusesPublicCredstorePass).
	exitSave := osExit
	osExit = func(c int) { panic(credpassExitSentinel{c}) }
	defer func() { osExit = exitSave }()
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				if _, ok := rec.(credpassExitSentinel); !ok {
					panic(rec)
				}
			}
		}()
		runHarnessCredsAdd([]string{"redprov", "redacct", "-"})
	}()

	// Read the stored value back through the same store the command
	// writes, and require the stdin secret — not the argv marker.
	store, err := openCredstore()
	if err != nil {
		t.Fatalf("setup broken: openCredstore: %v", err)
	}
	got, err := store.Get("redprov", "redacct")
	if err != nil {
		t.Fatalf("setup broken: read back redprov/redacct: %v", err)
	}
	if got != "REAL-SECRET" {
		t.Errorf("SECURITY: [credstore-argv-secret] `harness creds add redprov redacct -` with REAL-SECRET piped on stdin stored %q: the only accepted entry shape is the plaintext secret in argv, where ps/procfs expose it to every local process — the same local-observer the encrypted store exists to defend against. \"-\" must resolve from stdin (or the command must refuse it), never store the marker.", got)
	}
}
