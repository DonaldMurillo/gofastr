package migrate_test

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func mixedEntity(extra ...schema.Field) *entity.Entity {
	return entity.Define("mixed", entity.EntityConfig{
		Table: "MixedAccount",
		Fields: append([]schema.Field{
			{Name: "id", Type: schema.String, Required: true},
			{Name: "name", Type: schema.String},
		}, extra...),
		PrimaryKey: []string{"id"},
	}.WithTimestamps(false))
}

// The framework writes and reads a declared MixedAccount as the folded
// mixedaccount. Boot matched live tables without case, so a hand-made
// quoted "MixedAccount" read as the declared table: boot skipped the
// CREATE and altered a mixedaccount that did not exist. Once both exist,
// its key, foreign key and columns read as the entity's drift.
func TestPGQuotedMixedCaseTableNotOurs(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	fkExec(t, db, `CREATE TABLE hand_ref (id TEXT PRIMARY KEY)`)
	fkExec(t, db, `CREATE TABLE "MixedAccount" (code TEXT PRIMARY KEY, ref TEXT REFERENCES hand_ref (id), extra TEXT)`)
	fkExec(t, db, `CREATE TABLE "OnlyQuoted" (id TEXT)`)
	ctx := context.Background()
	boot := func(step int, ent *entity.Entity) {
		t.Helper()
		if err := migrate.AutoMigratePlanContext(ctx, db, migrate.Plan{Registry: fkReg{ent}}); err != nil {
			t.Fatalf("boot %d: %v", step, err)
		}
	}
	boot(1, mixedEntity())
	boot(2, mixedEntity())
	fkExec(t, db, `INSERT INTO mixedaccount (id, name) VALUES ('a', 'x')`)
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM "MixedAccount"`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("the hand-made table changed: rows=%d err=%v", n, err)
	}
	changes, err := migrate.DiffSchema(ctx, db, fkReg{mixedEntity()})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		t.Errorf("drift on the declared table: %s: %s", c.Summary, c.SQL)
	}
	// A column only the quoted table has is still missing from ours.
	boot(3, mixedEntity(schema.Field{Name: "extra", Type: schema.String}))
	fkExec(t, db, `UPDATE mixedaccount SET extra = 'y'`)

	exists, err := migrate.TableExistsBulk(ctx, db, []string{"OnlyQuoted"}, migrate.DialectPostgres)
	if err != nil {
		t.Fatal(err)
	}
	if exists["OnlyQuoted"] {
		t.Fatal(`TableExistsBulk read the quoted "OnlyQuoted" as the declared onlyquoted`)
	}
}
