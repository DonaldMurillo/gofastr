package isolation

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

func TestSQLiteStateDirTightensExistingDir(t *testing.T) {
	r := &Runtime{projectDir: t.TempDir(), id: "loose"}
	dir := filepath.Join(r.projectDir, ".gofastr", "isolation", r.id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := r.sqliteDSN("app.db"); err != nil {
		t.Fatal(err)
	}
	filepermtest.AssertOwnerOnly(t, dir, 0o700)
}

func resolveWith(t *testing.T, yml string) *Runtime {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "gofastr.settings.yml"), []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	rt, err := Resolve(root)
	if err != nil {
		t.Fatal(err)
	}
	return rt
}

func TestSettingsEnvKeepsOnlyTemplatedResourceNames(t *testing.T) {
	t.Setenv(envIsolation, "")
	rt := resolveWith(t, `
isolation:
  mode: always
  env:
    HTTPS_PROXY: http://x:{port}
    https_proxy: http://x:{port}
    GOFASTR_ISOLATION_REWRITE: "{port}"
    STATIC_URL: redis://static
    REDIS_URL: redis://localhost:{port}
`)
	for _, dropped := range []string{"HTTPS_PROXY", "https_proxy", "GOFASTR_ISOLATION_REWRITE", "STATIC_URL"} {
		if _, ok := rt.cfg.Env[dropped]; ok {
			t.Errorf("settings env kept %s", dropped)
		}
	}
	if _, ok := rt.cfg.Env["REDIS_URL"]; !ok {
		t.Error("settings env dropped a templated resource locator")
	}
}

func TestEnvNeverOverridesOperatorValue(t *testing.T) {
	t.Setenv(envIsolation, "")
	rt := resolveWith(t, "isolation:\n  mode: always\n  env:\n    REDIS_URL: redis://localhost:{port}\n")
	got, _ := envLookup(rt.Env([]string{"REDIS_URL=redis://mine"}), "REDIS_URL")
	if got != "redis://mine" {
		t.Fatalf("REDIS_URL = %q, want the operator's value", got)
	}
}

func TestIsolationDisabledSpelling(t *testing.T) {
	t.Setenv(envIsolation, "disabled")
	if resolveWith(t, "isolation:\n  mode: always\n").Active() {
		t.Fatal("GOFASTR_ISOLATION=disabled left isolation on")
	}
}

func TestGitDirWithoutHEADIsNotARepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if kind, _ := gitMarker(dir); kind != gitNone {
		t.Fatalf("gitMarker = %v for a .git directory with no HEAD, want gitNone", kind)
	}
}
