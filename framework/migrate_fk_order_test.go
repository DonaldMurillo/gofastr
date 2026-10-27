package framework

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func regOf(t *testing.T, ents ...*entity.Entity) *Registry {
	t.Helper()
	reg := NewRegistry()
	for _, e := range ents {
		if err := reg.Register(e); err != nil {
			t.Fatalf("register %s: %v", e.GetName(), err)
		}
	}
	return reg
}

// A key change onto columns the same diff adds copies those columns too.
// Leaving them out of the rebuild's copy left nothing to copy, so the
// rebuild skipped its INSERT and still dropped the old table: an opted-in
// apply emptied it. The two rows repeat the defaulted key, so the change
// must fail and keep them.
func TestKeyOntoAddedColumnsKeepsRows(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		before := entity.Define("t", entity.EntityConfig{
			Table:  "t",
			Fields: []schema.Field{{Name: "note", Type: schema.String}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, regOf(t, before)); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		execAll(t, db,
			"INSERT INTO t (id, note) VALUES ('r1', 'one')",
			"INSERT INTO t (id, note) VALUES ('r2', 'two')",
		)
		after := entity.Define("t", entity.EntityConfig{
			Table: "t",
			Fields: []schema.Field{
				{Name: "a", Type: schema.String, Required: true, Default: "x"},
				{Name: "b", Type: schema.String, Required: true, Default: "y"},
			},
			PrimaryKey: []string{"a", "b"},
		}.WithTimestamps(false))
		changes, err := DiffSchema(ctx, db, regOf(t, after))
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		_, err = migrate.ApplySchemaDiffWithOptions(ctx, db, changes, migrate.ApplyOptions{AllowDestructive: true})
		if n := enrollmentRows(t, db, "t"); n != 2 {
			t.Fatalf("rows after the key change = %d (apply err %v), want 2", n, err)
		}
		if err == nil {
			t.Fatal("a key change over two rows repeating the defaulted key applied")
		}
		// One row does not repeat the key, so it moves across with it.
		execAll(t, db, "DELETE FROM t WHERE id = 'r2'")
		applyDestructive(t, db, changes)
		var a, b string
		if err := db.QueryRow("SELECT a, b FROM t").Scan(&a, &b); err != nil {
			t.Fatalf("read the carried row: %v", err)
		}
		if a != "x" || b != "y" {
			t.Fatalf("carried row key = (%q, %q), want (x, y)", a, b)
		}
	})
}

// cascadeParent / cascadeChild: a child whose relation onto the parent
// deletes its rows with it, or (cascade false) a plain column.
func cascadeParent(nType schema.FieldType) *entity.Entity {
	return entity.Define("p", entity.EntityConfig{
		Table:  "p",
		Fields: []schema.Field{{Name: "n", Type: nType}},
	}.WithTimestamps(false))
}

func cascadeChild(cascade bool) *entity.Entity {
	cfg := entity.EntityConfig{
		Table:  "c",
		Fields: []schema.Field{{Name: "p_id", Type: schema.String}},
	}.WithTimestamps(false)
	if cascade {
		cfg.Relations = []entity.Relation{entity.BelongsTo("p", "p", "p_id").OnDeleteAction(entity.OnDeleteCascade)}
	}
	return entity.Define("c", cfg)
}

// The generate guard against a cascading SQLite rebuild read only the
// desired relations. Removing the child's CASCADE relation in the same
// change as a parent retype passed it, though the old constraint is still
// on the child table when the generated file rebuilds the parent: the
// runner's DROP TABLE deleted every child row.
func TestGenerateGuardsLiveCascade(t *testing.T) {
	ctx := context.Background()
	db := openTestDB(t, DialectSQLite)
	oldReg := regOf(t, cascadeParent(schema.Int), cascadeChild(true))
	if err := AutoMigrate(db, oldReg); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	execAll(t, db,
		"INSERT INTO p (id, n) VALUES ('p1', 1)",
		"INSERT INTO c (id, p_id) VALUES ('c1', 'p1')",
	)
	prev := migrate.SnapshotFromRegistry(oldReg, DialectSQLite)
	newReg := regOf(t, cascadeParent(schema.Text), cascadeChild(false))
	up, down, _, err := migrate.GeneratePlan(migrate.Plan{Registry: newReg}, prev, DialectSQLite)
	if err != nil {
		if !strings.Contains(err.Error(), "foreign keys in c would") {
			t.Fatalf("GeneratePlan refused without naming c: %v", err)
		}
		return // refused: nothing ran
	}
	m := coremig.New(db, coremig.WithDialect(DialectSQLite))
	if err := m.Register(coremig.Migration{Version: 1, Name: "retype", Up: up, Down: down}); err != nil {
		t.Fatal(err)
	}
	runErr := m.Up(ctx)
	if n := enrollmentRows(t, db, "c"); n != 1 {
		t.Fatalf("child rows after the generated migration = %d (runner err %v), want 1\n%s", n, runErr, up)
	}
}

// keyedParent is keyed on (a, b) in the given order; keyedChild holds a
// two-column foreign key onto it, its columns paired with the key in the
// given order.
func keyedParent(key ...string) *entity.Entity {
	return entity.Define("kp", entity.EntityConfig{
		Table: "kp",
		Fields: []schema.Field{
			{Name: "a", Type: schema.String, Required: true},
			{Name: "b", Type: schema.String, Required: true},
		},
		PrimaryKey: key,
	}.WithTimestamps(false))
}

func keyedChild(fk1, fk2 string) *entity.Entity {
	return entity.Define("kc", entity.EntityConfig{
		Table: "kc",
		Fields: []schema.Field{
			{Name: "x", Type: schema.String},
			{Name: "y", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("kp", "kp", fk1, fk2)},
	}.WithTimestamps(false))
}

// Reordering a referenced key: Postgres refused to drop the key
// constraint while the child's foreign key depended on it, so even an
// empty table failed to boot.
func TestReorderReferencedKeyBoots(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if err := AutoMigrate(db, regOf(t, keyedParent("a", "b"), keyedChild("x", "y"))); err != nil {
			t.Fatalf("AutoMigrate (a, b): %v", err)
		}
		if err := AutoMigrate(db, regOf(t, keyedParent("b", "a"), keyedChild("y", "x"))); err != nil {
			t.Fatalf("AutoMigrate (b, a): %v", err)
		}
		execAll(t, db, "INSERT INTO kp (a, b) VALUES ('1', '2')")
		execAll(t, db, "INSERT INTO kc (id, x, y) VALUES ('c1', '1', '2')")
		if _, err := db.Exec("INSERT INTO kc (id, x, y) VALUES ('c2', '2', '1')"); err == nil {
			t.Fatal("after the reorder the child accepted (x, y) = (2, 1), which names no parent")
		}
	})
}

// The same reorder through the diff and through a generated migration,
// with rows on Postgres: both detach the child's key, swap the parent's,
// and re-add the child's under the new pairing, keeping the rows.
func TestReorderReferencedKeyMigrates(t *testing.T) {
	ctx := context.Background()
	oldReg := func(t *testing.T) *Registry { return regOf(t, keyedParent("a", "b"), keyedChild("x", "y")) }
	newReg := func(t *testing.T) *Registry { return regOf(t, keyedParent("b", "a"), keyedChild("y", "x")) }
	check := func(t *testing.T, db *sql.DB) {
		t.Helper()
		if n := enrollmentRows(t, db, "kc"); n != 1 {
			t.Fatalf("child rows = %d, want 1", n)
		}
		if _, err := db.Exec("INSERT INTO kc (id, x, y) VALUES ('c2', '2', '1')"); err == nil {
			t.Fatal("after the reorder the child accepted (x, y) = (2, 1), which names no parent")
		}
	}
	seed := func(t *testing.T, db *sql.DB) {
		t.Helper()
		if err := AutoMigrate(db, oldReg(t)); err != nil {
			t.Fatalf("AutoMigrate (a, b): %v", err)
		}
		execAll(t, db, "INSERT INTO kp (a, b) VALUES ('1', '2')", "INSERT INTO kc (id, x, y) VALUES ('c1', '1', '2')")
	}
	t.Run("diff", func(t *testing.T) {
		forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
			seed(t, db)
			changes, err := DiffSchema(ctx, db, newReg(t))
			if err != nil {
				t.Fatalf("DiffSchema: %v", err)
			}
			applyDestructive(t, db, changes)
			check(t, db)
			if after, err := DiffSchema(ctx, db, newReg(t)); err != nil || len(after) != 0 {
				t.Fatalf("DiffSchema after the apply = %+v, %v; want none", after, err)
			}
		})
	})
	// Postgres only: a generated SQLite rebuild of a table other rows
	// reference fails its DROP TABLE inside the runner's transaction, with
	// foreign keys on, and rolls back. That is a refusal, not this bug.
	t.Run("generate", func(t *testing.T) {
		d := DialectPostgres
		func(t *testing.T, db *sql.DB) {
			seed(t, db)
			prev := migrate.SnapshotFromRegistry(oldReg(t), d)
			up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: newReg(t)}, prev, d)
			if err != nil {
				t.Fatalf("GeneratePlan: %v", err)
			}
			m := coremig.New(db, coremig.WithDialect(d))
			if err := m.Register(coremig.Migration{Version: 1, Name: "rekey", Up: up}); err != nil {
				t.Fatal(err)
			}
			if err := m.Up(ctx); err != nil {
				t.Fatalf("runner Up:\n%s\n: %v", up, err)
			}
			check(t, db)
		}(t, openTestDB(t, d))
	})
}

// A relation removed from an existing table leaves its foreign key behind.
// The diff reports dropping it as destructive, so it needs the opt-in.
func TestDiffDropsRemovedRelationKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		if err := AutoMigrate(db, regOf(t, cascadeParent(schema.Int), cascadeChild(true))); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		reg := regOf(t, cascadeParent(schema.Int), cascadeChild(false))
		changes, err := DiffSchema(ctx, db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		var refused *migrate.DestructiveChangeError
		if _, err := ApplySchemaDiff(ctx, db, changes); !errors.As(err, &refused) {
			t.Fatalf("ApplySchemaDiff without opt-in: err = %v, want a DestructiveChangeError (changes %+v)", err, changes)
		}
		applyDestructive(t, db, changes)
		execAll(t, db, "INSERT INTO c (id, p_id) VALUES ('c1', 'nowhere')")
		if after, err := DiffSchema(ctx, db, reg); err != nil || len(after) != 0 {
			t.Fatalf("DiffSchema after the apply = %+v, %v; want none", after, err)
		}
	})
}

// Reversing the columns of a composite foreign key changes what it
// accepts, and the diff never compared foreign keys of an existing table:
// the old constraint kept refusing the newly valid tuple.
func TestDiffReportsForeignKeyReorder(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		if err := AutoMigrate(db, regOf(t, keyedParent("a", "b"), keyedChild("x", "y"))); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		execAll(t, db, "INSERT INTO kp (a, b) VALUES ('1', '2')")
		execAll(t, db, "INSERT INTO kc (id, x, y) VALUES ('c1', '1', '2')")
		reg := regOf(t, keyedParent("a", "b"), keyedChild("y", "x"))
		changes, err := DiffSchema(ctx, db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) == 0 {
			t.Fatal("DiffSchema reports nothing after the foreign key reversed")
		}
		execAll(t, db, "DELETE FROM kc")
		applyDestructive(t, db, changes)
		execAll(t, db, "INSERT INTO kc (id, x, y) VALUES ('c2', '2', '1')")
		if _, err := db.Exec("INSERT INTO kc (id, x, y) VALUES ('c3', '1', '2')"); err == nil {
			t.Fatal("the old pairing (x, y) = (1, 2) is still accepted")
		}
		if after, err := DiffSchema(ctx, db, reg); err != nil || len(after) != 0 {
			t.Fatalf("DiffSchema after the apply = %+v, %v; want none", after, err)
		}
	})
}
