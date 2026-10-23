package framework

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// enrollment is a join table that started keyed on a generated id and
// later declares the pair it links as its key.
func enrollment(composite bool) *entity.Entity {
	cfg := entity.EntityConfig{
		Table: "enrollments",
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String, Required: true},
			{Name: "course_id", Type: schema.String, Required: true},
			{Name: "grade", Type: schema.String},
		},
	}.WithTimestamps(false)
	if composite {
		cfg.PrimaryKey = []string{"student_id", "course_id"}
	}
	return entity.Define("enrollment", cfg)
}

func enrollmentRegistry(t *testing.T, composite bool) *Registry {
	t.Helper()
	reg := NewRegistry()
	if err := reg.Register(enrollment(composite)); err != nil {
		t.Fatalf("register: %v", err)
	}
	return reg
}

func execAll(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, q := range stmts {
		if _, err := db.ExecContext(context.Background(), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
}

func enrollmentRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// A key change is destructive: ApplySchemaDiff refuses it, and with
// AllowDestructive the table moves to the new key with its rows intact.
func TestKeyChangeAppliesOnOptIn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		if err := AutoMigrate(db, enrollmentRegistry(t, false)); err != nil {
			t.Fatalf("AutoMigrate id-keyed: %v", err)
		}
		execAll(t, db,
			"INSERT INTO enrollments (id, student_id, course_id) VALUES ('r1', 's1', 'c1')",
			"INSERT INTO enrollments (id, student_id, course_id) VALUES ('r2', 's1', 'c2')",
		)
		reg := enrollmentRegistry(t, true)
		changes, err := DiffSchema(ctx, db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		var keyChange *migrate.SchemaChange
		for i, c := range changes {
			if strings.Contains(c.Summary, "change primary key (id) → (student_id, course_id)") {
				keyChange = &changes[i]
			}
		}
		if keyChange == nil || !keyChange.Destructive {
			t.Fatalf("no destructive key change in %+v", changes)
		}
		var refused *migrate.DestructiveChangeError
		if _, err := ApplySchemaDiff(ctx, db, changes); !errors.As(err, &refused) {
			t.Fatalf("ApplySchemaDiff without opt-in: err = %v, want a DestructiveChangeError", err)
		}
		if _, err := migrate.ApplySchemaDiffWithOptions(ctx, db, changes, migrate.ApplyOptions{AllowDestructive: true}); err != nil {
			t.Fatalf("apply with AllowDestructive: %v", err)
		}
		if n := enrollmentRows(t, db, "enrollments"); n != 2 {
			t.Fatalf("rows after the key change = %d, want 2", n)
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO enrollments (student_id, course_id) VALUES ('s1', 'c1')"); err == nil {
			t.Fatal("a repeated (student_id, course_id) pair was accepted after the key change")
		}
		execAll(t, db, "INSERT INTO enrollments (student_id, course_id) VALUES ('s2', 'c1')")
		if after, err := DiffSchema(ctx, db, reg); err != nil || len(after) != 0 {
			t.Fatalf("DiffSchema after the change = %+v, %v; want none", after, err)
		}
	})
}

// Rows that repeat the new key fail the change, and the table keeps its
// old key and its rows.
func TestKeyChangeRefusesRepeatedKeys(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		if err := AutoMigrate(db, enrollmentRegistry(t, false)); err != nil {
			t.Fatalf("AutoMigrate id-keyed: %v", err)
		}
		execAll(t, db,
			"INSERT INTO enrollments (id, student_id, course_id) VALUES ('r1', 's1', 'c1')",
			"INSERT INTO enrollments (id, student_id, course_id) VALUES ('r2', 's1', 'c1')",
		)
		changes, err := DiffSchema(ctx, db, enrollmentRegistry(t, true))
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if _, err := migrate.ApplySchemaDiffWithOptions(ctx, db, changes, migrate.ApplyOptions{AllowDestructive: true}); err == nil {
			t.Fatal("a key change over repeated pairs applied")
		}
		if n := enrollmentRows(t, db, "enrollments"); n != 2 {
			t.Fatalf("rows after the refused change = %d, want 2", n)
		}
		execAll(t, db, "INSERT INTO enrollments (id, student_id, course_id) VALUES ('r3', 's1', 'c1')")
	})
}

// Boot moves an empty table to the declared key, keeping the old key
// column (boot never drops one) and leaving it nullable.
func TestBootChangesEmptyTableKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		if err := AutoMigrate(db, enrollmentRegistry(t, false)); err != nil {
			t.Fatalf("AutoMigrate id-keyed: %v", err)
		}
		if err := AutoMigrate(db, enrollmentRegistry(t, true)); err != nil {
			t.Fatalf("AutoMigrate composite over an empty table: %v", err)
		}
		if _, ok := liveColumns(t, db, "enrollments")["id"]; !ok {
			t.Fatal("boot dropped the old key column")
		}
		execAll(t, db, "INSERT INTO enrollments (student_id, course_id) VALUES ('s1', 'c1')")
		if _, err := db.ExecContext(ctx, "INSERT INTO enrollments (student_id, course_id) VALUES ('s1', 'c1')"); err == nil {
			t.Fatal("a repeated pair was accepted after boot changed the key")
		}
		if err := AutoMigrate(db, enrollmentRegistry(t, true)); err != nil {
			t.Fatalf("a second boot: %v", err)
		}
	})
}

// A column that leaves the key and has no default loses the NOT NULL the
// key gave it; otherwise boot leaves a table that refuses every insert.
func TestBootKeyChangeFreesOldKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		cfg := entity.EntityConfig{
			Table:      "enrollments",
			Fields:     append(enrollment(true).Config.Fields, schema.Field{Name: "code", Type: schema.String}),
			PrimaryKey: []string{"code"},
		}.WithTimestamps(false)
		reg := NewRegistry()
		if err := reg.Register(entity.Define("enrollment", cfg)); err != nil {
			t.Fatalf("register: %v", err)
		}
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate code-keyed: %v", err)
		}
		if err := AutoMigrate(db, enrollmentRegistry(t, true)); err != nil {
			t.Fatalf("AutoMigrate composite: %v", err)
		}
		execAll(t, db, "INSERT INTO enrollments (student_id, course_id) VALUES ('s1', 'c1')")
	})
}

// Boot refuses to change the key of a table that has rows.
func TestBootRefusesPopulatedKeyChange(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if err := AutoMigrate(db, enrollmentRegistry(t, false)); err != nil {
			t.Fatalf("AutoMigrate id-keyed: %v", err)
		}
		execAll(t, db, "INSERT INTO enrollments (id, student_id, course_id) VALUES ('r1', 's1', 'c1')")
		err := AutoMigrate(db, enrollmentRegistry(t, true))
		if err == nil || !strings.Contains(err.Error(), "has rows") {
			t.Fatalf("AutoMigrate over a populated table: err = %v, want a refusal", err)
		}
		if n := enrollmentRows(t, db, "enrollments"); n != 1 {
			t.Fatalf("rows after the refused boot = %d, want 1", n)
		}
	})
}

// A table with rows and no primary key boots with its rows: nothing in it
// contradicts the declared key, so boot warns and leaves it.
func TestBootKeepsKeylessPopulatedTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		execAll(t, db,
			"CREATE TABLE enrollments (student_id TEXT NOT NULL, course_id TEXT NOT NULL, grade TEXT)",
			"INSERT INTO enrollments (student_id, course_id) VALUES ('s1', 'c1')",
		)
		if err := AutoMigrate(db, enrollmentRegistry(t, true)); err != nil {
			t.Fatalf("AutoMigrate over a keyless table with rows: %v", err)
		}
		if n := enrollmentRows(t, db, "enrollments"); n != 1 {
			t.Fatalf("rows after boot = %d, want 1", n)
		}
	})
}

// A generated migration carries the key change, from a snapshot that
// records keys. A snapshot written before keys were recorded generates no
// key change; boot is the check for those.
func TestGenerateKeyChange(t *testing.T) {
	for _, d := range []struct {
		dialect migrate.Dialect
		want    string
	}{
		{migrate.DialectPostgres, "ADD PRIMARY KEY (student_id, course_id)"},
		{migrate.DialectSQLite, "PRIMARY KEY (student_id, course_id)"},
	} {
		t.Run(fmt.Sprint(d.dialect), func(t *testing.T) {
			prev := migrate.SnapshotFromRegistry(enrollmentRegistry(t, false), d.dialect)
			if got := prev.PrimaryKeys["enrollments"]; len(got) != 1 || got[0] != "id" {
				t.Fatalf("snapshot primary key = %v, want [id]", got)
			}
			up, _, next, err := migrate.GenerateMigration(enrollmentRegistry(t, true), prev, d.dialect)
			if err != nil {
				t.Fatalf("GenerateMigration: %v", err)
			}
			if !strings.Contains(up, d.want) {
				t.Fatalf("up has no key change:\n%s", up)
			}
			if got := next.PrimaryKeys["enrollments"]; len(got) != 2 {
				t.Fatalf("next snapshot primary key = %v", got)
			}
			prev.PrimaryKeys = nil
			up, _, _, err = migrate.GenerateMigration(enrollmentRegistry(t, true), prev, d.dialect)
			if err != nil {
				t.Fatalf("GenerateMigration from an old snapshot: %v", err)
			}
			if strings.Contains(up, "PRIMARY KEY") {
				t.Fatalf("an old snapshot generated a key change:\n%s", up)
			}
		})
	}
}

// Two versions of one entity share one table, so they must declare the
// same key.
func TestVersionsShareOneKey(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), "same primary key") {
			t.Fatalf("recover() = %v, want a primary-key conflict", r)
		}
	}()
	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("enrollments", entity.EntityConfig{
		Table:  "enrollments",
		Fields: enrollment(false).Config.Fields,
	}.WithTimestamps(false))
	app.Entity("enrollmentsPaired", entity.EntityConfig{
		Table:      "enrollments",
		Fields:     enrollment(true).Config.Fields,
		PrimaryKey: []string{"student_id", "course_id"},
	}.WithTimestamps(false))
}

// Postgres folds unquoted names, so keys that differ only in case are one
// key.
func TestVersionsKeyCaseFolds(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	app.Entity("items", entity.EntityConfig{
		Table:      "items",
		Fields:     []schema.Field{{Name: "code", Type: schema.String}},
		PrimaryKey: []string{"code"},
	}.WithTimestamps(false))
	app.Entity("itemsCased", entity.EntityConfig{
		Table:      "items",
		Fields:     []schema.Field{{Name: "Code", Type: schema.String}},
		PrimaryKey: []string{"Code"},
	}.WithTimestamps(false))
}

// keyed registers one "items" entity with the given fields, key and renames.
func keyed(t *testing.T, fields []schema.Field, key []string, renames map[string]string) *Registry {
	t.Helper()
	reg := NewRegistry()
	cfg := entity.EntityConfig{Table: "items", Fields: fields, PrimaryKey: key, Renames: renames}.WithTimestamps(false)
	if err := reg.Register(entity.Define("item", cfg)); err != nil {
		t.Fatalf("register: %v", err)
	}
	return reg
}

func applyDestructive(t *testing.T, db *sql.DB, changes []migrate.SchemaChange) {
	t.Helper()
	if _, err := migrate.ApplySchemaDiffWithOptions(context.Background(), db, changes, migrate.ApplyOptions{AllowDestructive: true}); err != nil {
		t.Fatalf("apply %+v: %v", changes, err)
	}
}

// The diff reads the live key under the names the renames give it: a key
// column rename alone is no key change, and a rename beside a key change
// alters the renamed column.
func TestKeyChangeFollowsRenames(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		ctx := context.Background()
		str := func(n string) schema.Field { return schema.Field{Name: n, Type: schema.String, Required: true} }
		if err := AutoMigrate(db, keyed(t, []schema.Field{str("code"), str("bin")}, []string{"code"}, nil)); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		execAll(t, db, "INSERT INTO items (code, bin) VALUES ('a', 'x')")

		changes, err := DiffSchema(ctx, db, keyed(t, []schema.Field{str("sku"), str("bin")}, []string{"sku"}, map[string]string{"code": "sku"}))
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		for _, c := range changes {
			if c.Destructive {
				t.Fatalf("a key column rename diffed as destructive: %+v", changes)
			}
		}
		applyDestructive(t, db, changes)

		changes, err = DiffSchema(ctx, db, keyed(t, []schema.Field{str("ref"), str("bin")}, []string{"ref", "bin"}, map[string]string{"sku": "ref"}))
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		applyDestructive(t, db, changes)
		if _, err := db.ExecContext(ctx, "INSERT INTO items (ref, bin) VALUES ('a', 'x')"); err == nil {
			t.Fatal("a repeated (ref, bin) pair was accepted")
		}
		execAll(t, db, "INSERT INTO items (ref, bin) VALUES ('a', 'y')")
	})
}

// A Postgres key change's Down restores the old key and the nullability
// the new key took away.
func TestKeyChangeDownRestoresKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, d Dialect) {
		if d != migrate.DialectPostgres {
			t.Skip("SQLite rebuilds the table and ships no Down")
		}
		ctx := context.Background()
		fields := []schema.Field{{Name: "a", Type: schema.String, Required: true}, {Name: "b", Type: schema.String}}
		if err := AutoMigrate(db, keyed(t, fields, []string{"a"}, nil)); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		execAll(t, db, "INSERT INTO items (a, b) VALUES ('1', 'x')")
		changes, err := DiffSchema(ctx, db, keyed(t, fields, []string{"b"}, nil))
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		applyDestructive(t, db, changes)
		for _, c := range slices.Backward(changes) {
			if c.Down == "" {
				t.Fatalf("no Down for %q", c.Summary)
			}
			execAll(t, db, c.Down)
		}
		execAll(t, db, "INSERT INTO items (a) VALUES ('2')")
		if _, err := db.ExecContext(ctx, "INSERT INTO items (a) VALUES ('1')"); err == nil {
			t.Fatal("after the Down a repeated a was accepted")
		}
	})
}

// Moving off a key column the entity no longer declares drops the column
// and its values, so the generated migration has no Down: re-adding an
// empty id beside the new key would roll back to neither schema.
func TestKeyChangeOffDroppedColumnHasNoDown(t *testing.T) {
	for _, d := range []migrate.Dialect{migrate.DialectPostgres, migrate.DialectSQLite} {
		t.Run(fmt.Sprint(d), func(t *testing.T) {
			prev := migrate.SnapshotFromRegistry(enrollmentRegistry(t, false), d)
			up, down, _, err := migrate.GenerateMigration(enrollmentRegistry(t, true), prev, d)
			if err != nil {
				t.Fatalf("GenerateMigration: %v", err)
			}
			if !strings.Contains(up, "PRIMARY KEY (student_id, course_id)") {
				t.Fatalf("up has no key change:\n%s", up)
			}
			if down != "" {
				t.Fatalf("migration off a dropped id has a Down:\n%s", down)
			}
		})
	}
}

// EagerLoad names a mixed-case key the way the DDL wrote it, unquoted, so
// Postgres folds both to the same column.
func TestEagerLoadMixedCaseKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		skus := entity.Define("skus", entity.EntityConfig{
			Fields:     []schema.Field{{Name: "Sku", Type: schema.String}, {Name: "name", Type: schema.String}},
			PrimaryKey: []string{"Sku"},
		}.WithTimestamps(false))
		lines := entity.Define("lines", entity.EntityConfig{
			Fields:     []schema.Field{{Name: "Code", Type: schema.String}, {Name: "sku_id", Type: schema.String}},
			PrimaryKey: []string{"Code"},
			Relations:  []entity.Relation{entity.BelongsTo("sku", "skus", "sku_id")},
		}.WithTimestamps(false))
		for _, e := range []*entity.Entity{skus, lines} {
			if err := reg.Register(e); err != nil {
				t.Fatalf("register: %v", err)
			}
		}
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		execAll(t, db,
			"INSERT INTO skus (sku, name) VALUES ('k1', 'Bolt')",
			"INSERT INTO lines (code, sku_id) VALUES ('l1', 'k1')",
		)
		got, err := EagerLoad(context.Background(), db, lines, lines.Config.Relations, []string{"l1"}, reg)
		if err != nil {
			t.Fatalf("EagerLoad: %v", err)
		}
		// SQLite reports the column as declared, Postgres folded.
		sku, _ := got["l1"]["sku"].(map[string]any)
		var key any
		for k, v := range sku {
			if strings.EqualFold(k, "sku") {
				key = v
			}
		}
		if sku["name"] != "Bolt" || key != "k1" {
			t.Fatalf("belongs_to on a mixed-case key = %v, want the Bolt row with its key", got["l1"]["sku"])
		}
	})
}

// Two entities on one table render the same key change; when only one of
// them drops the old key column, the migration still has no Down.
func TestSharedTableKeyChangeHasNoDown(t *testing.T) {
	opt := func(n string) schema.Field { return schema.Field{Name: n, Type: schema.String} }
	prev := migrate.SnapshotFromRegistry(keyed(t, []schema.Field{opt("code"), opt("b")}, []string{"code"}, nil), migrate.DialectPostgres)
	reg := NewRegistry()
	for name, fields := range map[string][]schema.Field{"a_full": {opt("code"), opt("b")}, "z_partial": {opt("b")}} {
		cfg := entity.EntityConfig{Table: "items", Fields: fields, PrimaryKey: []string{"b"}}.WithTimestamps(false)
		if err := reg.Register(entity.Define(name, cfg)); err != nil {
			t.Fatalf("register %s: %v", name, err)
		}
	}
	up, down, _, err := migrate.GenerateMigration(reg, prev, migrate.DialectPostgres)
	if err != nil {
		t.Fatalf("GenerateMigration: %v", err)
	}
	if !strings.Contains(up, "DROP COLUMN code") {
		t.Fatalf("z_partial's diff should drop code:\n%s", up)
	}
	if down != "" {
		t.Fatalf("migration dropping the old key column has a Down:\n%s", down)
	}
}

// A key change that keeps every old key column keeps its Down.
func TestKeyChangeKeepingColumnsHasDown(t *testing.T) {
	fields := []schema.Field{{Name: "a", Type: schema.String, Required: true}, {Name: "b", Type: schema.String}}
	prev := migrate.SnapshotFromRegistry(keyed(t, fields, []string{"a"}, nil), migrate.DialectPostgres)
	_, down, _, err := migrate.GenerateMigration(keyed(t, fields, []string{"a", "b"}, nil), prev, migrate.DialectPostgres)
	if err != nil {
		t.Fatalf("GenerateMigration: %v", err)
	}
	if !strings.Contains(down, "ADD PRIMARY KEY (a)") {
		t.Fatalf("down does not restore the key:\n%s", down)
	}
}

// A key that turns composite while another entity holds a foreign key to
// it is refused before anything changes, for an existing child too.
func TestCompositeBelongsToTargetRefused(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		withChild := func(composite bool) *Registry {
			reg := enrollmentRegistry(t, composite)
			grade := entity.Define("grade", entity.EntityConfig{
				Table:     "grades",
				Fields:    []schema.Field{{Name: "enrollment_id", Type: schema.String}},
				Relations: []entity.Relation{entity.BelongsTo("enrollment", "enrollment", "enrollment_id")},
			}.WithTimestamps(false))
			if err := reg.Register(grade); err != nil {
				t.Fatalf("register: %v", err)
			}
			return reg
		}
		if err := AutoMigrate(db, withChild(false)); err != nil {
			t.Fatalf("AutoMigrate id-keyed: %v", err)
		}
		if err := AutoMigrate(db, withChild(true)); !errors.Is(err, entity.ErrCompositeKey) {
			t.Fatalf("AutoMigrate: err = %v, want ErrCompositeKey", err)
		}
		if _, err := DiffSchema(context.Background(), db, withChild(true)); !errors.Is(err, entity.ErrCompositeKey) {
			t.Fatalf("DiffSchema: err = %v, want ErrCompositeKey", err)
		}
	})
}
