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
		for _, value := range []struct{ name, argument string }{{"flag", "--help"}, {"empty", ""}} {
			t.Run(flag+"/"+value.name, func(t *testing.T) {
				exitCode := 0
				previous := cli.OsExit
				t.Cleanup(func() { cli.OsExit = previous })
				cli.OsExit = func(code int) { exitCode = code }
				output := clitest.CaptureStdout(t, func() { RunPack([]string{dir, flag, value.argument}) })
				if exitCode != 1 {
					t.Errorf("%s %q exited %d, want refusal", flag, value.argument, exitCode)
				}
				if data, err := os.ReadFile("--help"); !os.IsNotExist(err) {
					t.Errorf("flag became an output file containing secret=%t: %v", strings.Contains(string(data), "never-write-this-secret"), err)
				}
				if strings.Contains(output, "never-write-this-secret") {
					t.Error("secret reached stdout")
				}
			})
		}
	}
}
