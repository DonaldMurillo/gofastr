//go:build red

package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F15 Secret lifecycle
// Property: a harness that reads and stores provider API keys must never derive its
//           at-rest credential-store key from a constant published in the repo; with no
//           operator secret configured it must fail closed exactly like
//           harness.deriveCredstoreKey ("need CredstorePass or MachineKey").
// Surfaces: cmd/gofastr/harness_mcp.go::runHarnessMCP (silent default),
//           cmd/gofastr/harness.go::runHarness (default + warn),
//           cmd/gofastr/harness_creds.go::deriveCredstoreKeyFromEnv (default + warn)
// Finding: `gofastr harness mcp` — the wiring its own doc comment advertises to Claude
//          Code / Codex, where real provider keys get stored — boots a fully working
//          harness with CredstorePass silently set to the repo-public constant
//          "harness-default-passphrase-change-me" when GOFASTR_HARNESS_PASSPHRASE is
//          unset. No osExit, no warning. Every credential the store then holds is
//          decryptable by anyone who obtains the file, because the passphrase is in
//          the public source tree.
// Severity: high — provider API keys at rest are protected by a public constant, and
//           the MCP path (the advertised always-on wiring) applies it silently.
// Fix direction: runHarnessMCP must not substitute a default: with neither passphrase
//                nor machine key it should exit with the same error deriveCredstoreKey
//                returns. The documented dev-only default in runHarness/creds needs a
//                maintainer decision (see the sibling CONTRACT-QUESTION probe).

type credpassExitSentinel struct{ code int }

func TestHarnessMCPRefusesPublicCredstorePass(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No passphrase, no machine key: machineKeyFromEnv("") returns (nil, nil).
	t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "")
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")

	// Serve() reads os.Stdin; hand it a closed pipe so an EOF ends the MCP
	// loop promptly instead of blocking the test on the real stdin.
	stdinSave := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdin = r
	defer func() { os.Stdin = stdinSave }()

	// Capture stderr and the exit request.
	stderrSave := os.Stderr
	sr, sw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = sw
	defer func() { os.Stderr = stderrSave }()

	exitSave := osExit
	var exitCode int
	var exited bool
	osExit = func(c int) { exitCode, exited = c, true; panic(credpassExitSentinel{c}) }
	defer func() { osExit = exitSave }()

	var stderrText strings.Builder
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stderrText, sr)
		close(done)
	}()

	func() {
		defer func() {
			if rec := recover(); rec != nil {
				if _, ok := rec.(credpassExitSentinel); !ok {
					panic(rec)
				}
			}
		}()
		runHarnessMCP(nil)
	}()

	_ = sw.Close()
	<-done
	_ = sr.Close()
	out := stderrText.String()

	if !exited {
		t.Fatalf("SECURITY: [credstore-public-pass] `gofastr harness mcp` booted a working harness with no GOFASTR_HARNESS_PASSPHRASE and no machine key: the credential store is keyed by the repo-public default passphrase. It must refuse to boot (fail closed like harness.deriveCredstoreKey) instead. stderr so far: %q", out)
	}
	if exitCode == 0 {
		t.Fatalf("SECURITY: [credstore-public-pass] refusal exited 0; want non-zero")
	}
	if !strings.Contains(out, "PASSPHRASE") && !strings.Contains(strings.ToLower(out), "passphrase") {
		t.Fatalf("SECURITY: [credstore-public-pass] refusal must name the missing passphrase/machine-key configuration; stderr: %q", out)
	}
}
