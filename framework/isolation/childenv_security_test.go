package isolation

// Property (2026-09-05 adversarial pass round 4, promoted from the
// childenv red probe): isolation config discovered from the (possibly
// hostile) project directory may remap ports and DSNs for child
// processes, but must not inject or override process-shaping
// environment — proxy selection, PATH, dynamic loader, HOME — and in
// particular must not beat values the operator exported themselves.
//
// The boundary is enforced twice: loadConfig drops env-shaping names and
// static (non-template) values from the file, and Env never overrides a
// variable present in the env it was handed. The consumer is
// cmd/gofastr/dev.go (runtimeIsolation.Env(os.Environ()) builds the
// dev-server child's entire environment for every rebuild); that child
// holds the operator's session cookies, DB, and .env secrets.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func childEnvOf(t *testing.T, projectDir string) []string {
	t.Helper()
	rt, err := Resolve(projectDir)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !rt.Active() {
		t.Fatal("fixture check: isolation must activate from mode: always")
	}
	return rt.Env(os.Environ())
}

func envLookup(env []string, key string) (string, bool) {
	prefix := key + "="
	// First occurrence wins on Linux glibc; match that reading.
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return strings.TrimPrefix(kv, prefix), true
		}
	}
	return "", false
}

// TestIsolationEnvNoProxyOverride pins that a project-dir isolation file
// cannot plant or override process-shaping env in the `gofastr dev` child.
func TestIsolationEnvNoProxyOverride(t *testing.T) {
	root := t.TempDir()
	yml := `
isolation:
  mode: always
  env:
    HTTPS_PROXY: http://attacker.example:3128
    PATH: /tmp/attacker-bin:/bin:/usr/bin
    LD_PRELOAD: /tmp/attacker.so
    HOME: ` + filepath.Join(root, "fake-home") + `
    REDIS_URL: redis://localhost:{port}
`
	if err := os.WriteFile(filepath.Join(root, "gofastr.isolation.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}

	// The operator's own exports must be untouchable.
	t.Setenv("HTTPS_PROXY", "http://corp-proxy.example:3128")
	t.Setenv("PATH", "/usr/local/bin:/usr/bin:/bin")
	// These are absent from the operator's environment; the file must not
	// introduce them either.
	t.Setenv("LD_PRELOAD", "")
	_ = os.Unsetenv("LD_PRELOAD")
	t.Setenv("HOME", "")
	_ = os.Unsetenv("HOME")
	// Default rewrite posture: the operator did not opt out of rewriting.
	t.Setenv("GOFASTR_ISOLATION_REWRITE", "")
	_ = os.Unsetenv("GOFASTR_ISOLATION_REWRITE")

	child := childEnvOf(t, root)

	// Happy path first: the legitimate use of isolation.env — a port
	// template — must keep expanding, so the guard cannot be dropping the
	// feature wholesale.
	if got, ok := envLookup(child, "REDIS_URL"); !ok || !strings.Contains(got, "redis://localhost:") || strings.Contains(got, "{port}") {
		t.Errorf("REDIS_URL = %q (%v); the port-template contract must keep working", got, ok)
	}

	if got, _ := envLookup(child, "HTTPS_PROXY"); got != "http://corp-proxy.example:3128" {
		t.Errorf("SECURITY: project-dir isolation file overrode the operator's HTTPS_PROXY in the dev-server child env: got %q", got)
	}
	if got, _ := envLookup(child, "PATH"); got != "/usr/local/bin:/usr/bin:/bin" {
		t.Errorf("SECURITY: project-dir isolation file overrode PATH in the dev-server child env: got %q", got)
	}
	if got, ok := envLookup(child, "LD_PRELOAD"); ok && got != "" {
		t.Errorf("SECURITY: project-dir isolation file planted LD_PRELOAD=%q in the dev-server child env", got)
	}
	if got, ok := envLookup(child, "HOME"); ok && got != "" {
		t.Errorf("SECURITY: project-dir isolation file planted HOME=%q in the dev-server child env", got)
	}
}
