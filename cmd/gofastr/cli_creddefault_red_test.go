//go:build red

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// CONTRACT-QUESTION red: the maintainer must decide whether the documented
// "dev-only default passphrase (warns)" for the credential store stays. The doc
// comments at cmd/gofastr/harness_creds.go::deriveCredstoreKeyFromEnv and
// cmd/gofastr/harness.go::runHarness bless a hard-coded fallback passphrase, but
// that constant is published in the repo, so "encrypted at rest" is
// indistinguishable from plaintext to anyone who obtains the file — and the
// same `gofastr harness` boots that apply it read real provider API keys. If
// the default stays, this probe should be deleted with the rationale recorded
// beside the pinning decision; if it goes, this becomes the permanent
// fail-closed pin.
//
// RED TEST — open finding, 2026-09-04 adversarial pass round 3 (tests-only; no fix applied).
// Family: F15 Secret lifecycle
// Property: the credential-store key must never be derived from a constant that
//           ships in the public repository; with neither machine key nor
//           passphrase configured, key derivation must fail closed.
// Surfaces: cmd/gofastr/harness_creds.go::deriveCredstoreKeyFromEnv,
//           cmd/gofastr/harness_creds.go::openCredstore,
//           cmd/gofastr/harness.go::runHarness (the same fallback inline)
// Finding: deriveCredstoreKeyFromEnv returns a working key derived from
//          "harness-default-passphrase-change-me" when both env vars are unset.
//          Every credential stored afterwards (gofastr harness creds add, or any
//          harness run) is encrypted under a passphrase printed in the public
//          source tree.
// Severity: high — at-rest protection for provider API keys is illusory while
//           the fallback exists.
// Fix direction: delete the fallback and fail closed like
//                harness.deriveCredstoreKey; require the operator to set a
//                passphrase or machine key before any credential is stored.

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
		t.Fatalf("SECURITY: [credstore-public-pass] deriveCredstoreKeyFromEnv returned a usable key (%d bytes) with no passphrase and no machine key: it fell back to the repo-public default passphrase, so every stored credential is decryptable from the source tree alone. It must fail closed.", len(key))
	}
}
