package migratecmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestGenerateRejectsInvalidEntities(t *testing.T) {
	for _, value := range []string{"abc", "-5"} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			clitest.CovT_chdir(t, dir)
			spec := filepath.Join(dir, "gofastr.yml")
			valid := "app:\n  name: testapp\nentities:\n  - name: posts\n    fields:\n      - name: count\n        type: int\n        min: 0\n"
			clitest.WriteTestFile(t, spec, valid)
			args := []string{"generate", "init", "--from=" + spec}
			if code := clitest.CovT_capExit(t, func() {
				clitest.CovT_capStdout(t, func() { RunMigrate(args) })
			}); code != -1 {
				t.Fatalf("valid schema exited %d", code)
			}
			snapshotPath := filepath.Join(dir, "migrations", "schema.snapshot.json")
			before, err := os.ReadFile(snapshotPath)
			if err != nil {
				t.Fatal(err)
			}
			clitest.WriteTestFile(t, spec, valid+"        default: "+value+"\n")
			var code int
			output := clitest.CovT_capStdout(t, func() {
				code = clitest.CovT_capExit(t, func() { RunMigrate([]string{"generate", "bad", "--from=" + spec}) })
			})
			if code != 1 {
				t.Errorf("invalid default %q exited %d, want 1; output: %s", value, code, output)
			}
			if code == 1 && !strings.Contains(output, "posts") {
				t.Errorf("refusal does not name entity: %s", output)
			}
			after, err := os.ReadFile(snapshotPath)
			if err != nil || !bytes.Equal(before, after) {
				t.Errorf("invalid schema changed committed snapshot: %v", err)
			}
			files, err := filepath.Glob(filepath.Join(dir, "migrations", "*.sql"))
			if err != nil || len(files) != 1 {
				t.Errorf("invalid schema wrote a migration: %v, %v", files, err)
			}
		})
	}
}
