package crud

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// covNote is the struct shape for typed-query round-tripping.
type covNote struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
}

// covNotesHandler builds an owner-free notes entity + handler over SQLite.
func covNotesHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE notes (id TEXT PRIMARY KEY, title TEXT, body TEXT)`)
	ent := entity.Define("notes", entity.EntityConfig{
		Name: "notes", Table: "notes",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	return ch, db
}

func TestInProcess_CreateGetUpdateDelete(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ctx := context.Background()

	created, err := ch.CreateOne(ctx, map[string]any{"title": "hello", "body": "world"})
	if err != nil {
		t.Fatalf("CreateOne: %v", err)
	}
	id := created["id"].(string)

	got, err := ch.GetOne(ctx, id, nil)
	if err != nil {
		t.Fatalf("GetOne: %v", err)
	}
	if got["title"] != "hello" {
		t.Errorf("GetOne title = %v", got["title"])
	}

	upd, err := ch.UpdateOne(ctx, id, map[string]any{"title": "hi"})
	if err != nil {
		t.Fatalf("UpdateOne: %v", err)
	}
	if upd["title"] != "hi" {
		t.Errorf("UpdateOne title = %v", upd["title"])
	}

	if err := ch.DeleteOne(ctx, id); err != nil {
		t.Fatalf("DeleteOne: %v", err)
	}
	if _, err := ch.GetOne(ctx, id, nil); !errors.Is(err, errNotFound) {
		t.Errorf("GetOne after delete err = %v, want errNotFound", err)
	}
}

func TestInProcess_GetOneNotFound(t *testing.T) {
	ch, _ := covNotesHandler(t)
	if _, err := ch.GetOne(context.Background(), "nope", nil); !errors.Is(err, errNotFound) {
		t.Errorf("err = %v, want errNotFound", err)
	}
}

func TestInProcess_ListAllAndCountAll(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ctx := context.Background()
	for _, title := range []string{"a", "b", "c"} {
		if _, err := ch.CreateOne(ctx, map[string]any{"title": title}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := ch.ListAll(ctx, ListOptions{
		Sorts:  []filter.ParsedSort{{Field: "title", Desc: false}},
		Limit:  2,
		Offset: 1,
	})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("ListAll len = %d, want 2", len(rows))
	}
	n, err := ch.CountAll(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("CountAll: %v", err)
	}
	if n != 3 {
		t.Errorf("CountAll = %d, want 3", n)
	}
}

func TestInProcess_BatchCreateUpdateDelete(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ctx := context.Background()

	created, err := ch.BatchCreateMany(ctx, []map[string]any{
		{"title": "one"}, {"title": "two"},
	})
	if err != nil {
		t.Fatalf("BatchCreateMany: %v", err)
	}
	if len(created) != 2 {
		t.Fatalf("created len = %d", len(created))
	}
	ids := []string{created[0]["id"].(string), created[1]["id"].(string)}

	updated, err := ch.BatchUpdateMany(ctx, ids, []map[string]any{
		{"title": "ONE"}, {"title": "TWO"},
	})
	if err != nil {
		t.Fatalf("BatchUpdateMany: %v", err)
	}
	if updated[0]["title"] != "ONE" {
		t.Errorf("batch update mismatch: %v", updated[0])
	}

	deleted, err := ch.BatchDeleteMany(ctx, ids)
	if err != nil {
		t.Fatalf("BatchDeleteMany: %v", err)
	}
	if len(deleted) != 2 {
		t.Errorf("deleted len = %d", len(deleted))
	}
}

func TestInProcess_BatchUpdateLengthMismatch(t *testing.T) {
	ch, _ := covNotesHandler(t)
	_, err := ch.BatchUpdateMany(context.Background(), []string{"a"}, []map[string]any{})
	if err == nil {
		t.Error("expected length-mismatch error")
	}
}

func TestInProcess_BatchCreateRollsBackOnError(t *testing.T) {
	ch, db := covNotesHandler(t)
	ctx := context.Background()
	// Second item missing required title → whole batch rolls back.
	_, err := ch.BatchCreateMany(ctx, []map[string]any{
		{"title": "ok"}, {"body": "no title"},
	})
	if err == nil {
		t.Fatal("expected batch error")
	}
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("rollback failed: %d rows persisted", n)
	}
}

func TestTypedQuery_FindFirstCountExists(t *testing.T) {
	ch, _ := covNotesHandler(t)
	ctx := context.Background()
	_, _ = ch.CreateOne(ctx, map[string]any{"title": "alpha", "body": "x"})
	_, _ = ch.CreateOne(ctx, map[string]any{"title": "beta", "body": "y"})

	q := NewTypedQuery[covNote](ch).
		Where(entity.NewStringColumn("title").Eq("alpha")).
		Order(entity.NewStringColumn("title").Asc()).
		Limit(10).
		Offset(0)
	found, err := q.Find(ctx)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(found) != 1 || found[0].Title != "alpha" {
		t.Fatalf("Find = %+v", found)
	}

	first, err := NewTypedQuery[covNote](ch).Where(entity.NewStringColumn("title").Eq("beta")).First(ctx)
	if err != nil {
		t.Fatalf("First: %v", err)
	}
	if first.Title != "beta" {
		t.Errorf("First = %+v", first)
	}

	if _, err := NewTypedQuery[covNote](ch).Where(entity.NewStringColumn("title").Eq("none")).First(ctx); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("First miss err = %v", err)
	}

	n, err := NewTypedQuery[covNote](ch).Count(ctx)
	if err != nil || n != 2 {
		t.Errorf("Count = %d, err=%v", n, err)
	}

	ex, err := NewTypedQuery[covNote](ch).Where(entity.NewStringColumn("title").Eq("alpha")).Exists(ctx)
	if err != nil || !ex {
		t.Errorf("Exists = %v, err=%v", ex, err)
	}
	ex, err = NewTypedQuery[covNote](ch).Where(entity.NewStringColumn("title").Eq("none")).Exists(ctx)
	if err != nil || ex {
		t.Errorf("Exists(none) = %v, err=%v", ex, err)
	}
}

func TestTypedQuery_UpdateAllAndDeleteAll(t *testing.T) {
	ch, db := covNotesHandler(t)
	ctx := context.Background()
	_, _ = ch.CreateOne(ctx, map[string]any{"title": "keep", "body": "1"})
	_, _ = ch.CreateOne(ctx, map[string]any{"title": "keep", "body": "2"})
	_, _ = ch.CreateOne(ctx, map[string]any{"title": "other", "body": "3"})

	n, err := NewTypedQuery[covNote](ch).
		Where(entity.NewStringColumn("title").Eq("keep")).
		UpdateAll(ctx, map[string]any{"body": "patched", "id": "ignored"})
	if err != nil {
		t.Fatalf("UpdateAll: %v", err)
	}
	if n != 2 {
		t.Errorf("UpdateAll touched %d, want 2", n)
	}
	// id must not have been overwritten.
	var cnt int
	_ = db.QueryRow("SELECT COUNT(*) FROM notes WHERE id = 'ignored'").Scan(&cnt)
	if cnt != 0 {
		t.Error("UpdateAll wrongly mutated primary key")
	}

	del, err := NewTypedQuery[covNote](ch).Where(entity.NewStringColumn("title").Eq("keep")).DeleteAll(ctx)
	if err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	if del != 2 {
		t.Errorf("DeleteAll = %d, want 2", del)
	}
}

func TestTypedQuery_UpdateAllEmptyFields(t *testing.T) {
	ch, _ := covNotesHandler(t)
	if _, err := NewTypedQuery[covNote](ch).UpdateAll(context.Background(), nil); err == nil {
		t.Error("UpdateAll with no fields should error")
	}
}

func TestMarshalUnmarshalEntity(t *testing.T) {
	row := map[string]any{"id": "1", "title": "hi", "body": "b"}
	var n covNote
	if err := UnmarshalEntity(row, &n); err != nil {
		t.Fatalf("UnmarshalEntity: %v", err)
	}
	if n.Title != "hi" {
		t.Errorf("unmarshal = %+v", n)
	}
	back, err := MarshalEntity(&covNote{ID: "1", Title: "hi", Body: "b"})
	if err != nil {
		t.Fatalf("MarshalEntity: %v", err)
	}
	if back["title"] != "hi" {
		t.Errorf("marshal = %+v", back)
	}
}

func TestUpsertOne_InsertThenUpdate(t *testing.T) {
	ch, db := covNotesHandler(t)
	ctx := context.Background()

	// Insert via upsert.
	res, err := ch.UpsertOne(ctx, map[string]any{"id": "u1", "title": "first"})
	if err != nil {
		t.Fatalf("UpsertOne insert: %v", err)
	}
	if res["title"] != "first" {
		t.Errorf("insert result = %v", res)
	}

	// Conflict → update.
	res, err = ch.UpsertOne(ctx, map[string]any{"id": "u1", "title": "second"})
	if err != nil {
		t.Fatalf("UpsertOne update: %v", err)
	}
	if res["title"] != "second" {
		t.Errorf("update result = %v", res)
	}
	var n int
	_ = db.QueryRow("SELECT COUNT(*) FROM notes").Scan(&n)
	if n != 1 {
		t.Errorf("upsert created duplicate: %d rows", n)
	}
}
