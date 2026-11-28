package harnesscmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	xharness "github.com/DonaldMurillo/gofastr/framework/experimental/harness"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/logging"
	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/profile"
)

// A credential `gofastr harness creds add` stores is the one the harness the
// CLI boots reads: same directory, salt, key derivation and file.
func TestCredsAddIsReadByTheHarness(t *testing.T) {
	for _, tc := range []struct {
		name, xdg string
	}{
		{"home config", ""},
		{"XDG_CONFIG_HOME", t.TempDir()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", tc.xdg)
			t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "parity-pass")
			t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")
			if code := clitest.CovT_capExit(t, func() {
				clitest.CovT_capOutput(t, func() { runHarnessCreds([]string{"add", "zai", "default", "z-parity-key"}) })
			}); code != -1 {
				t.Fatalf("creds add exited %d", code)
			}
			config, state, err := harnessDirs()
			if err != nil {
				t.Fatal(err)
			}
			if tc.xdg != "" && config != filepath.Join(tc.xdg, "gofastr", "harness") {
				t.Errorf("harness config dir %s ignores XDG_CONFIG_HOME", config)
			}
			prof, err := profile.Embedded("default")
			if err != nil {
				t.Fatal(err)
			}
			h, err := xharness.New(xharness.Config{Profile: prof, WorkingDir: t.TempDir(), XDGConfig: config, XDGState: state,
				Logger: logging.New(nil, logging.LevelWarn), CredstorePass: "parity-pass"})
			if err != nil {
				t.Fatal(err)
			}
			defer h.Shutdown()
			if got, err := h.Creds.Get("zai", "default"); err != nil || got != "z-parity-key" {
				t.Errorf("the harness read %q, %v; want the credential creds add stored", got, err)
			}
		})
	}
}

// gofastr harness and gofastr harness mcp themselves read
// $XDG_CONFIG_HOME/gofastr/harness: a repository approval stored there is
// honoured, and nothing is created under ~/.config.
func TestHarnessAndMCPReadTheXDGConfigDirectory(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func()
	}{
		{"harness", func() { RunHarness([]string{"--prompt", "hi"}) }},
		{"harness mcp", func() { runHarnessMCP(nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, xdg := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", xdg)
			t.Setenv("GOFASTR_HARNESS_PASSPHRASE", "")
			t.Setenv("GOFASTR_HARNESS_MACHINE_KEY", "")
			clonedRepo(t)
			config := filepath.Join(xdg, "gofastr", "harness")
			var consent bytes.Buffer
			loadRepoSecrets(config, false, true, strings.NewReader("y\n"), &consent)
			if err := os.Unsetenv("OPENROUTER_API_KEY"); err != nil {
				t.Fatal(err)
			}

			var code int
			errOut := clitest.CovT_capStderr(t, func() { code = clitest.CovT_capExit(t, tc.run) })
			if code != 1 || !strings.Contains(errOut, noCredstoreKeyHelp) {
				t.Fatalf("exit %d, stderr %q: want exit 1 with the missing-key help", code, errOut)
			}
			if got := os.Getenv("OPENROUTER_API_KEY"); got != "sk-author" || strings.Contains(errOut, "not approved") {
				t.Errorf("the approval stored under XDG_CONFIG_HOME was not read: key %q, stderr %q", got, errOut)
			}
			if _, err := os.Stat(config); err != nil {
				t.Errorf("config directory under XDG_CONFIG_HOME: %v", err)
			}
			if _, err := os.Stat(filepath.Join(home, ".config", "gofastr", "harness")); !os.IsNotExist(err) {
				t.Errorf("~/.config/gofastr/harness exists with XDG_CONFIG_HOME set (stat err %v)", err)
			}
		})
	}
}
