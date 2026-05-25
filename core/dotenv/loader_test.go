package dotenv_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
)

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

func TestApply_ExistingEnvWins(t *testing.T) {
	const k = "DOTENV_TEST_EXISTING"
	t.Setenv(k, "from-env")

	loaded, skipped := dotenv.Apply(map[string]string{k: "from-file"})
	if len(loaded) != 0 {
		t.Errorf("loaded = %v, want empty (existing env should win)", loaded)
	}
	if len(skipped) != 1 || skipped[0] != k {
		t.Errorf("skipped = %v, want [%s]", skipped, k)
	}
	if got := os.Getenv(k); got != "from-env" {
		t.Errorf("env still = %q, want from-env (Apply must not clobber)", got)
	}
}

func TestApply_SetsMissing(t *testing.T) {
	const k = "DOTENV_TEST_NEW"
	os.Unsetenv(k)
	t.Cleanup(func() { os.Unsetenv(k) })

	loaded, skipped := dotenv.Apply(map[string]string{k: "from-file"})
	if len(loaded) != 1 || loaded[0] != k {
		t.Errorf("loaded = %v, want [%s]", loaded, k)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want empty", skipped)
	}
	if got := os.Getenv(k); got != "from-file" {
		t.Errorf("env = %q, want from-file", got)
	}
}

func TestApply_Idempotent(t *testing.T) {
	const k = "DOTENV_TEST_IDEM"
	os.Unsetenv(k)
	t.Cleanup(func() { os.Unsetenv(k) })

	_, _ = dotenv.Apply(map[string]string{k: "v1"})
	loaded, skipped := dotenv.Apply(map[string]string{k: "v2"})
	if len(loaded) != 0 || len(skipped) != 1 {
		t.Errorf("second call should skip; loaded=%v skipped=%v", loaded, skipped)
	}
	if got := os.Getenv(k); got != "v1" {
		t.Errorf("second Apply must not overwrite; got %q", got)
	}
}

func TestLoad_FileMissingIsNonError(t *testing.T) {
	dir := t.TempDir()
	got, err := dotenv.Load(filepath.Join(dir, "nope.env"))
	if err != nil {
		t.Fatalf("missing file should be silent, got err %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("missing file should return empty, got %v", got)
	}
}

func TestLoad_EarlierFileWins(t *testing.T) {
	dir := t.TempDir()
	first := writeTempFile(t, dir, ".env.local", "K=local\nUNIQUE=l\n")
	second := writeTempFile(t, dir, ".env", "K=base\nFROMBASE=b\n")

	got, err := dotenv.Load(first, second)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got["K"] != "local" {
		t.Errorf("K=%q, want local (earlier file wins)", got["K"])
	}
	if got["UNIQUE"] != "l" {
		t.Errorf("UNIQUE missing: %v", got)
	}
	if got["FROMBASE"] != "b" {
		t.Errorf("FROMBASE not picked up from second file: %v", got)
	}
}

func TestLoadAndApply_AppliesAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "a.env", "DOTENV_TEST_LAA_A=alpha\n")
	writeTempFile(t, dir, "b.env", "DOTENV_TEST_LAA_B=beta\n")
	t.Cleanup(func() {
		os.Unsetenv("DOTENV_TEST_LAA_A")
		os.Unsetenv("DOTENV_TEST_LAA_B")
	})

	if err := dotenv.LoadAndApply(filepath.Join(dir, "a.env"), filepath.Join(dir, "b.env")); err != nil {
		t.Fatalf("LoadAndApply: %v", err)
	}
	if got := os.Getenv("DOTENV_TEST_LAA_A"); got != "alpha" {
		t.Errorf("A = %q, want alpha", got)
	}
	if got := os.Getenv("DOTENV_TEST_LAA_B"); got != "beta" {
		t.Errorf("B = %q, want beta", got)
	}
}

func TestLoad_MalformedFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	bad := writeTempFile(t, dir, ".env", "NO_EQUALS_HERE\n")
	if _, err := dotenv.Load(bad); err == nil {
		t.Fatalf("expected parse error for malformed file")
	}
}
