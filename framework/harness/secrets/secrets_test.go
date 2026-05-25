package secrets

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFromAndWalkUp(t *testing.T) {
	// Set up: <tmp>/.git, <tmp>/.harness-secrets/env, <tmp>/a/b/c
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	_ = os.MkdirAll(filepath.Join(root, ".harness-secrets"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".harness-secrets", "env"), []byte(`
# comment
ZAI_API_KEY=zai_test_value
OPENROUTER_API_KEY="sk-or-quoted"
EMPTY_VALUE=
`), 0o600)
	_ = os.MkdirAll(filepath.Join(root, "a", "b", "c"), 0o755)

	// Clear env then load.
	t.Setenv("ZAI_API_KEY", "")
	t.Setenv("OPENROUTER_API_KEY", "")
	_ = os.Unsetenv("ZAI_API_KEY")
	_ = os.Unsetenv("OPENROUTER_API_KEY")

	path, err := LoadFrom(filepath.Join(root, "a", "b", "c"))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Base(path) != "env" {
		t.Errorf("loaded path = %q", path)
	}
	if got := os.Getenv("ZAI_API_KEY"); got != "zai_test_value" {
		t.Errorf("ZAI_API_KEY = %q", got)
	}
	if got := os.Getenv("OPENROUTER_API_KEY"); got != "sk-or-quoted" {
		t.Errorf("OPENROUTER_API_KEY = %q", got)
	}
}

func TestEnvWinsOverFile(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".harness-secrets"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".harness-secrets", "env"),
		[]byte("KEY=from-file\n"), 0o600)

	// Set the env var first.
	t.Setenv("KEY", "from-env")

	if _, err := LoadFrom(root); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("KEY"); got != "from-env" {
		t.Errorf("KEY = %q, want from-env (env should win)", got)
	}
}

func TestLoadMissingFileIsNoError(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".git"), 0o755) // stop walk at this dir
	path, err := LoadFrom(root)
	if err != nil {
		t.Fatal(err)
	}
	if path != "" {
		t.Errorf("expected empty path when no file, got %q", path)
	}
}

func TestLoadRejectsMalformed(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, ".harness-secrets"), 0o755)
	_ = os.WriteFile(filepath.Join(root, ".harness-secrets", "env"),
		[]byte("no-equals-here\n"), 0o600)
	_, err := LoadFrom(root)
	if err == nil {
		t.Fatal("expected error for malformed line")
	}
}
