package migrate_test

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/internal/testdb"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// fkReg is a minimal entity.Registry over a fixed set of entities.
type fkReg []*entity.Entity

func (r fkReg) All() map[string]*entity.Entity {
	out := make(map[string]*entity.Entity, len(r))
	for _, e := range r {
		out[e.GetName()] = e
	}
	return out
}

func (r fkReg) AllSorted() []*entity.Entity {
	out := slices.Clone([]*entity.Entity(r))
	slices.SortFunc(out, func(a, b *entity.Entity) int { return strings.Compare(a.GetName(), b.GetName()) })
	return out
}

func (r fkReg) Get(name string) (*entity.Entity, error) {
	for _, e := range r {
		if e.GetName() == name {
			return e, nil
		}
	}
	return nil, errors.New("not found")
}

func fkExec(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, q := range stmts {
		if _, err := db.ExecContext(context.Background(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func fkApply(t *testing.T, db *sql.DB, changes []migrate.SchemaChange) {
	t.Helper()
	if _, err := migrate.ApplySchemaDiffWithOptions(context.Background(), db, changes, migrate.ApplyOptions{AllowDestructive: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
}

func fkRunUp(t *testing.T, db *sql.DB, d migrate.Dialect, up string) {
	t.Helper()
	m := coremig.New(db, coremig.WithDialect(d))
	if err := m.Register(coremig.Migration{Version: 1, Name: "m", Up: up}); err != nil {
		t.Fatal(err)
	}
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("runner Up:\n%s\n: %v", up, err)
	}
}

// pgConstraints lists the foreign-key constraint names table holds.
func pgConstraints(t *testing.T, db *sql.DB, table string) []string {
	t.Helper()
	rows, err := db.Query(`SELECT conname FROM pg_constraint WHERE contype = 'f' AND conrelid = $1::regclass ORDER BY conname`, table)
	if err != nil {
		t.Fatalf("read constraints of %s: %v", table, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// rkParent is keyed on (a, b) in the given order, with a separately
// unique column code.
func rkParent(key ...string) *entity.Entity {
	return entity.Define("rkp", entity.EntityConfig{
		Table: "rkp",
		Fields: []schema.Field{
			{Name: "a", Type: schema.String, Required: true},
			{Name: "b", Type: schema.String, Required: true},
			{Name: "code", Type: schema.String, Unique: true},
		},
		PrimaryKey: key,
	}.WithTimestamps(false))
}

// A parent key change detached the child's foreign key and re-added it
// only when the child's PREVIOUS columns held it. A child that renames a
// key column in the same change got no re-add, and a generated migration
// never compares foreign keys, so it committed without the constraint.
func TestKeyChangeReaddsAfterChildRename(t *testing.T) {
	ctx := context.Background()
	child := func(renamed bool) *entity.Entity {
		cfg := entity.EntityConfig{
			Table: "rkc",
			Fields: []schema.Field{
				{Name: "x", Type: schema.String},
				{Name: "y", Type: schema.String},
			},
			Relations: []entity.Relation{entity.BelongsTo("rkp", "rkp", "x", "y")},
		}.WithTimestamps(false)
		if renamed {
			cfg.Fields[0].Name = "u"
			cfg.Renames = map[string]string{"x": "u"}
			cfg.Relations = []entity.Relation{entity.BelongsTo("rkp", "rkp", "y", "u")}
		}
		return entity.Define("rkc", cfg)
	}
	oldReg := fkReg{rkParent("a", "b"), child(false)}
	newReg := fkReg{rkParent("b", "a"), child(true)}
	seed := func(t *testing.T, db *sql.DB) {
		t.Helper()
		if err := migrate.AutoMigrate(db, oldReg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		fkExec(t, db, "INSERT INTO rkp (a, b) VALUES ('1', '2')", "INSERT INTO rkc (id, x, y) VALUES ('c1', '1', '2')")
	}
	check := func(t *testing.T, db *sql.DB) {
		t.Helper()
		if _, err := db.Exec("INSERT INTO rkc (id, u, y) VALUES ('c2', '9', '9')"); err == nil {
			t.Fatal("the child accepted (u, y) = (9, 9), which names no parent: its foreign key is gone")
		}
		if after, err := migrate.DiffSchema(ctx, db, newReg); err != nil || len(after) != 0 {
			t.Fatalf("DiffSchema after the change = %+v, %v; want none", after, err)
		}
	}
	t.Run("generate", func(t *testing.T) {
		d := migrate.DialectPostgres
		db := testdb.Open(t, d)
		seed(t, db)
		up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: newReg}, migrate.SnapshotFromRegistry(oldReg, d), d)
		if err != nil {
			t.Fatalf("GeneratePlan: %v", err)
		}
		fkRunUp(t, db, d, up)
		check(t, db)
	})
	// Boot renames nothing, a pending rename hint stops it adding u, and
	// it drops no foreign key a relation no longer declares, so this child
	// held no key before and declares u with no hint: boot adds u, and the
	// key change on the empty parent has to leave the child holding (y, u).
	t.Run("boot", func(t *testing.T) {
		db := testdb.Open(t, migrate.DialectPostgres)
		bare := child(false)
		bare.Config.Relations = nil
		if err := migrate.AutoMigrate(db, fkReg{rkParent("a", "b"), bare}); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		added := child(true)
		added.Config.Renames = nil
		if err := migrate.AutoMigrate(db, fkReg{rkParent("b", "a"), added}); err != nil {
			t.Fatalf("AutoMigrate (b, a): %v", err)
		}
		if _, err := db.Exec("INSERT INTO rkc (id, u, y) VALUES ('c2', '9', '9')"); err == nil {
			t.Fatal("the child accepted (u, y) = (9, 9), which names no parent: its foreign key is gone")
		}
	})
	t.Run("diff", func(t *testing.T) {
		testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, _ migrate.Dialect) {
			seed(t, db)
			changes, err := migrate.DiffSchema(ctx, db, newReg)
			if err != nil {
				t.Fatalf("DiffSchema: %v", err)
			}
			fkApply(t, db, changes)
			check(t, db)
		})
	})
}

// The detach before a Postgres key change dropped every foreign key a
// child held onto the parent, so a hand-added key onto a separately
// unique parent column went at boot with no opt-in. Only the keys that
// depend on the replaced key go.
func TestKeyChangeKeepsKeyOntoUniqueColumn(t *testing.T) {
	ctx := context.Background()
	child := func(fk1, fk2 string) *entity.Entity {
		return entity.Define("rkc", entity.EntityConfig{
			Table: "rkc",
			Fields: []schema.Field{
				{Name: "x", Type: schema.String},
				{Name: "y", Type: schema.String},
				{Name: "pcode", Type: schema.String},
			},
			Relations: []entity.Relation{entity.BelongsTo("rkp", "rkp", fk1, fk2)},
		}.WithTimestamps(false))
	}
	seed := func(t *testing.T, db *sql.DB) {
		t.Helper()
		if err := migrate.AutoMigrate(db, fkReg{rkParent("a", "b"), child("x", "y")}); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		fkExec(t, db, "ALTER TABLE rkc ADD CONSTRAINT rkc_pcode_fk FOREIGN KEY (pcode) REFERENCES rkp(code)")
	}
	newReg := fkReg{rkParent("b", "a"), child("y", "x")}
	kept := func(t *testing.T, db *sql.DB) {
		t.Helper()
		if got := pgConstraints(t, db, "rkc"); !slices.Contains(got, "rkc_pcode_fk") {
			t.Fatalf("rkc constraints after the key change = %v; the hand-added rkc_pcode_fk is gone", got)
		}
	}
	d := migrate.DialectPostgres
	t.Run("boot", func(t *testing.T) {
		db := testdb.Open(t, d)
		seed(t, db)
		if err := migrate.AutoMigrate(db, newReg); err != nil {
			t.Fatalf("AutoMigrate (b, a): %v", err)
		}
		kept(t, db)
	})
	t.Run("generate", func(t *testing.T) {
		db := testdb.Open(t, d)
		seed(t, db)
		up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: newReg}, migrate.SnapshotFromRegistry(fkReg{rkParent("a", "b"), child("x", "y")}, d), d)
		if err != nil {
			t.Fatalf("GeneratePlan: %v", err)
		}
		fkRunUp(t, db, d, up)
		kept(t, db)
	})
	// The diff reports the hand-added key as it would without the key
	// change: a key no relation declares, dropped only with the opt-in.
	t.Run("diff", func(t *testing.T) {
		db := testdb.Open(t, d)
		seed(t, db)
		changes, err := migrate.DiffSchema(ctx, db, newReg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		var noted bool
		for _, c := range changes {
			if strings.Contains(c.Summary, "drop foreign key (pcode)") && c.Destructive {
				noted = true
			}
		}
		if !noted {
			t.Fatalf("DiffSchema does not report the undeclared (pcode) key as a destructive drop: %+v", changes)
		}
	})
}

// Renaming a parent's key column renames the column every foreign key
// onto it names (SQLite's RENAME COLUMN rewrites the child's definition,
// Postgres keys by column number), but the comparison read the child's
// key as still naming the old column: SQLite scheduled a destructive
// child rebuild that default apply refused, and Postgres swapped the
// constraint for nothing.
func TestParentKeyRenameKeepsChildKey(t *testing.T) {
	ctx := context.Background()
	parent := func(renamed bool) *entity.Entity {
		cfg := entity.EntityConfig{
			Table:  "rnp",
			Fields: []schema.Field{{Name: "n", Type: schema.String}},
		}.WithTimestamps(false)
		if renamed {
			cfg.Fields = append(cfg.Fields, schema.Field{Name: "code", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true})
			cfg.PrimaryKey = []string{"code"}
			cfg.Renames = map[string]string{"id": "code"}
		}
		return entity.Define("rnp", cfg)
	}
	child := entity.Define("rnc", entity.EntityConfig{
		Table:     "rnc",
		Fields:    []schema.Field{{Name: "p_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("rnp", "rnp", "p_id")},
	}.WithTimestamps(false))
	testdb.ForEachDialect(t, func(t *testing.T, db *sql.DB, _ migrate.Dialect) {
		if err := migrate.AutoMigrate(db, fkReg{parent(false), child}); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		reg := fkReg{parent(true), child}
		changes, err := migrate.DiffSchema(ctx, db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		for _, c := range changes {
			if strings.Contains(c.Summary, "foreign key") {
				t.Errorf("the rename reported a foreign key change: %s", c.Summary)
			}
		}
		if _, err := migrate.ApplySchemaDiff(ctx, db, changes); err != nil {
			t.Fatalf("ApplySchemaDiff without opt-in: %v", err)
		}
		if _, err := db.Exec("INSERT INTO rnc (id, p_id) VALUES ('c1', 'nowhere')"); err == nil {
			t.Fatal("the child accepted a p_id naming no parent after the rename")
		}
		if after, err := migrate.DiffSchema(ctx, db, reg); err != nil || len(after) != 0 {
			t.Fatalf("DiffSchema after the apply = %+v, %v; want none", after, err)
		}
	})
}

// A generated many_to_many pivot holds a foreign key onto each side's key,
// but the detach before a Postgres key change walked only the entities'
// belongs-to relations: dropping the old key failed (2BP01) on the
// pivot's dependent constraint, even on empty tables.
func TestKeyChangeDetachesPivotKey(t *testing.T) {
	ctx := context.Background()
	tag := entity.Define("pvt", entity.EntityConfig{Table: "pvt", Fields: []schema.Field{{Name: "n", Type: schema.String}}}.WithTimestamps(false))
	post := func(byCode bool) *entity.Entity {
		cfg := entity.EntityConfig{
			Table: "pvp",
			Fields: []schema.Field{
				{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
				{Name: "code", Type: schema.UUID},
			},
			Relations: []entity.Relation{entity.ManyToMany("tags", "pvt", "pvp_tags", "post_id", "tag_id")},
		}.WithTimestamps(false)
		if byCode {
			cfg.PrimaryKey = []string{"code"}
		}
		return entity.Define("pvp", cfg)
	}
	oldReg := fkReg{tag, post(false)}
	newReg := fkReg{tag, post(true)}
	const code = "00000000-0000-0000-0000-000000000001"
	check := func(t *testing.T, db *sql.DB) {
		t.Helper()
		fkExec(t, db,
			"INSERT INTO pvt (id, n) VALUES ('00000000-0000-0000-0000-0000000000aa', 't')",
			"INSERT INTO pvp (id, code) VALUES ('00000000-0000-0000-0000-0000000000bb', '"+code+"')",
			"INSERT INTO pvp_tags (post_id, tag_id) VALUES ('"+code+"', '00000000-0000-0000-0000-0000000000aa')",
		)
		if _, err := db.Exec("INSERT INTO pvp_tags (post_id, tag_id) VALUES ('00000000-0000-0000-0000-0000000000bb', '00000000-0000-0000-0000-0000000000aa')"); err == nil {
			t.Fatal("the pivot accepted a post_id naming no post code: its key onto pvp is gone")
		}
	}
	d := migrate.DialectPostgres
	t.Run("boot", func(t *testing.T) {
		db := testdb.Open(t, d)
		if err := migrate.AutoMigrate(db, oldReg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		if err := migrate.AutoMigrate(db, newReg); err != nil {
			t.Fatalf("AutoMigrate (code): %v", err)
		}
		check(t, db)
	})
	t.Run("diff", func(t *testing.T) {
		db := testdb.Open(t, d)
		if err := migrate.AutoMigrate(db, oldReg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		changes, err := migrate.DiffSchema(ctx, db, newReg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		fkApply(t, db, changes)
		check(t, db)
	})
	t.Run("generate", func(t *testing.T) {
		db := testdb.Open(t, d)
		if err := migrate.AutoMigrate(db, oldReg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: newReg}, migrate.SnapshotFromRegistry(oldReg, d), d)
		if err != nil {
			t.Fatalf("GeneratePlan: %v", err)
		}
		fkRunUp(t, db, d, up)
		check(t, db)
	})
}

// The generate guard against a cascading SQLite rebuild matched its
// pattern over the whole recorded DDL, string literals included: a child
// column whose text DEFAULT spells a cascading reference counted as one,
// and a retype of the parent was refused.
func TestCascadeGuardSkipsLiterals(t *testing.T) {
	parent := func(typ schema.FieldType) *entity.Entity {
		return entity.Define("lgp", entity.EntityConfig{
			Table:  "lgp",
			Fields: []schema.Field{{Name: "n", Type: typ}},
		}.WithTimestamps(false))
	}
	child := entity.Define("lgc", entity.EntityConfig{
		Table:  "lgc",
		Fields: []schema.Field{{Name: "note", Type: schema.String, Default: "REFERENCES lgp(id) ON DELETE CASCADE"}},
	}.WithTimestamps(false))
	d := migrate.DialectSQLite
	prev := migrate.SnapshotFromRegistry(fkReg{parent(schema.Int), child}, d)
	if !strings.Contains(prev.TableDDL["lgc"], "ON DELETE CASCADE") {
		t.Fatalf("the child's recorded DDL does not carry the literal: %s", prev.TableDDL["lgc"])
	}
	if _, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: fkReg{parent(schema.Text), child}}, prev, d); err != nil {
		t.Fatalf("GeneratePlan refused a retype no foreign key cascades onto: %v", err)
	}
}

// A Postgres key change detaches the foreign keys onto the old key. One
// no relation declares (hand-added, onto the key's own columns) went with
// them at boot, with no opt-in. Boot and a generated migration now refuse
// and keep it; the diff reports it as the destructive drop it is.
func TestKeyChangeRefusesUndeclaredKey(t *testing.T) {
	ctx := context.Background()
	child := func(fk1, fk2 string) *entity.Entity {
		return entity.Define("rkc", entity.EntityConfig{
			Table: "rkc",
			Fields: []schema.Field{
				{Name: "x", Type: schema.String},
				{Name: "y", Type: schema.String},
				{Name: "z", Type: schema.String},
				{Name: "w", Type: schema.String},
			},
			Relations: []entity.Relation{entity.BelongsTo("rkp", "rkp", fk1, fk2)},
		}.WithTimestamps(false))
	}
	oldReg := fkReg{rkParent("a", "b"), child("x", "y")}
	newReg := fkReg{rkParent("b", "a"), child("y", "x")}
	seed := func(t *testing.T, db *sql.DB) {
		t.Helper()
		if err := migrate.AutoMigrate(db, oldReg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		fkExec(t, db, "ALTER TABLE rkc ADD CONSTRAINT rkc_hand_fk FOREIGN KEY (z, w) REFERENCES rkp(a, b)")
	}
	kept := func(t *testing.T, db *sql.DB, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "rkc_hand_fk") {
			t.Fatalf("err = %v, want a refusal naming rkc_hand_fk", err)
		}
		if got := pgConstraints(t, db, "rkc"); !slices.Contains(got, "rkc_hand_fk") {
			t.Fatalf("rkc constraints after the refusal = %v; rkc_hand_fk is gone", got)
		}
	}
	d := migrate.DialectPostgres
	t.Run("boot", func(t *testing.T) {
		db := testdb.Open(t, d)
		seed(t, db)
		kept(t, db, migrate.AutoMigrate(db, newReg))
	})
	t.Run("generate", func(t *testing.T) {
		db := testdb.Open(t, d)
		seed(t, db)
		up, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: newReg}, migrate.SnapshotFromRegistry(oldReg, d), d)
		if err != nil {
			t.Fatalf("GeneratePlan: %v", err)
		}
		m := coremig.New(db, coremig.WithDialect(d))
		if err := m.Register(coremig.Migration{Version: 1, Name: "m", Up: up}); err != nil {
			t.Fatal(err)
		}
		kept(t, db, m.Up(ctx))
	})
	t.Run("diff", func(t *testing.T) {
		db := testdb.Open(t, d)
		seed(t, db)
		changes, err := migrate.DiffSchema(ctx, db, newReg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		reported := slices.ContainsFunc(changes, func(c migrate.SchemaChange) bool {
			return c.Destructive && strings.Contains(c.Summary, "drop foreign key (z, w)")
		})
		if !reported {
			t.Fatalf("DiffSchema does not report dropping (z, w) as destructive: %+v", changes)
		}
		fkApply(t, db, changes)
		if got := pgConstraints(t, db, "rkc"); slices.Contains(got, "rkc_hand_fk") {
			t.Fatalf("rkc constraints after the opted-in apply = %v; rkc_hand_fk should be dropped", got)
		}
		if after, err := migrate.DiffSchema(ctx, db, newReg); err != nil || len(after) != 0 {
			t.Fatalf("DiffSchema after the apply = %+v, %v; want none", after, err)
		}
	})
}

// skParent is keyed on id, or on code; skChild and the generated pivot
// sk_tags both reference its key.
func skParent(byCode bool, pivot bool) *entity.Entity {
	cfg := entity.EntityConfig{
		Table: "skp",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
			{Name: "code", Type: schema.UUID},
		},
	}.WithTimestamps(false)
	if byCode {
		cfg.PrimaryKey = []string{"code"}
	}
	if pivot {
		cfg.Relations = []entity.Relation{entity.ManyToMany("tags", "skt", "sk_tags", "post_id", "tag_id")}
	}
	return entity.Define("skp", cfg)
}

// SQLite cannot repoint a foreign key, and boot rebuilt only the table
// whose key changed: a child or pivot naming the old key column was left
// referencing a column no key holds, and every later write to it failed
// with "foreign key mismatch". A generated migration did the same. Both
// refuse now, naming the table that references the old key, and change
// nothing. A reorder of the same columns still boots (the key set is
// unchanged).
func TestSQLiteKeyChangeRefusesStrandedKeys(t *testing.T) {
	tag := entity.Define("skt", entity.EntityConfig{Table: "skt", Fields: []schema.Field{{Name: "n", Type: schema.String}}}.WithTimestamps(false))
	child := entity.Define("skc", entity.EntityConfig{
		Table:     "skc",
		Fields:    []schema.Field{{Name: "p_id", Type: schema.UUID}},
		Relations: []entity.Relation{entity.BelongsTo("skp", "skp", "p_id")},
	}.WithTimestamps(false))
	cases := []struct {
		name       string
		old, new   fkReg
		referrer   string
		writeAfter string
	}{
		{"child", fkReg{skParent(false, false), child}, fkReg{skParent(true, false), child}, "skc",
			"INSERT INTO skc (id, p_id) VALUES ('c1', 'i1')"},
		{"pivot", fkReg{skParent(false, true)}, fkReg{skParent(true, true)}, "sk_tags",
			"INSERT INTO sk_tags (post_id, tag_id) VALUES ('i1', 't1')"},
	}
	d := migrate.DialectSQLite
	for _, tc := range cases {
		refused := func(t *testing.T, db *sql.DB, err error) {
			t.Helper()
			if err == nil || !strings.Contains(err.Error(), tc.referrer) {
				t.Fatalf("err = %v, want a refusal naming %s", err, tc.referrer)
			}
			// Nothing changed: the old key still holds the references.
			fkExec(t, db, "INSERT INTO skt (id, n) VALUES ('t1', 't')", "INSERT INTO skp (id, code) VALUES ('i1', 'k1')", tc.writeAfter)
		}
		t.Run(tc.name+"/boot", func(t *testing.T) {
			db := testdb.Open(t, d)
			if err := migrate.AutoMigrate(db, append(tc.old, tag)); err != nil {
				t.Fatalf("AutoMigrate: %v", err)
			}
			refused(t, db, migrate.AutoMigrate(db, append(tc.new, tag)))
		})
		t.Run(tc.name+"/generate", func(t *testing.T) {
			db := testdb.Open(t, d)
			if err := migrate.AutoMigrate(db, append(tc.old, tag)); err != nil {
				t.Fatalf("AutoMigrate: %v", err)
			}
			_, _, _, err := migrate.GeneratePlan(migrate.Plan{Registry: append(tc.new, tag)}, migrate.SnapshotFromRegistry(append(tc.old, tag), d), d)
			refused(t, db, err)
		})
		// The refusals point here: DiffSchema rebuilds an entity table
		// that references the old key too. A generated pivot it does not
		// rebuild, so that case says to write the migration by hand.
		t.Run(tc.name+"/diff", func(t *testing.T) {
			if tc.name == "pivot" {
				t.Skip("DiffSchema does not rebuild a SQLite pivot")
			}
			db := testdb.Open(t, d)
			if err := migrate.AutoMigrate(db, append(tc.old, tag)); err != nil {
				t.Fatalf("AutoMigrate: %v", err)
			}
			changes, err := migrate.DiffSchema(context.Background(), db, append(tc.new, tag))
			if err != nil {
				t.Fatalf("DiffSchema: %v", err)
			}
			fkApply(t, db, changes)
			fkExec(t, db, "INSERT INTO skt (id, n) VALUES ('t1', 't')", "INSERT INTO skp (id, code) VALUES ('i1', 'i1')", tc.writeAfter)
		})
	}
}

// A table referencing a column the rebuilt table still holds unique is not
// stranded: the SQLite key change boots, and the reference still resolves.
func TestSQLiteKeyChangeKeepsUniqueReference(t *testing.T) {
	parent := func(key string) *entity.Entity {
		return entity.Define("skq", entity.EntityConfig{
			Table: "skq",
			Fields: []schema.Field{
				{Name: "id", Type: schema.String},
				{Name: "code", Type: schema.String},
				{Name: "slug", Type: schema.String, Unique: true},
			},
			PrimaryKey: []string{key},
		}.WithTimestamps(false))
	}
	db := testdb.Open(t, migrate.DialectSQLite)
	if err := migrate.AutoMigrate(db, fkReg{parent("id")}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	fkExec(t, db, "CREATE TABLE skh (s TEXT REFERENCES skq(slug))")
	if err := migrate.AutoMigrate(db, fkReg{parent("code")}); err != nil {
		t.Fatalf("key change with a reference onto a unique column: %v", err)
	}
	fkExec(t, db, "INSERT INTO skq (id, code, slug) VALUES ('i1', 'k1', 's1')", "INSERT INTO skh (s) VALUES ('s1')")
}

// A reference that names no columns follows the key, so a key change of
// the same width keeps it and one that widens the key strands it.
func TestSQLiteBareReferenceFollowsKey(t *testing.T) {
	parent := func(key ...string) *entity.Entity {
		return entity.Define("skw", entity.EntityConfig{
			Table: "skw",
			Fields: []schema.Field{
				{Name: "id", Type: schema.String},
				{Name: "code", Type: schema.String},
			},
			PrimaryKey: key,
		}.WithTimestamps(false))
	}
	db := testdb.Open(t, migrate.DialectSQLite)
	if err := migrate.AutoMigrate(db, fkReg{parent("id")}); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	fkExec(t, db, "CREATE TABLE skb (p TEXT REFERENCES skw)")
	if err := migrate.AutoMigrate(db, fkReg{parent("id", "code")}); err == nil || !strings.Contains(err.Error(), "skb") {
		t.Fatalf("widened key with a bare reference: err = %v, want a refusal naming skb", err)
	}
	if err := migrate.AutoMigrate(db, fkReg{parent("code")}); err != nil {
		t.Fatalf("same-width key change with a bare reference: %v", err)
	}
	fkExec(t, db, "INSERT INTO skw (id, code) VALUES ('i1', 'k1')", "INSERT INTO skb (p) VALUES ('k1')")
}
