package harnesscmd

import (
	"path/filepath"
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
