package packcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestPackRefusesFlagAsOutput(t *testing.T) {
	dir := materializeAppSpec(t, packCheckFixture())
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("JWT_SECRET=never-write-this-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for _, flag := range []string{"-o", "--out"} {
		exitCode := 0
		previous := cli.OsExit
		cli.OsExit = func(code int) { exitCode = code }
		output := clitest.CaptureStdout(t, func() { RunPack([]string{dir, flag, "--help"}) })
		cli.OsExit = previous
		if exitCode != 1 {
			t.Errorf("%s --help exited %d, want refusal", flag, exitCode)
		}
		if data, err := os.ReadFile("--help"); !os.IsNotExist(err) {
			t.Errorf("flag became an output file containing secret=%t: %v", strings.Contains(string(data), "never-write-this-secret"), err)
		}
		if strings.Contains(output, "never-write-this-secret") {
			t.Error("secret reached stdout")
		}
	}
}
