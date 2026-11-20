package secrets

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

func TestSweepWalkStopsAtRepoRootAndSkipsEmpty(t *testing.T) {
	outer := t.TempDir()
	_ = os.MkdirAll(filepath.Join(outer, ".harness-secrets"), 0o700)
	_ = os.WriteFile(filepath.Join(outer, ".harness-secrets", "env"), []byte("SWEEP_OUTER_API_KEY=x\n"), 0o600)
	repo := filepath.Join(outer, "repo")
	_ = os.MkdirAll(filepath.Join(repo, ".git"), 0o700)
	t.Setenv("SWEEP_OUTER_API_KEY", "")
	os.Unsetenv("SWEEP_OUTER_API_KEY")
	if path, err := LoadFrom(repo); err != nil || path != "" {
		t.Fatalf("LoadFrom crossed the repo root: path=%q err=%v", path, err)
	}
	_ = os.MkdirAll(filepath.Join(repo, ".harness-secrets"), 0o700)
	_ = os.WriteFile(filepath.Join(repo, ".harness-secrets", "env"), []byte("SWEEP_EMPTY_API_KEY=\"\"\n"), 0o600)
	t.Setenv("SWEEP_EMPTY_API_KEY", "")
	os.Unsetenv("SWEEP_EMPTY_API_KEY")
	if _, err := LoadFrom(repo); err != nil {
		t.Fatal(err)
	}
	if _, set := os.LookupEnv("SWEEP_EMPTY_API_KEY"); set {
		t.Fatal("an empty value was set")
	}
}

func TestSweepApprovalsFileIsOwnerOnly(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".git"), 0o700)
	_ = os.MkdirAll(filepath.Join(repo, ".harness-secrets"), 0o700)
	_ = os.WriteFile(filepath.Join(repo, ".harness-secrets", "env"), []byte("SWEEP_ASK_API_KEY=v\n"), 0o600)
	t.Setenv("SWEEP_ASK_API_KEY", "")
	os.Unsetenv("SWEEP_ASK_API_KEY")
	approvals := filepath.Join(t.TempDir(), "cfg", ApprovalsFile)
	if _, err := LoadWithConsent(repo, approvals, false, func(Request) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	filepermtest.AssertOwnerOnly(t, approvals, 0o600)
}

func TestSweepGofastrKeysAndSetEnvNeverComeFromFile(t *testing.T) {
	for _, k := range []string{"GOFASTR_HARNESS_API_KEY", "GOFASTR_TOKEN"} {
		if isProviderCredentialKey(k) {
			t.Errorf("%s qualified as a provider credential", k)
		}
	}
	t.Setenv("SWEEP_LATE_API_KEY", "from-env")
	if err := apply([]pair{{"SWEEP_LATE_API_KEY", "from-file"}}); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("SWEEP_LATE_API_KEY"); got != "from-env" {
		t.Fatalf("apply overwrote a key set after the file was read: %q", got)
	}
}

func TestSweepDeclinedAskSetsAndRemembersNothing(t *testing.T) {
	repo := t.TempDir()
	_ = os.MkdirAll(filepath.Join(repo, ".git"), 0o700)
	_ = os.MkdirAll(filepath.Join(repo, ".harness-secrets"), 0o700)
	_ = os.WriteFile(filepath.Join(repo, ".harness-secrets", "env"), []byte("SWEEP_NO_API_KEY=v\n"), 0o600)
	t.Setenv("SWEEP_NO_API_KEY", "")
	os.Unsetenv("SWEEP_NO_API_KEY")
	approvals := filepath.Join(t.TempDir(), ApprovalsFile)
	_, err := LoadWithConsent(repo, approvals, false, func(Request) (bool, error) { return false, nil })
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("err = %v, want ErrRefused", err)
	}
	if _, set := os.LookupEnv("SWEEP_NO_API_KEY"); set {
		t.Fatal("a declined key was set")
	}
	if _, err := os.Stat(approvals); !os.IsNotExist(err) {
		t.Fatalf("a declined ask was remembered: %v", err)
	}
}
