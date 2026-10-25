package migratecmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// ── migrate generate down-note coverage ──────────────────────────────

func TestRunMigrateGenerateNoDownNote(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	bp := filepath.Join(dir, "gofastr.yml")
	appSpec := `app:
  name: testapp
entities:
  - name: posts
    table: posts
    fields:
      - name: title
        type: string
`
	if err := os.WriteFile(bp, []byte(appSpec), 0o644); err != nil {
		t.Fatal(err)
	}
	// postgres driver path → DialectPostgres branch.
	clitest.CovT_capStdout(t, func() { runMigrateGenerate([]string{"create", "--from=" + bp, "--driver=postgres"}) })
	matches, _ := filepath.Glob(filepath.Join(dir, "migrations", "0001_*.sql"))
	if len(matches) == 0 {
		t.Fatal("no migration generated")
	}
}
