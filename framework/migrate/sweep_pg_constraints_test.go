package migrate_test

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func TestSweepPGPivotKeyNeedsItsColumns(t *testing.T) {
	tag := func(byCode bool) *entity.Entity {
		cfg := entity.EntityConfig{
			Table: "spt",
			Fields: []schema.Field{
				{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
				{Name: "code", Type: schema.UUID},
			},
		}.WithTimestamps(false)
		if byCode {
			cfg.PrimaryKey = []string{"code"}
		}
		return entity.Define("spt", cfg)
	}
	post := entity.Define("spp", entity.EntityConfig{
		Table:     "spp",
		Fields:    []schema.Field{{Name: "n", Type: schema.String}},
		Relations: []entity.Relation{entity.ManyToMany("tags", "spt", "spp_tags", "post_id", "tag_id")},
	}.WithTimestamps(false))
	db := testdb.Open(t, migrate.DialectPostgres)
	if err := migrate.AutoMigrate(db, fkReg{tag(false), post}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	fkExec(t, db, "ALTER TABLE spp_tags DROP COLUMN tag_id")
	err := migrate.AutoMigrate(db, fkReg{tag(true), post})
	if err == nil || !strings.Contains(err.Error(), "migrate the pivot explicitly") {
		t.Fatalf("boot with the pivot's tag column gone = %v, want the pivot drift refusal, not a key re-added onto a column the pivot lacks", err)
	}
}

func TestSweepPGForeignKeySwapAndDrop(t *testing.T) {
	parent := entity.Define("sfp", entity.EntityConfig{Table: "sfp", Fields: []schema.Field{{Name: "n", Type: schema.String}}}.WithTimestamps(false))
	child := func(onDelete entity.OnDeleteAction) *entity.Entity {
		return entity.Define("sfc", entity.EntityConfig{
			Table:     "sfc",
			Fields:    []schema.Field{{Name: "p_id", Type: schema.UUID}, {Name: "q", Type: schema.UUID}},
			Relations: []entity.Relation{entity.BelongsTo("sfp", "sfp", "p_id").OnDeleteAction(onDelete)},
		}.WithTimestamps(false))
	}
	db := testdb.Open(t, migrate.DialectPostgres)
	if err := migrate.AutoMigrate(db, fkReg{parent, child(entity.OnDeleteRestrict)}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	fkExec(t, db, "ALTER TABLE sfc ADD CONSTRAINT sfc_q_fkey FOREIGN KEY (q) REFERENCES sfp (id)")
	changes, err := migrate.DiffSchema(context.Background(), db, fkReg{parent, child(entity.OnDeleteCascade)})
	if err != nil {
		t.Fatalf("DiffSchema: %v", err)
	}
	var safe []migrate.SchemaChange
	for _, c := range changes {
		if !c.Destructive {
			safe = append(safe, c)
		}
	}
	if _, err := migrate.ApplySchemaDiff(context.Background(), db, safe); err != nil {
		t.Fatalf("apply the non-destructive changes: %v", err)
	}
	if got := pgConstraints(t, db, "sfc"); len(got) != 2 {
		t.Fatalf("after the swap sfc holds %v, want the swapped p_id key and the undeclared q key", got)
	}
	const pid = "00000000-0000-0000-0000-0000000000aa"
	fkExec(t, db,
		"INSERT INTO sfp (id, n) VALUES ('"+pid+"', 'p')",
		"INSERT INTO sfc (id, p_id) VALUES ('00000000-0000-0000-0000-0000000000bb', '"+pid+"')",
		"DELETE FROM sfp WHERE id = '"+pid+"'",
	)
	var left int
	if err := db.QueryRow("SELECT count(*) FROM sfc").Scan(&left); err != nil || left != 0 {
		t.Fatalf("child rows after deleting the parent = %d (%v), want 0: the swapped key does not cascade", left, err)
	}
	fkApply(t, db, changes)
	if got := pgConstraints(t, db, "sfc"); len(got) != 1 {
		t.Fatalf("after the destructive changes sfc holds %v, want only the declared key", got)
	}
}

func TestSweepPGPartialUniqueCoversNothing(t *testing.T) {
	db := testdb.Open(t, migrate.DialectPostgres)
	if err := migrate.AutoMigrate(db, fkReg{ixEntity(codeField(false))}); err != nil {
		t.Fatal(err)
	}
	fkExec(t, db, "CREATE UNIQUE INDEX ixt_code_partial ON ixt (code) WHERE id = 'none'")
	if err := migrate.AutoMigrate(db, fkReg{ixEntity(codeField(true))}); err != nil {
		t.Fatal(err)
	}
	if !rejectsDuplicate(t, db, "code") {
		t.Fatal("boot took a partial unique index as holding code unique and added none")
	}
}
