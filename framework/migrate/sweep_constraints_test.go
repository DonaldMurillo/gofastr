package migrate

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func sweepMemDB(t *testing.T, ddl ...string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, s := range ddl {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%v\n%s", err, s)
		}
	}
	return db
}

func sweepEnt(name string, cfg entity.EntityConfig) *entity.Entity {
	if cfg.Table == "" {
		cfg.Table = name
	}
	return entity.Define(name, cfg.WithTimestamps(false))
}

func TestSweepConstraintsBootRefusesPopulatedKeyChange(t *testing.T) {
	db := sweepMemDB(t,
		`CREATE TABLE pk_rows (id TEXT NOT NULL PRIMARY KEY, code TEXT NOT NULL)`,
		`INSERT INTO pk_rows VALUES ('a', 'x')`,
	)
	ent := sweepEnt("pk_rows", entity.EntityConfig{
		Fields:     []schema.Field{{Name: "code", Type: schema.String, Required: true}},
		PrimaryKey: []string{"code"},
	})
	err := AutoMigrate(db, testReg{"pk_rows": ent})
	if err == nil || !strings.Contains(err.Error(), "has rows") {
		t.Fatalf("boot changed the key of a populated table: %v", err)
	}
}

func TestSweepConstraintsDefaultPKName(t *testing.T) {
	long := strings.Repeat("t", 70)
	if got := defaultPKName("Aux." + long); got != strings.Repeat("t", 58)+"_pkey" {
		t.Errorf("defaultPKName = %q", got)
	}
	if got := defaultPKName("Widgets"); got != "widgets_pkey" {
		t.Errorf("defaultPKName = %q", got)
	}
}

func TestSweepConstraintsSameKeyFoldsCaseKeepsOrder(t *testing.T) {
	if !sameKey([]string{"ID", "Org"}, []string{"id", "org"}) {
		t.Error("sameKey is case-sensitive")
	}
	if sameKey([]string{"id", "org"}, []string{"org", "id"}) {
		t.Error("sameKey ignores key order")
	}
}

func sweepKeyEnts() (*entity.Entity, *entity.Entity, map[string]*entity.Entity) {
	parent := sweepEnt("kp", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "code", Type: schema.String},
			{Name: "keep", Type: schema.String, Required: true},
		},
		PrimaryKey: []string{"code"},
	})
	child := sweepEnt("kc", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "p_code", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("kp", "kp", "p_code")},
	})
	return parent, child, map[string]*entity.Entity{"kp": parent, "kc": child}
}

func TestSweepConstraintsPostgresKeyChangeSQL(t *testing.T) {
	parent, _, all := sweepKeyEnts()
	lk := liveKey{cols: []string{"id"}, constraint: "kp_pkey"}
	ch, err := postgresKeyChange(parent, all, "kp", lk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"DROP CONSTRAINT \"kp_pkey\"", "ALTER COLUMN id DROP NOT NULL", "ADD PRIMARY KEY (code)"} {
		if !strings.Contains(ch.SQL, want) {
			t.Errorf("up lacks %q:\n%s", want, ch.SQL)
		}
	}
	if ch.Down != "" {
		t.Errorf("an old key column the entity no longer declares still got a Down:\n%s", ch.Down)
	}

	lk = liveKey{cols: []string{"keep"}, constraint: "kp_pkey"}
	ch, err = postgresKeyChange(parent, all, "kp", lk, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ch.SQL, "keep DROP NOT NULL") {
		t.Errorf("a Required column leaving the key lost its NOT NULL:\n%s", ch.SQL)
	}
	for _, want := range []string{"DROP CONSTRAINT kp_pkey", "ALTER COLUMN code DROP NOT NULL", "ADD PRIMARY KEY (keep)"} {
		if !strings.Contains(ch.Down, want) {
			t.Errorf("down lacks %q:\n%s", want, ch.Down)
		}
	}
}

func TestSweepConstraintsKeyChangeDetachIsIrreversible(t *testing.T) {
	parent, _, all := sweepKeyEnts()
	tables := map[string]map[string]string{"kc": {"id": "TEXT", "p_code": "TEXT"}}
	ch, err := postgresKeyChange(parent, all, "kp", liveKey{cols: []string{"keep"}, constraint: "kp_pkey"}, tables, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ch.SQL, "DO $$") || !strings.Contains(ch.SQL, "ALTER TABLE kc ADD") {
		t.Fatalf("the change neither detaches nor re-adds the child key:\n%s", ch.SQL)
	}
	if ch.Down != "" || !ch.irreversible {
		t.Errorf("a change that detached foreign keys kept a Down:\n%s", ch.Down)
	}
}

func TestSweepConstraintsDetachRefusesUndeclaredKey(t *testing.T) {
	block := detachForeignKeysSQL("kp", []string{"kc"}, []string{"kc:p_code"})
	if !strings.Contains(block, "RAISE EXCEPTION") || !strings.Contains(block, "'kc:p_code'") || !strings.Contains(block, "pk.contype = 'p'") {
		t.Errorf("the detach block does not refuse a key outside keep:\n%s", block)
	}
}

func TestSweepConstraintsPostgresForeignKeySwapAndDrop(t *testing.T) {
	parent := sweepEnt("fp", entity.EntityConfig{Fields: []schema.Field{{Name: "n", Type: schema.String}}})
	child := sweepEnt("fc", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "p_id", Type: schema.String}, {Name: "q", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("fp", "fp", "p_id").OnDeleteAction(entity.OnDeleteCascade)},
	})
	all := map[string]*entity.Entity{"fp": parent, "fc": child}
	env := &fkDiffEnv{fks: map[string][]liveFK{"fc": {
		{name: "fc_p_id_fkey", cols: []string{"p_id"}, refTable: "fp", refCols: []string{"id"}, onDelete: "NO ACTION"},
		{name: "fc_q_fkey", cols: []string{"q"}, refTable: "fp", refCols: []string{"id"}, onDelete: "NO ACTION"},
	}}}
	changes, differ, err := foreignKeyChanges(child, all, DialectPostgres, "fc", env, nil)
	if err != nil || !differ || len(changes) != 2 {
		t.Fatalf("changes=%+v differ=%v err=%v", changes, differ, err)
	}
	swap, drop := changes[0], changes[1]
	if !strings.Contains(swap.SQL, `DROP CONSTRAINT IF EXISTS "fc_p_id_fkey"`) || !strings.Contains(swap.SQL, "ADD") || swap.Destructive || !swap.irreversible {
		t.Errorf("a redefined key is not one irreversible swap: %+v", swap)
	}
	if !strings.Contains(drop.SQL, `"fc_q_fkey"`) || !drop.Destructive {
		t.Errorf("a key no relation declares is not a destructive drop: %+v", drop)
	}
}

func TestSweepConstraintsKeyChangedParentKeepsChildKeys(t *testing.T) {
	parent, child, all := sweepKeyEnts()
	_ = parent
	env := &fkDiffEnv{
		fks:        map[string][]liveFK{"kc": {{name: "kc_p_code_fkey", cols: []string{"p_code"}, refTable: "kp", refCols: []string{"id"}, onDelete: "NO ACTION"}}},
		keyChanged: map[string][]string{"kp": {"id"}},
	}
	changes, _, err := foreignKeyChanges(child, all, DialectPostgres, "kc", env, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 0 {
		t.Errorf("a key the parent's key change detaches and re-adds diffed as %+v", changes)
	}
}

func TestSweepConstraintsIncomingKeysReaddOrDefer(t *testing.T) {
	parent, _, all := sweepKeyEnts()
	in, err := incomingForeignKeys(parent, all, map[string]map[string]string{"kc": {"id": "TEXT", "p_code": "TEXT"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.readd) != 1 || len(in.deferred) != 0 || in.children[0] != "kc" {
		t.Errorf("a child holding the columns is not re-added at once: %+v", in)
	}
	in, err = incomingForeignKeys(parent, all, map[string]map[string]string{"kc": {"id": "TEXT"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.readd) != 0 || len(in.deferred) != 1 {
		t.Errorf("a child lacking the columns is not deferred: %+v", in)
	}
}

func TestSweepConstraintsPivotKeyNeedsItsColumns(t *testing.T) {
	tag := sweepEnt("ptag", entity.EntityConfig{Fields: []schema.Field{{Name: "n", Type: schema.String}}})
	post := sweepEnt("ppost", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "n", Type: schema.String}},
		Relations: []entity.Relation{entity.ManyToMany("ptag", "ptag", "ppost_ptag", "ppost_id", "ptag_id")},
	})
	all := map[string]*entity.Entity{"ptag": tag, "ppost": post}
	in, err := incomingForeignKeys(tag, all, map[string]map[string]string{"ppost_ptag": {"other": "TEXT"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(in.readd)+len(in.deferred) != 0 {
		t.Errorf("a pivot key with no column to sit on is re-added: %+v", in)
	}
}

func TestSweepConstraintsPostgresRefNamesItsSchema(t *testing.T) {
	other := liveFK{refTable: "other.parents", refSchema: "other", refCurrent: false}
	if refIsTable(other, "parents", DialectPostgres) {
		t.Error("a key onto other.parents matched the bare parents")
	}
	if !refIsTable(other, "other.parents", DialectPostgres) {
		t.Error("a key onto other.parents missed other.parents")
	}
	if refIsTable(other, "aux.parents", DialectPostgres) {
		t.Error("a key onto other.parents matched aux.parents")
	}
	if !refIsTable(liveFK{refTable: "parents", refSchema: "public", refCurrent: true}, "parents", DialectPostgres) {
		t.Error("a key in the search-path schema missed the bare table")
	}
}

func TestSweepConstraintsChildRenameKeepsKey(t *testing.T) {
	db := sweepMemDB(t, `PRAGMA foreign_keys=on`)
	parent := sweepEnt("rp", entity.EntityConfig{Fields: []schema.Field{{Name: "n", Type: schema.String}}})
	child := func(col string, renames map[string]string) *entity.Entity {
		return sweepEnt("rc", entity.EntityConfig{
			Fields:    []schema.Field{{Name: col, Type: schema.String}},
			Relations: []entity.Relation{entity.BelongsTo("rp", "rp", col)},
			Renames:   renames,
		})
	}
	if err := AutoMigrate(db, testReg{"rp": parent, "rc": child("owner", nil)}); err != nil {
		t.Fatal(err)
	}
	changes, err := DiffSchema(context.Background(), db, testReg{"rp": parent, "rc": child("p_id", map[string]string{"owner": "p_id"})})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if strings.Contains(c.Summary, "foreign key") {
			t.Errorf("renaming the child's key column reported a foreign key change: %s", c.Summary)
		}
	}
}

func TestSweepConstraintsBareReferenceReadsParentKey(t *testing.T) {
	db := sweepMemDB(t,
		`CREATE TABLE bp (code TEXT NOT NULL PRIMARY KEY)`,
		`CREATE TABLE bc (id TEXT NOT NULL PRIMARY KEY, p TEXT REFERENCES bp)`,
	)
	fks, err := readLiveForeignKeysQ(context.Background(), db, []string{"bc"}, DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if got := fks["bc"]; len(got) != 1 || len(got[0].refCols) != 1 || got[0].refCols[0] != "code" {
		t.Errorf("REFERENCES bp read as %+v, want refCols [code]", got)
	}
}

func TestSweepConstraintsUniqueIndexNameClashFails(t *testing.T) {
	db := sweepMemDB(t,
		`CREATE TABLE ut (id TEXT NOT NULL PRIMARY KEY, a_b TEXT)`,
		`CREATE TABLE ut_a (id TEXT NOT NULL PRIMARY KEY, b TEXT)`,
		`CREATE UNIQUE INDEX ut_a_b_key ON ut_a (b)`,
	)
	ent := sweepEnt("ut", entity.EntityConfig{Fields: []schema.Field{{Name: "a_b", Type: schema.String, Unique: true}}})
	if err := ensureUniqueIndexes(context.Background(), db, ent, DialectSQLite); err == nil {
		t.Fatal("boot left ut.a_b taking duplicates because another table holds an index of its name")
	}
}

func TestSweepConstraintsPartialOrExpressionUniqueCoversNothing(t *testing.T) {
	db := sweepMemDB(t,
		`CREATE TABLE pu (id INTEGER PRIMARY KEY, email TEXT, name TEXT)`,
		`CREATE UNIQUE INDEX pu_email ON pu (email) WHERE email <> ''`,
		`CREATE UNIQUE INDEX pu_name ON pu (lower(name))`,
	)
	got, err := liveUniqueColumns(context.Background(), db, "pu", DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("a partial or expression unique index counted as covering a column: %v", got)
	}
}

func TestSweepConstraintsQuotedIndexNames(t *testing.T) {
	db := sweepMemDB(t,
		`ATTACH DATABASE ':memory:' AS aux`,
		`CREATE TABLE aux.ix (id INTEGER PRIMARY KEY, label TEXT, note TEXT)`,
		`CREATE INDEX aux."note's.index" ON ix (note)`,
		`CREATE UNIQUE INDEX aux."label-name" ON ix (label)`,
		`CREATE UNIQUE INDEX aux."label.expression" ON ix (lower(label))`,
	)
	ctx := context.Background()
	ent := rawEnt("ix", "aux.ix", []schema.Field{
		{Name: "id", Type: schema.Int},
		{Name: "label", Type: schema.String},
	}, nil, "id")
	changes, err := DiffSchema(ctx, db, testReg{"ix": ent})
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].sqliteRebuildOf != "ix" || !strings.Contains(changes[0].SQL, "DROP COLUMN aux.ix.note by table rebuild") {
		t.Fatalf("expected a rebuild to drop the indexed column, got %+v", changes)
	}
	unique, err := liveUniqueColumns(ctx, db, "aux.ix", DialectSQLite)
	if err != nil {
		t.Fatal(err)
	}
	if len(unique) != 1 || !unique["label"] {
		t.Fatalf("expected only label to be covered by unique indexes, got %v", unique)
	}
}

func TestSweepConstraintsStaleUniqueSkipsDeclaredAlone(t *testing.T) {
	ent := sweepEnt("su", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "code", Type: schema.String},
			{Name: "email", Type: schema.String},
			{Name: "org", Type: schema.String},
			{Name: "user_id", Type: schema.String},
		},
		PrimaryKey: []string{"code"},
		Indices: []entity.Index{
			{Columns: []string{"email"}, Unique: true},
			{Columns: []string{"code", "org"}, Unique: true},
			{Columns: []string{"email", "org"}, Unique: true},
			{Columns: []string{"user_id", "org"}, Unique: true},
			{Columns: []string{"user_id", "email"}, Unique: true},
		},
	})
	got := staleUniqueColumns(ent, map[string]bool{"code": true, "email": true, "user_id": true}, DialectSQLite)
	if len(got) != 1 || got[0] != "user_id" {
		t.Errorf("staleUniqueColumns = %v, want [user_id] once", got)
	}
}
