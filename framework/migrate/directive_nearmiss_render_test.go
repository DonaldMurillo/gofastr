package migrate_test

import (
	"errors"
	"os"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// The runner refuses a line that reads as a directive with its spacing or
// case off ("--+migrate Down"), but the generator's guard matched only the
// exact "-- +migrate" prefix. A multi-line DEFAULT holding such a line was
// written into a migration file the runner then refused to load: the
// guard must be exactly as wide as the parser (2026-11-01 bug bash review).
func TestGeneratorGuardCoversNearMissDirectives(t *testing.T) {
	for _, line := range []string{
		"--+migrate Down",
		"-- +Migrate Down",
		"--  +migrate Down",
		"\t--+MIGRATE Down",
		"-- +migrated from v1",
	} {
		t.Run(line, func(t *testing.T) {
			ent := entity.Define("nm", entity.EntityConfig{
				Table: "nm",
				Fields: []schema.Field{
					{Name: "id", Type: schema.String, Required: true},
					{Name: "note", Type: schema.String, Default: "a\n" + line + "\nb"},
				},
				PrimaryKey: []string{"id"},
			}.WithTimestamps(false))
			dir := t.TempDir()
			path, err := migrate.GenerateMigrationFile(migrate.Plan{Registry: fkReg{ent}}, "nm",
				migrate.MigrationFileOptions{MigrationsDir: dir, Dialect: migrate.DialectSQLite})
			if errors.Is(err, migrate.ErrDirectiveInSQL) {
				return
			}
			if err != nil {
				t.Fatalf("generate: %v", err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			m := coremig.New(nil, coremig.WithDialect(coremig.DialectSQLite))
			if err := m.RegisterFromReader(f); err != nil {
				t.Fatalf("the generator wrote %s, which the runner refuses: %v", path, err)
			}
			if body, _ := os.ReadFile(path); !strings.Contains(string(body), line) {
				t.Fatalf("the rendered file lost the default's line")
			}
		})
	}
}
