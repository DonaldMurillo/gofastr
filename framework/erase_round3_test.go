package framework

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/datexport"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/teststorage"
)

// jsonEscapeAll spells every byte of s as a \u00XX escape, which any JSON
// decoder reads back as s.
func jsonEscapeAll(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		fmt.Fprintf(&b, `\u%04x`, s[i])
	}
	return b.String()
}

// A variants column may spell a key with \u escapes. The other user's row
// still names the object, so erasing u1 must leave it in storage.
func TestEraseSeesEscapedVariantKey(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		datexport.Reset(t)
		local := teststorage.New(t, t.TempDir())
		app := NewApp(WithDB(db), WithFileStorage(local))
		app.Registry.Register(entity.Define("profiles", entity.EntityConfig{
			Table: "profiles",
			Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{
				{Name: "avatar", Type: schema.Image},
				{Name: "avatar_variants", Type: schema.JSON},
				{Name: "owner_id", Type: schema.String},
			},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		ctx := context.Background()
		ff, err := file.ProcessFileField(ctx, local, strings.NewReader("shared"), "victim.png", "profiles", "avatar")
		if err != nil {
			t.Fatalf("ProcessFileField: %v", err)
		}
		key := ff.StorageRef
		ins := `INSERT INTO profiles (id, avatar, avatar_variants, owner_id) VALUES ($1, NULL, $2, $3)`
		if _, err := db.Exec(ins, "v", `[{"storage_ref":"`+jsonEscapeAll(key)+`"}]`, "u2"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(ins, "p1", `[{"storage_ref":"`+key+`"}]`, "u1"); err != nil {
			t.Fatal(err)
		}
		if _, err := app.EraseUserData(ctx, "u1"); err != nil {
			t.Fatalf("EraseUserData: %v", err)
		}
		mux := http.NewServeMux()
		mux.HandleFunc("/uploads/{key...}", upload.ServeHandler(local))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/uploads/"+key, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("u2's object deleted by u1's erasure: GET = %d, want 200", rec.Code)
		}
	})
}

// eraseBoth runs a dry run and then the erasure, and fails unless both
// succeed with the same total.
func eraseBoth(t *testing.T, app *App, user string) EraseReport {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dry, err := app.EraseUserData(ctx, user, WithEraseDryRun())
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	rep, err := app.EraseUserData(ctx, user)
	if err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	if dry.TotalErased() != rep.TotalErased() {
		t.Errorf("dry run counted %d rows, the erasure %d", dry.TotalErased(), rep.TotalErased())
	}
	return rep
}

// SQLite lets a TEXT primary key hold NULL. Such a row cannot be told apart
// by its key, so erasing it is refused, in the dry run too, and the staging
// still converges instead of re-adding the row on every pass.
func TestEraseRefusesNullKeyChild(t *testing.T) {
	db := openTestDB(t, migrate.DialectSQLite)
	app := NewApp(WithDB(db))
	app.Registry.Register(entity.Define("docs", entity.EntityConfig{
		Table:  "docs",
		Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
		Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
	}.WithTimestamps(false)))
	app.Table(migrate.Table{
		Name: "doc_tags",
		Columns: []migrate.Column{
			{Name: "tag", Type: schema.String, PrimaryKey: true},
			{Name: "doc_id", Type: schema.String},
		},
		ForeignKeys: []migrate.ForeignKey{{Columns: []string{"doc_id"}, RefTable: "docs"}},
	})
	if err := AutoMigrate(db, app.Registry); err != nil {
		t.Fatalf("AutoMigrate: %v", err)
	}
	cascadeExec(t, db,
		`INSERT INTO docs (id, owner_id) VALUES ('d1','u1')`,
		`INSERT INTO doc_tags (tag, doc_id) VALUES (NULL,'d1'), ('x','d1')`,
	)
	for _, opts := range [][]EraseOption{{WithEraseDryRun()}, nil} {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := app.EraseUserData(ctx, "u1", opts...)
		cancel()
		if err == nil || !strings.Contains(err.Error(), "NULL primary key") {
			t.Fatalf("EraseUserData(%d opts) = %v, want a refusal naming the NULL key", len(opts), err)
		}
	}
	wantIDs(t, db, "docs", "d1")
}

// A keyless child matched through a composite key whose first column a SET
// NULL relation also uses: the parent behind that relation must not be
// deleted first, or the child no longer matches and survives the count.
func TestEraseKeylessChildBeforeSetNull(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("a_stores", entity.EntityConfig{
			Table:  "a_stores",
			Scope:  &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{{Name: "owner_id", Type: schema.String}},
		}.WithTimestamps(false)))
		app.Registry.Register(entity.Define("z_docs", entity.EntityConfig{
			Table:      "z_docs",
			Scope:      &entity.ScopeConfig{OwnerField: "owner_id"},
			PrimaryKey: []string{"store", "no"},
			Fields: []schema.Field{
				{Name: "store", Type: schema.String, Required: true},
				{Name: "no", Type: schema.Int, Required: true},
				{Name: "owner_id", Type: schema.String},
			},
		}.WithTimestamps(false)))
		links := migrate.Table{
			Name: "doc_links",
			Columns: []migrate.Column{
				{Name: "store", Type: schema.String},
				{Name: "no", Type: schema.Int},
			},
			ForeignKeys: []migrate.ForeignKey{
				{Columns: []string{"store", "no"}, RefTable: "z_docs"},
				{Columns: []string{"store"}, RefTable: "a_stores"},
			},
		}.ToEntity()
		links.Config.Relations[1].OnDelete = entity.OnDeleteSetNull
		if err := app.Registry.Register(links); err != nil {
			t.Fatal(err)
		}
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cascadeExec(t, db,
			`INSERT INTO a_stores (id, owner_id) VALUES ('s1','u1')`,
			`INSERT INTO z_docs (store, no, owner_id) VALUES ('s1', 1, 'u1')`,
			`INSERT INTO doc_links (store, no) VALUES ('s1', 1)`,
		)
		rep := eraseBoth(t, app, "u1")
		if r, _ := cascadeResult(rep, "doc_links"); r.RowsAffected != 1 {
			t.Errorf("Cascaded[doc_links] = %+v, want 1 row", r)
		}
		if n := countRows(t, db, "doc_links"); n != 0 {
			t.Errorf("doc_links rows left = %d, want 0", n)
		}
	})
}

// Two tables naming each other, every row the user's: no table order
// deletes them, so the references between erased rows are cut first.
func TestEraseTableCycle(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := newCycleApp(t, db)
		seedCycle(t, db)
		cascadeExec(t, db,
			`INSERT INTO a_side (id, owner_id, b_id) VALUES ('a2','u2',NULL)`,
			`INSERT INTO b_side (id, owner_id, a_id) VALUES ('b2','u2','a2')`,
		)
		eraseBoth(t, app, "u1")
		wantIDs(t, db, "a_side", "a2")
		wantIDs(t, db, "b_side", "b2")
	})
}

// A self-reference declared ON DELETE RESTRICT, every row the user's: the
// erasure removes them all, and the dry run agrees.
func TestEraseRestrictSelfReference(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		app.Registry.Register(entity.Define("nodes", entity.EntityConfig{
			Table: "nodes",
			Scope: &entity.ScopeConfig{OwnerField: "owner_id"},
			Fields: []schema.Field{
				{Name: "owner_id", Type: schema.String},
				{Name: "parent_id", Type: schema.String},
			},
			Relations: []entity.Relation{
				entity.BelongsTo("parent", "nodes", "parent_id").OnDeleteAction(entity.OnDeleteRestrict),
			},
		}.WithTimestamps(false)))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cascadeExec(t, db,
			`INSERT INTO nodes (id, owner_id, parent_id) VALUES ('n1','u1',NULL)`,
			`INSERT INTO nodes (id, owner_id, parent_id) VALUES ('n2','u1','n1')`,
			`INSERT INTO nodes (id, owner_id, parent_id) VALUES ('n3','u1','n2')`,
			`INSERT INTO nodes (id, owner_id, parent_id) VALUES ('m1','u2',NULL)`,
		)
		eraseBoth(t, app, "u1")
		wantIDs(t, db, "nodes", "m1")
	})
}

// Two delete erasers reaching one row through different columns: the dry
// run counts it once, as the erasure deletes it once.
func TestEraseDryRunCountsSharedRowOnce(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		datexport.Reset(t)
		cascadeExec(t, db, `CREATE TABLE msgs (id TEXT PRIMARY KEY, sender TEXT, recipient TEXT)`,
			`INSERT INTO msgs (id, sender, recipient) VALUES ('m1','u1','u1'), ('m2','u1','u2'), ('m3','u2','u2')`)
		datexport.RegisterEraser(datexport.DataEraser{Name: "sent", Source: "test", Table: "msgs", Column: "sender", Mode: datexport.EraseDelete})
		datexport.RegisterEraser(datexport.DataEraser{Name: "received", Source: "test", Table: "msgs", Column: "recipient", Mode: datexport.EraseDelete})
		app := NewApp(WithDB(db))
		if rep := eraseBoth(t, app, "u1"); rep.TotalErased() != 2 {
			t.Errorf("erased %d rows, want 2", rep.TotalErased())
		}
		wantIDs(t, db, "msgs", "m3")
	})
}
