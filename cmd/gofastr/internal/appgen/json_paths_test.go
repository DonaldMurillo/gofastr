package appgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestCodegenJSONPathsIncludeOutput(t *testing.T) {
	for _, outputDir := range []string{"gen", "generated/reports with spaces"} {
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
