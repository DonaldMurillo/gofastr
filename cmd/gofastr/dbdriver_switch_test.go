package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// pgSwitchSpec declares a Postgres app: the app spec an operator points at
// a real database with `db.driver: postgres`. D-07 keeps refusing
// Postgres-only reserved words for SQLite app specs because DB_DRIVER can
// switch engines at boot, and the generated openDB switch accepts both
// engines — but the generated main.go used to blank-import only the
// declared driver, so switching DB_DRIVER answered `sql: unknown driver`
// and the switch was dead code for the other engine.
const pgSwitchSpec = `app:
  name: PgSwitch
  module: github.com/DonaldMurillo/gofastr/examples/pgswitch
  db:
    driver: postgres
entities:
  - name: widgets
    crud: true
    fields:
      - name: title
        type: string
        required: true
screens:
  - name: home
    route: /
    body:
      - kind: entity_list
        entity: widgets
        fields: [title]
`

// TestPostgresSpecAppBootsOnSQLite proves the engine switch works: a
// generated app that declares Postgres links both drivers, so booting it
// with DB_DRIVER=sqlite and a file DSN opens SQLite, migrates and serves.
// Before the fix the child died at once with `sql: unknown driver
// "sqlite3"`, which bootGeneratedApp reports as an exit before serving.
//
// The test writes its fixture instead of reading one, and generation runs
// the in-tree CLI, so it declares only the generator as its input.
func TestPostgresSpecAppBootsOnSQLite(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, builds, and boots an app")
	}
	clitest.VariantBuild(t, "cmd/gofastr/internal/appgen/")
	fixture := filepath.Join(t.TempDir(), "gofastr.yml")
	if err := os.WriteFile(fixture, []byte(pgSwitchSpec), 0o600); err != nil {
		t.Fatalf("write fixture spec: %v", err)
	}
	bin, appDir := exampleTree(t, fixture, "pgswitch")
	// bootGeneratedApp sets DATABASE_URL to a fresh SQLite file; DB_DRIVER
	// rides the inherited environment, which is exactly the operator's
	// engine switch.
	t.Setenv("DB_DRIVER", "sqlite")
	baseURL, output, _ := bootGeneratedApp(t, "pgswitch", bin, appDir)
	if strings.Contains(output.String(), "unknown driver") {
		t.Fatalf("the generated app still links only its declared driver:\n%s", output.String())
	}
	resp, err := bootProbeClient.Get(baseURL + "/")
	if err != nil {
		t.Fatalf("probe %s: %v", baseURL, err)
	}
	resp.Body.Close()
	if resp.StatusCode >= 500 {
		t.Fatalf("booted on sqlite but / answered %d:\n%s", resp.StatusCode, output.String())
	}
}
