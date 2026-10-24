package crud

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestKeyColsDefaultToEntityKey(t *testing.T) {
	ch := &CrudHandler{Entity: &entity.Entity{Config: entity.EntityConfig{PrimaryKey: []string{"code"}}}}
	if cond, _ := ch.keyCond([]any{"x"}); cond != "code = $1" {
		t.Errorf("keyCond = %q, want the entity's key column", cond)
	}
	// Includes, nested filters and create cascades join on keyColumn.
	if col, err := ch.keyColumn(); err != nil || col != "code" {
		t.Errorf("keyColumn = %q, %v; want the entity's key column", col, err)
	}
}

func TestAuditRecordIDForGuards(t *testing.T) {
	ctx := context.WithValue(context.Background(), auditRecordKey{}, auditRecord{entity: "posts", id: "7"})
	if id, ok := AuditRecordIDFor(ctx, "posts"); !ok || id != "7" {
		t.Fatalf("matching entity: got %q %v", id, ok)
	}
	if id, ok := AuditRecordIDFor(ctx, "users"); ok || id != "" {
		t.Errorf("other entity: got %q %v, want none", id, ok)
	}
	empty := context.WithValue(context.Background(), auditRecordKey{}, auditRecord{entity: "posts"})
	if id, ok := AuditRecordIDFor(empty, "posts"); ok || id != "" {
		t.Errorf("empty id: got %q %v, want none", id, ok)
	}
}

func TestKeyCondNamesEachColumn(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: []string{"code"}}
	cond, args := ch.keyCond([]any{"x"})
	if cond != "code = $1" || !slices.Equal(args, []any{"x"}) {
		t.Errorf("keyCond = %q %v", cond, args)
	}
}

func TestKeyCondMismatchMatchesNothing(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: []string{"code"}}
	for _, vals := range [][]any{nil, {"a", "b"}} {
		if cond, args := ch.keyCond(vals); cond != "1 = 0" || args != nil {
			t.Errorf("keyCond(%v) = %q %v, want a condition matching nothing", vals, cond, args)
		}
	}
}

// A keyless table's rows have no name: no key values, and a condition that
// matches nothing rather than an empty WHERE.
func TestKeylessTableNamesNoRow(t *testing.T) {
	ch := &CrudHandler{Entity: &entity.Entity{Config: entity.EntityConfig{
		PrimaryKey: []string{},
		Fields:     []schema.Field{{Name: "id", Type: schema.String}},
	}}}
	if vals, ok := ch.rowKeyVals(map[string]any{"id": "x"}); ok {
		t.Errorf("rowKeyVals on a keyless table = %v, true", vals)
	}
	if cond, args := ch.keyCond(nil); cond != "1 = 0" || args != nil {
		t.Errorf("keyCond on a keyless table = %q %v", cond, args)
	}
	// Every lookup by id goes through keyWhere: a keyless table answers
	// not-found rather than panicking on its missing key column.
	if cond, args := ch.keyWhere("x"); cond != "1 = 0" || args != nil {
		t.Errorf("keyWhere on a keyless table = %q %v", cond, args)
	}
}

func TestRowKeyValsReadsWireOrColumn(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: []string{"user_code"}, JSONCase: CaseCamel}
	for _, row := range []map[string]any{{"userCode": "u1"}, {"user_code": "u1"}} {
		vals, ok := ch.rowKeyVals(row)
		if !ok || !slices.Equal(vals, []any{"u1"}) {
			t.Errorf("rowKeyVals(%v) = %v, %v", row, vals, ok)
		}
	}
	if _, ok := ch.rowKeyVals(map[string]any{"userCode": nil}); ok {
		t.Error("a nil key value must not name a row")
	}
}

func TestKeyPayloadUsesWireKey(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: []string{"user_code"}, JSONCase: CaseCamel}
	got := ch.keyPayload("u1")
	if len(got) != 1 || got["userCode"] != "u1" {
		t.Errorf("keyPayload = %v", got)
	}
}

// A file key already on a row passes the media check only for the row's
// own id: a second spelling of that id answers as an id naming no record
// does, so it cannot probe which keys a row holds.
func TestMediaCheckReadsOwnID(t *testing.T) {
	db := setupDB(t, `CREATE TABLE docs (code TEXT COLLATE NOCASE, scope TEXT, doc TEXT, PRIMARY KEY (code, scope))`)
	if _, err := db.Exec(`INSERT INTO docs (code, scope, doc) VALUES ('abc', 's', 'media/a.pdf')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("doc", entity.EntityConfig{
		Table: "docs",
		Fields: []schema.Field{
			{Name: "code", Type: schema.String, Required: true},
			{Name: "scope", Type: schema.String, Required: true},
			{Name: "doc", Type: schema.File},
		},
		PrimaryKey: []string{"code", "scope"},
		Unmanaged:  true,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)
	body := func() map[string]any { return map[string]any{"doc": "media/a.pdf"} }
	if _, err := ch.UpdateOne(t.Context(), "abc/s", body()); err != nil {
		t.Fatalf("the row's own id refused its own file key: %v", err)
	}
	_, absent := ch.UpdateOne(t.Context(), "zzz/s", body())
	_, alias := ch.UpdateOne(t.Context(), "ABC/s", body())
	if absent == nil || alias == nil || absent.Error() != alias.Error() {
		t.Errorf("ABC/s answered %v; an id naming no record answers %v", alias, absent)
	}
}

// A row another write swaps in under a second spelling of the id, after
// the id was read and before the statement runs, is not the record the
// id names: the statement checks the row it reached, not an earlier one.
func TestKeyCheckReadsReachedRow(t *testing.T) {
	db := setupDB(t, `CREATE TABLE codes (code TEXT COLLATE NOCASE, scope TEXT, label TEXT, PRIMARY KEY (code, scope))`)
	if _, err := db.Exec(`INSERT INTO codes (code, scope, label) VALUES ('abc', 's', 'public')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("code", entity.EntityConfig{
		Table: "codes",
		Fields: []schema.Field{
			{Name: "code", Type: schema.String, Required: true},
			{Name: "scope", Type: schema.String, Required: true},
			{Name: "label", Type: schema.String},
		},
		PrimaryKey: []string{"code", "scope"},
		Unmanaged:  true,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)
	if row, err := ch.GetOne(t.Context(), "abc/s", nil); err != nil || row["label"] != "public" {
		t.Fatalf("GetOne(abc/s) = %v, %v; the swap below proves nothing", row, err)
	}
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeGet, func(ctx context.Context, _ any) error {
		_, err := db.ExecContext(ctx, `DELETE FROM codes; INSERT INTO codes (code, scope, label) VALUES ('ABC', 's', 'private')`)
		return err
	})
	row, err := ch.GetOne(WithReadHooks(t.Context()), "abc/s", nil)
	if !errors.Is(err, errNotFound) {
		t.Errorf("GetOne(abc/s) after the swap = %v, %v; want not found", row, err)
	}
}

// A SQLite key declared under a stricter collation than its column holds
// two rows that equality on the column matches together. A read picks the
// id's own row; a write that would reach both is refused whole.
func TestKeyStricterThanColumn(t *testing.T) {
	db := setupDB(t, `CREATE TABLE codes (code TEXT COLLATE NOCASE, scope TEXT, label TEXT, PRIMARY KEY (code COLLATE BINARY, scope))`)
	if _, err := db.Exec(`INSERT INTO codes (code, scope, label) VALUES ('abc', 's', 'lower'), ('ABC', 's', 'upper')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("code", entity.EntityConfig{
		Table: "codes",
		Fields: []schema.Field{
			{Name: "code", Type: schema.String, Required: true},
			{Name: "scope", Type: schema.String, Required: true},
			{Name: "label", Type: schema.String},
		},
		PrimaryKey: []string{"code", "scope"},
		Unmanaged:  true,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)
	for id, want := range map[string]string{"abc/s": "lower", "ABC/s": "upper"} {
		if row, err := ch.GetOne(t.Context(), id, nil); err != nil || row["label"] != want {
			t.Errorf("GetOne(%s) = %v, %v; want label %q", id, row, err, want)
		}
	}
	if _, err := ch.UpdateOne(t.Context(), "abc/s", map[string]any{"label": "changed"}); !errors.Is(err, errNotFound) {
		t.Errorf("UpdateOne(abc/s) reaching both rows = %v; want not found", err)
	}
	if err := ch.DeleteOne(t.Context(), "abc/s"); !errors.Is(err, errNotFound) {
		t.Errorf("DeleteOne(abc/s) reaching both rows = %v; want not found", err)
	}
	var labels string
	if err := db.QueryRow(`SELECT group_concat(label, ',') FROM (SELECT label FROM codes ORDER BY label)`).Scan(&labels); err != nil || labels != "lower,upper" {
		t.Errorf("labels after the refused writes = %q, %v; want both rows untouched", labels, err)
	}
}

// SQLite cannot return rows from a virtual table, so a delete by id on an
// FTS5 index checks the rows it names before it runs, not through
// RETURNING.
func TestDeleteOnVirtualTable(t *testing.T) {
	db := setupDB(t, `CREATE VIRTUAL TABLE notes USING fts5(slug, body)`)
	if _, err := db.Exec(`INSERT INTO notes (slug, body) VALUES ('a', 'first'), ('b', 'second')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("note", entity.EntityConfig{
		Table: "notes",
		Fields: []schema.Field{
			{Name: "slug", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
		PrimaryKey: []string{"slug"},
		Unmanaged:  true,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)
	if err := ch.DeleteOne(t.Context(), "a"); err != nil {
		t.Fatalf("DeleteOne(a) on an FTS5 table: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM notes`).Scan(&n); err != nil || n != 1 {
		t.Errorf("rows after the delete = %d, %v; want 1", n, err)
	}
}

// The rows a SQLite delete checks are the ones its own WHERE reaches: a
// trashed row under another spelling of the id blocks neither the live
// row's soft delete nor, once trashed, a purge of the other.
func TestDeleteCheckUsesWriteScope(t *testing.T) {
	db := setupDB(t, `CREATE TABLE codes (code TEXT COLLATE NOCASE, scope TEXT, deleted_at DATETIME, PRIMARY KEY (code COLLATE BINARY, scope))`)
	if _, err := db.Exec(`INSERT INTO codes (code, scope, deleted_at) VALUES ('abc', 's', NULL), ('ABC', 's', '2026-01-01')`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("code", entity.EntityConfig{
		Table: "codes",
		Fields: []schema.Field{
			{Name: "code", Type: schema.String, Required: true},
			{Name: "scope", Type: schema.String, Required: true},
		},
		PrimaryKey: []string{"code", "scope"},
		Unmanaged:  true,
		Scope:      &entity.ScopeConfig{SoftDelete: true},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db)
	if err := ch.DeleteOne(t.Context(), "abc/s"); err != nil {
		t.Errorf("DeleteOne(abc/s) beside a trashed ABC/s: %v", err)
	}
	if _, err := db.Exec(`UPDATE codes SET deleted_at = NULL WHERE code = 'abc' COLLATE BINARY`); err != nil {
		t.Fatal(err)
	}
	if err := ch.PurgeOne(t.Context(), "ABC/s"); err != nil {
		t.Errorf("PurgeOne(ABC/s) beside a live abc/s: %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM codes WHERE code = 'abc' COLLATE BINARY AND deleted_at IS NULL`).Scan(&n); err != nil || n != 1 {
		t.Errorf("live abc/s rows after the purge = %d, %v; want 1", n, err)
	}
}
