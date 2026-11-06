package appgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestCodegenJSONPathsIncludeOutput(t *testing.T) {
	outputDirs := []string{"gen", "generated/reports with spaces"}
	if runtime.GOOS != "windows" {
		// A control byte in a valid POSIX directory name: Go's %q spells it
		// \x1b, which no JSON parser reads.
		outputDirs = append(outputDirs, "generated/report\x1b")
	}
	for _, outputDir := range outputDirs {
		for _, dryRun := range []bool{false, true} {
			t.Run(outputDir+map[bool]string{true: "/dry", false: "/write"}[dryRun], func(t *testing.T) {
				dir := jsonExtension(t, "")
				args := []string{"--config=gofastr.settings.yml", "--json", "--out=" + outputDir}
				if dryRun {
					args = append(args, "--dry-run")
				}
				output := clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, func() { GenerateProject(args) }) })
				var document struct{ Files []struct{ Path string } }
				if err := json.Unmarshal([]byte(output), &document); err != nil {
					t.Fatal(err)
				}
				want := filepath.Join(outputDir, "report.go")
				if len(document.Files) != 1 || document.Files[0].Path != want {
					t.Fatalf("reported %s, want %s", output, want)
				}
				if !dryRun {
					if _, err := os.Stat(filepath.Join(dir, document.Files[0].Path)); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
