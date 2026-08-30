package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

// Property: a repo-local .harness-secrets/env file comes from whatever
// directory the harness was started in — a cloned repo is untrusted
// input. It must deliver provider API keys, but it must never
// influence the variables that choose or derive the key protecting the
// user-global credential store (~/.config/gofastr/harness/creds.enc,
// shared by every repo on the machine).
//
// GOFASTR_HARNESS_MACHINE_KEY is consumed raw: any 32-character value
// plants an attacker-known key (cmd/gofastr harness.go
// machineKeyFromEnv), and GOFASTR_HARNESS_PASSPHRASE feeds DeriveKey.
// LoadRepo() runs at boot inside the working directory, so without a
// key allowlist a hostile clone encrypts the operator's first stored
// credential under a key the attacker knows. Project hooks face the
// same untrusted-directory threat and are gated off by default
// (--allow-project-hooks); the secrets loader is the same boundary
// with no gate, so the minimum fix is refusing GOFASTR_HARNESS_* keys
// from the file — real env stays authoritative via the existing
// env-wins rule.
func TestLoadRepoDropsHarnessKeyVars(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, ".harness-secrets"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".harness-secrets", "env"), []byte(`
ZAI_API_KEY=legit-provider-key
GOFASTR_HARNESS_MACHINE_KEY=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA
GOFASTR_HARNESS_PASSPHRASE=attacker-picked-passphrase
`), 0o600)
	_ = os.MkdirAll(filepath.Join(root, "a", "b"), 0o755)

	// Clear the three vars (existing pattern: t.Setenv snapshots for
	// restore, Unsetenv makes them absent for the load).
	for _, k := range []string{"ZAI_API_KEY", "GOFASTR_HARNESS_MACHINE_KEY", "GOFASTR_HARNESS_PASSPHRASE"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}

	path, err := LoadFrom(filepath.Join(root, "a", "b"))
	if err != nil {
		t.Fatal(err)
	}
	if path == "" {
		t.Fatal("expected the secrets file to be found and loaded")
	}
	// Happy path: the documented contract — API keys are delivered.
	if got := os.Getenv("ZAI_API_KEY"); got != "legit-provider-key" {
		t.Errorf("ZAI_API_KEY = %q, want the file's provider key (delivery contract must hold)", got)
	}
	// The property: key-derivation vars must not be settable from the
	// walked file.
	if got := os.Getenv("GOFASTR_HARNESS_MACHINE_KEY"); got != "" {
		t.Errorf("SECURITY: repo file planted GOFASTR_HARNESS_MACHINE_KEY = %q; it must not be settable from an untrusted directory", got)
	}
	if got := os.Getenv("GOFASTR_HARNESS_PASSPHRASE"); got != "" {
		t.Errorf("SECURITY: repo file planted GOFASTR_HARNESS_PASSPHRASE = %q; it must not be settable from an untrusted directory", got)
	}
}
