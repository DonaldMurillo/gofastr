package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Pins the repo-public default credstore passphrase, found by the
// 2026-09-04 red-probe round; fixed in deriveCredstoreKeyFromEnv
// failing closed with noCredstoreKeyHelp instead of substituting
// "harness-default-passphrase-change-me".
//
// Property: the credential-store key must never be derived from a
//
//	constant that ships in the public repository; with neither
//	machine key nor passphrase configured, key derivation must
//	fail closed.
//
// Surfaces: cmd/gofastr/harness_creds.go::deriveCredstoreKeyFromEnv,
//
//	cmd/gofastr/harness_creds.go::openCredstore,
//	cmd/gofastr/harness.go::runHarness (refusal at boot),
//	cmd/gofastr/harness_mcp.go::runHarnessMCP (refusal at boot;
//	previously substituted the constant silently).
func TestCredsKeyEnvRefusesPublicDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "")
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")

	cfgDir := filepath.Join(home, ".config", "gofastr", "harness")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	key, err := deriveCredstoreKeyFromEnv(cfgDir)
	if err == nil {
		t.Fatalf("SECURITY: [credstore-public-pass] deriveCredstoreKeyFromEnv returned a usable key (%d bytes) with no passphrase and no machine key: it fell back to a repo-public default passphrase, so every stored credential is decryptable from the source tree alone. It must fail closed.", len(key))
	}
	if !strings.Contains(err.Error(), "GOFASTR_HARNESS_PASSPHRASE") || !strings.Contains(err.Error(), "GOFASTR_HARNESS_MACHINE_KEY") {
		t.Fatalf("refusal must name the two ways to provide a key; got %q", err.Error())
	}

	// The same refusal surfaces through openCredstore (every
	// `gofastr harness creds` subcommand): no salt file may be created
	// for a caller that never gets a key.
	if _, err := openCredstore(); err == nil {
		t.Fatal("SECURITY: [credstore-public-pass] openCredstore opened the credential store with no configured key; it must refuse.")
	}
	if _, serr := os.Stat(filepath.Join(cfgDir, "salt")); serr == nil {
		t.Fatal("openCredstore created a salt file for a refused caller")
	}
}

type credpassExitSentinel struct{ code int }

// Pins `gofastr harness mcp` booting on the repo-public constant
// silently, found by the 2026-09-04 red-probe round; fixed in
// runHarnessMCP refusing to boot (exit 1, noCredstoreKeyHelp) when
// neither GOFASTR_HARNESS_PASSPHRASE nor GOFASTR_HARNESS_MACHINE_KEY is
// configured.
//
// Property: a harness that reads and stores provider API keys must
//
//	never derive its at-rest credential-store key from a
//	constant published in the repo; with no operator secret
//	configured it must fail closed exactly like
//	harness.deriveCredstoreKey.
//
// Surfaces: cmd/gofastr/harness_mcp.go::runHarnessMCP (boot refusal),
//
//	sharing noCredstoreKeyHelp with runHarness and
//	deriveCredstoreKeyFromEnv.
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
