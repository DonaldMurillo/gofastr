package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// covMissingTargetWorld registers a posts entity whose relations point at
// tables that don't exist, so each eager loader's QueryContext errors.
func covMissingTargetWorld(t *testing.T) (*CrudHandler, stubRegistry) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE eposts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`)
	seedRows(t, db, "eposts", []map[string]any{{"id": "p1", "title": "t", "author_id": "u1"}})
	postsEnt := entity.Define("eposts", entity.EntityConfig{
		Name: "eposts", Table: "eposts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "ghost_users", "author_id"),
			entity.HasMany("comments", "ghost_comments", "post_id"),
			entity.ManyToMany("tags", "ghost_tags", "ghost_pivot", "post_id", "tag_id"),
		},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{"eposts": postsEnt}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = reg
	return ch, reg
}

func TestInclude_LoaderDBError_HasMany(t *testing.T) {
	ch, _ := covMissingTargetWorld(t)
	req := withTestUser(httptest.NewRequest("GET", "/eposts?include=comments", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("missing target HasMany include = %d, want 500", rec.Code)
	}
}

func TestInclude_LoaderDBError_BelongsTo(t *testing.T) {
	ch, _ := covMissingTargetWorld(t)
	req := withTestUser(httptest.NewRequest("GET", "/eposts?include=author", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("missing target BelongsTo include = %d, want 500", rec.Code)
	}
}

func TestInclude_LoaderDBError_ManyToMany(t *testing.T) {
	ch, _ := covMissingTargetWorld(t)
	req := withTestUser(httptest.NewRequest("GET", "/eposts?include=tags", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("missing target M2M include = %d, want 500", rec.Code)
	}
}

func TestEagerLoad_LoaderDBErrors(t *testing.T) {
	ch, reg := covMissingTargetWorld(t)
	ctx := context.Background()
	// HasMany loader QueryContext error.
	hm := entity.HasMany("comments", "ghost_comments", "post_id")
	if _, err := EagerLoad(ctx, dbFromCh(ch), ch.Entity, []entity.Relation{hm}, []string{"p1"}, reg); err == nil {
		t.Error("EagerLoad HasMany against missing table should error")
	}
	// BelongsTo loader: source query OK but target query errors.
	bt := entity.BelongsTo("author", "ghost_users", "author_id")
	if _, err := EagerLoad(ctx, dbFromCh(ch), ch.Entity, []entity.Relation{bt}, []string{"p1"}, reg); err == nil {
		t.Error("EagerLoad BelongsTo against missing target should error")
	}
	// ManyToMany loader QueryContext error.
	m2m := entity.ManyToMany("tags", "ghost_tags", "ghost_pivot", "post_id", "tag_id")
	m2m.ForeignKey = "tag_id"
	if _, err := EagerLoad(ctx, dbFromCh(ch), ch.Entity, []entity.Relation{m2m}, []string{"p1"}, reg); err == nil {
		t.Error("EagerLoad M2M against missing tables should error")
	}
}

// dbFromCh extracts the *sql.DB the handler was built with.
func dbFromCh(ch *CrudHandler) DBExecutor { return ch.DB }

func TestEagerLoad_ManyToManyInvalidIdentifiers(t *testing.T) {
	db := setupDB(t, `CREATE TABLE p (id TEXT PRIMARY KEY)`)
	ent := entity.Define("p", entity.EntityConfig{Name: "p", Table: "p"}.WithTimestamps(false))
	ctx := context.Background()
	// Invalid through table.
	r := entity.Relation{Type: entity.RelManyToMany, Name: "t", Entity: "x", ForeignKey: "fk", Through: "bad table!", LocalKey: "lk", ForeignKeyTarget: "fkt"}
	if _, err := EagerLoad(ctx, db, ent, []entity.Relation{r}, []string{"1"}); err == nil {
		t.Error("invalid through table should error")
	}
	// Invalid local key.
	r2 := entity.Relation{Type: entity.RelManyToMany, Name: "t", Entity: "x", ForeignKey: "fk", Through: "pivot", LocalKey: "bad key!", ForeignKeyTarget: "fkt"}
	if _, err := EagerLoad(ctx, db, ent, []entity.Relation{r2}, []string{"1"}); err == nil {
		t.Error("invalid local key should error")
	}
	// Invalid FK target.
	r3 := entity.Relation{Type: entity.RelManyToMany, Name: "t", Entity: "x", ForeignKey: "fk", Through: "pivot", LocalKey: "lk", ForeignKeyTarget: "bad!"}
	if _, err := EagerLoad(ctx, db, ent, []entity.Relation{r3}, []string{"1"}); err == nil {
		t.Error("invalid FK target should error")
	}
}

func TestRunToolRequest_NoContent(t *testing.T) {
	// A DELETE that returns 204 → runToolRequest returns (nil, nil).
	ent, db, r := covSimpleEntity(t)
	wch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	RegisterCrudRoutes(r, wch, "/widgets")
	// Create one row first via direct handler to get an id.
	row, err := wch.CreateOne(context.Background(), map[string]any{"name": "x"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := runToolRequest(handler.SetUser(context.Background(), &testUser{id: "u1"}), r, http.MethodDelete, "/widgets/"+row["id"].(string), nil)
	if err != nil {
		t.Fatalf("delete tool: %v", err)
	}
	if out != nil {
		t.Errorf("204 should yield nil output, got %v", out)
	}
}

func TestDecodeJSONBody_TooLarge(t *testing.T) {
	// A body over the limit triggers MaxBytesReader; decodeJSONBody maps it
	// to errBodyTooLarge via the http.MaxBytesError path.
	var sb strings.Builder
	sb.WriteString(`{"title":"`)
	sb.WriteString(strings.Repeat("x", int(MaxJSONBodyBytes)+50))
	sb.WriteString(`"}`)
	req := httptest.NewRequest("POST", "/notes", strings.NewReader(sb.String()))
	rec := httptest.NewRecorder()
	limitJSONBody(rec, req)
	var dst map[string]any
	if err := decodeJSONBody(req, &dst); err == nil {
		t.Error("oversize body should error in decodeJSONBody")
	}
}
