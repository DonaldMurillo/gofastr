package migrate_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	migrate "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// A misspelled section directive was skipped as a directive the parser
// did not know, and the section above it stayed open. "-- +migrate down"
// (or "--+migrate Down", or "-- +Migrate Down") under an Up section put
// the Down statements into Up: the rollback ran as part of the apply, and
// the version was recorded applied (2026-11-01 bug bash). A row the Up
// inserted and the Down deletes is the data that went missing.

func typoFile(downDirective string) string {
	return "-- +migrate Version 2\n-- +migrate Name seed_admin\n-- +migrate Up\n" +
		"INSERT INTO settings (k, v) VALUES ('admin', 'on');\n" +
		downDirective + "\n" +
		"DELETE FROM settings;\n"
}

func TestMisspelledDirectiveRefused(t *testing.T) {
	typos := []string{
		"-- +migrate down",
		"-- +migrate DOWN",
		"--+migrate Down",
		"-- +Migrate Down",
		"--  +migrate Down",
		"-- +migrate Rollback",
	}
	dbs := map[string]func(t *testing.T) (*sql.DB, migrate.Dialect){
		"sqlite": func(t *testing.T) (*sql.DB, migrate.Dialect) {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { db.Close() })
			return db, migrate.DialectSQLite
		},
		"postgres": func(t *testing.T) (*sql.DB, migrate.Dialect) {
			return pgtest.DB(t), migrate.DialectPostgres
		},
	}
	for _, typo := range typos {
		for engine, open := range dbs {
			t.Run(engine+"/"+typo, func(t *testing.T) {
				db, d := open(t)
				m := migrate.New(db, migrate.WithDialect(d))
				if err := m.Register(migrate.Migration{Version: 1, Name: "settings",
					Up:   "CREATE TABLE settings (k TEXT PRIMARY KEY, v TEXT); INSERT INTO settings (k, v) VALUES ('site', 'up')",
					Down: "DROP TABLE settings"}); err != nil {
					t.Fatal(err)
				}
				regErr := m.RegisterFromReader(strings.NewReader(typoFile(typo)))
				if regErr != nil {
					if !strings.Contains(regErr.Error(), "directive") {
						t.Fatalf("refusal does not name the directive: %v", regErr)
					}
					return
				}
				if err := m.Up(context.Background()); err != nil {
					t.Fatalf("Up: %v", err)
				}
				var n int
				if err := db.QueryRow("SELECT COUNT(*) FROM settings").Scan(&n); err != nil {
					t.Fatal(err)
				}
				t.Fatalf("a %q line was taken for no directive: the Down statements ran inside Up and settings holds %d of its 2 rows", typo, n)
			})
		}
	}
}

// The directives the runner reads still parse, with their spacing.
func TestKnownDirectivesStillParse(t *testing.T) {
	file := "-- +migrate Version 3\n-- +migrate Name   ok\n-- +migrate NoTransaction\n" +
		"  -- +migrate Up  \nCREATE INDEX x ON t (a);\n-- +migrate   Down\nDROP INDEX x;\n"
	m := migrate.New(nil, migrate.WithDialect(migrate.DialectSQLite))
	if err := m.RegisterFromReader(strings.NewReader(file)); err != nil {
		t.Fatalf("a file spelling every directive was refused: %v", err)
	}
}
