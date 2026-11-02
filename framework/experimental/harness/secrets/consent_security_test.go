package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeRepo(t *testing.T, body string) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".harness-secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".harness-secrets", "env"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return repo
}

func unset(t *testing.T, keys ...string) {
	t.Helper()
	for _, k := range keys {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
}

// An approval covers one directory and the keys it named: another
// repository, or a key the file gained since, asks again. With nobody
// to ask the keys stay unset.
func TestLoadWithConsentScopesApprovals(t *testing.T) {
	approvals := filepath.Join(t.TempDir(), ApprovalsFile)
	a := writeRepo(t, "OPENROUTER_API_KEY=sk-a\n")
	b := writeRepo(t, "OPENROUTER_API_KEY=sk-b\n")
	yes := func(Request) (bool, error) { return true, nil }
	asked := 0
	counting := func(Request) (bool, error) { asked++; return true, nil }

	unset(t, "OPENROUTER_API_KEY", "ZAI_API_KEY")
	if _, err := LoadWithConsent(a, approvals, false, nil); !errors.Is(err, ErrRefused) {
		t.Fatalf("no ask, no approval: err = %v, want ErrRefused", err)
	}
	if _, ok := os.LookupEnv("OPENROUTER_API_KEY"); ok {
		t.Fatal("SECURITY: a refused key was set")
	}
	if _, err := LoadWithConsent(a, approvals, false, yes); err != nil {
		t.Fatal(err)
	}

	unset(t, "OPENROUTER_API_KEY")
	res, err := LoadWithConsent(a, approvals, false, nil)
	if err != nil || !res.Remembered || os.Getenv("OPENROUTER_API_KEY") != "sk-a" {
		t.Fatalf("remembered approval: res %+v, err %v", res, err)
	}

	unset(t, "OPENROUTER_API_KEY")
	if _, err := LoadWithConsent(b, approvals, false, nil); !errors.Is(err, ErrRefused) {
		t.Fatalf("SECURITY: repository a's approval covered repository b: err = %v", err)
	}

	if err := os.WriteFile(filepath.Join(a, ".harness-secrets", "env"), []byte("OPENROUTER_API_KEY=sk-a\nZAI_API_KEY=z\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unset(t, "OPENROUTER_API_KEY")
	if _, err := LoadWithConsent(a, approvals, false, counting); err != nil || asked != 1 {
		t.Fatalf("a new key must ask again: asked %d, err %v", asked, err)
	}
	unset(t, "OPENROUTER_API_KEY", "ZAI_API_KEY")
	if _, err := LoadWithConsent(a, approvals, false, counting); err != nil || asked != 1 {
		t.Fatalf("the widened approval must cover both keys: asked %d, err %v", asked, err)
	}
}

// A key the operator already set is theirs: nothing to ask about.
func TestLoadWithConsentSkipsKeysTheEnvironmentHas(t *testing.T) {
	repo := writeRepo(t, "OPENROUTER_API_KEY=sk-author\n")
	t.Setenv("OPENROUTER_API_KEY", "sk-mine")
	ask := func(Request) (bool, error) { t.Fatal("asked about a key the environment holds"); return false, nil }
	if _, err := LoadWithConsent(repo, "", false, ask); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("OPENROUTER_API_KEY"); got != "sk-mine" {
		t.Fatalf("key = %q, want the operator's", got)
	}
}
