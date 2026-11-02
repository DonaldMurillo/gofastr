package migratecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `migrate generate` writes SQLite or Postgres DDL. Any other --db value
// (mysql, which the help lists, or a typo like postgress) fell through
// to SQLite in silence, so the committed file carried DDL for an engine
// the operator never named. A driver it cannot write DDL for is refused
// before anything is written.
func TestMigrateGenerateRefusesUnknownDriver(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	spec := filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(spec, []byte("app:\n  name: testapp\nentities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--db=mysql", "--driver=postgress", "--db=oracle"} {
		code := clitest.CovT_capExit(t, func() {
			clitest.CovT_capStdout(t, func() { RunMigrate([]string{"generate", "init", "--from=" + spec, flag}) })
		})
		if code != 1 {
			t.Errorf("migrate generate %s exited %d, want 1", flag, code)
		}
		if _, err := os.Stat(filepath.Join(dir, "migrations")); !os.IsNotExist(err) {
			t.Fatalf("migrate generate %s wrote migrations/ before refusing", flag)
		}
	}
	for _, driver := range []string{"sqlite", "sqlite3", "postgres", "postgresql", "pgx"} {
		if _, err := generateDialect(driver); err != nil {
			t.Errorf("generateDialect(%q): %v", driver, err)
		}
	}
}

// The runner's SQL (tracking table, placeholders, advisory lock) is
// SQLite or Postgres. --db=mysql was listed in the help and docs, and
// with a MySQL driver linked in it would have run the Postgres dialect;
// the stock binary told the operator to build one that imports it. It
// is refused by name instead.
func TestMigrateRefusesDriverWithoutDialect(t *testing.T) {
	covT_migrationsDir(t)
	err := migrateArgsError([]string{"--db=mysql", "--db-url=mysql://x"})
	if err == nil || !strings.Contains(err.Error(), "SQLite and Postgres") {
		t.Fatalf("migrateArgsError(--db=mysql) = %v, want the SQLite and Postgres refusal", err)
	}
	for _, d := range []string{"--db=sqlite", "--db=postgres"} {
		if err := migrateArgsError([]string{d, "--db-url=x"}); err != nil {
			t.Errorf("migrateArgsError(%s) = %v", d, err)
		}
	}
}
