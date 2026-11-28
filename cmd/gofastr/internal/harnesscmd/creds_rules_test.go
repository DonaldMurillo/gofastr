package harnesscmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/internal/fileperm/filepermtest"
)

func withStdin(t *testing.T, text string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(text); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	saved := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = saved; _ = r.Close() })
}

func TestResolveCredSecret(t *testing.T) {
	t.Run("literal warns", func(t *testing.T) {
		var got string
		var err error
		warn := clitest.CovT_capStderr(t, func() { got, err = resolveCredSecret("sk-literal") })
		if err != nil || got != "sk-literal" || !strings.Contains(warn, "visible to every local process") {
			t.Errorf("got %q, %v, warning %q", got, err, warn)
		}
	})
	t.Run("env before stdin", func(t *testing.T) {
		t.Setenv("GOFASTR_HARNESS_SECRET", "from-env")
		withStdin(t, "from-stdin\n")
		if got, err := resolveCredSecret("-"); err != nil || got != "from-env" {
			t.Errorf("got %q, %v; want the env secret", got, err)
		}
	})
	t.Run("empty refused", func(t *testing.T) {
		t.Setenv("GOFASTR_HARNESS_SECRET", "")
		withStdin(t, "  \n")
		if got, err := resolveCredSecret("-"); err == nil {
			t.Errorf("an empty stdin resolved to %q", got)
		}
	})
}

func TestCredstoreKeyPrefersTheMachineKey(t *testing.T) {
	mk := strings.Repeat("k", 32)
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", mk)
	t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "also-set")
	dir := t.TempDir()
	key, err := deriveCredstoreKeyFromEnv(dir)
	if err != nil || !bytes.Equal(key, []byte(mk)) {
		t.Fatalf("key = %x, %v; want the machine key", key, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "salt")); err == nil {
		t.Error("a salt was written though the machine key was used")
	}
}

func TestCredsSaltIsOwnerOnly(t *testing.T) {
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")
	t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "salt-pass")
	dir := t.TempDir()
	if _, err := deriveCredstoreKeyFromEnv(dir); err != nil {
		t.Fatal(err)
	}
	filepermtest.AssertOwnerOnly(t, filepath.Join(dir, "salt"), 0o600)
}

// The consent prompt reads [y/N]: anything but y or yes, Enter included, refuses.
func TestRepoSecretsDefaultAnswerIsNo(t *testing.T) {
	for _, answer := range []string{"\n", "sure\n", ""} {
		_, xdg := clonedRepo(t)
		var out bytes.Buffer
		loadRepoSecrets(xdg, false, true, strings.NewReader(answer), &out)
		if _, ok := os.LookupEnv("OPENROUTER_API_KEY"); ok {
			t.Errorf("SECURITY: answer %q set the repository's key", answer)
		}
	}
}

func TestCredsListIsSorted(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "list-pass")
	t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")
	t.Setenv("GOFASTR_HARNESS_SECRET", "s")
	clitest.CovT_capOutput(t, func() {
		for _, pa := range [][2]string{{"zai", "b"}, {"openrouter", "z"}, {"zai", "a"}} {
			runHarnessCreds([]string{"add", pa[0], pa[1], "-"})
		}
	})
	out := clitest.CovT_capStdout(t, func() { runHarnessCreds([]string{"list"}) })
	row := func(p, a string) int { return strings.Index(out, fmt.Sprintf("%-20s  %s\n", p, a)) }
	i, j, k := row("openrouter", "z"), row("zai", "a"), row("zai", "b")
	if i < 0 || j < 0 || k < 0 || !(i < j && j < k) {
		t.Errorf("list is not sorted by provider then account:\n%s", out)
	}
}
