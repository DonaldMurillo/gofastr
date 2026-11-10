package migratecmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// `migrate generate --from=gofastr.yml` with no --db wrote SQLite DDL
// whatever database the app spec declared. A spec on Postgres got
// DATETIME columns and BOOLEAN DEFAULT 1, which Postgres refuses, so the
// first `migrate up` of the committed file failed. With no flag the
// spec's own app.db decides; a flag still wins.
func TestMigrateGenerateFollowsTheSpecDriver(t *testing.T) {
	const entities = "entities:\n  - name: posts\n    fields:\n      - name: live\n        type: bool\n        default: true\n      - name: at\n        type: datetime\n"
	for name, tc := range map[string]struct {
		db       string
		flags    []string
		postgres bool
	}{
		"driver postgres":         {db: "  db:\n    driver: postgres\n    url: postgres://u@localhost/app\n", postgres: true},
		"driver postgresql":       {db: "  db:\n    driver: postgresql\n    url: postgres://u@localhost/app\n", postgres: true},
		"postgres url, no driver": {db: "  db:\n    url: postgresql://u@localhost/app\n", postgres: true},
		"driver sqlite":           {db: "  db:\n    driver: sqlite\n    url: file:app.db\n"},
		"no db section":           {},
		"flag wins over the spec": {db: "  db:\n    driver: postgres\n    url: postgres://u@localhost/app\n", flags: []string{"--db=sqlite"}},
		"flag wins, to postgres":  {db: "  db:\n    driver: sqlite\n    url: file:app.db\n", flags: []string{"--driver=postgres"}, postgres: true},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			clitest.CovT_chdir(t, dir)
			spec := filepath.Join(dir, "gofastr.yml")
			if err := os.WriteFile(spec, []byte("app:\n  name: testapp\n"+tc.db+entities), 0o644); err != nil {
				t.Fatal(err)
			}
			code := clitest.CovT_capExit(t, func() {
				clitest.CovT_capStdout(t, func() {
					RunMigrate(append([]string{"generate", "init", "--from=" + spec}, tc.flags...))
				})
			})
			if code != -1 { // -1: the command returned without exiting
				t.Fatalf("migrate generate exited %d", code)
			}
			body, err := os.ReadFile(filepath.Join(dir, "migrations", "0001_init.sql"))
			if err != nil {
				t.Fatal(err)
			}
			// DATETIME and an integer boolean default are SQLite's spelling;
			// Postgres has no DATETIME type and refuses DEFAULT 1 on a boolean.
			sqlite := strings.Contains(string(body), "DATETIME") || strings.Contains(string(body), "DEFAULT 1")
			if sqlite == tc.postgres {
				t.Fatalf("want postgres DDL = %v, got:\n%s", tc.postgres, body)
			}
		})
	}
}

// A spec on a database the generator has no DDL for is refused by name
// instead of getting SQLite DDL.
func TestMigrateGenerateRefusesSpecDriverWithoutDialect(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	spec := filepath.Join(dir, "gofastr.yml")
	if err := os.WriteFile(spec, []byte("app:\n  name: testapp\n  db:\n    driver: mysql\n    url: mysql://u@localhost/app\nentities:\n  - name: posts\n    fields:\n      - name: title\n        type: string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out string
	code := clitest.CovT_capExit(t, func() {
		out = clitest.CovT_capStdout(t, func() { RunMigrate([]string{"generate", "init", "--from=" + spec}) })
	})
	if code != 1 {
		t.Fatalf("migrate generate exited %d, want 1; output:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "migrations")); !os.IsNotExist(err) {
		t.Fatal("migrate generate wrote migrations/ before refusing")
	}
}
