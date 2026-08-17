package crud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// nestedInHandler: posts BelongsTo author (people), the shape the nested
// _in path filters through an EXISTS subquery.
func nestedInHandler(t *testing.T) *CrudHandler {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE people (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`)
	people := entity.Define("people", entity.EntityConfig{
		Name: "people", Table: "people",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{entity.BelongsTo("author", "people", "author_id")},
	}.WithTimestamps(false))
	people.SetDB(db)
	posts.SetDB(db)
	ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"people": people, "posts": posts}}
	seedRows(t, db, "people", []map[string]any{
		{"id": "p-alice", "name": "alice"},
		{"id": "p-bob", "name": "bob"},
		{"id": "p-carol", "name": "carol"},
	})
	seedRows(t, db, "posts", []map[string]any{
		{"id": "po-1", "title": "by alice", "author_id": "p-alice"},
		{"id": "po-2", "title": "by bob", "author_id": "p-bob"},
		{"id": "po-3", "title": "by carol", "author_id": "p-carol"},
	})
	return ch
}

func nestedInList(t *testing.T, ch *CrudHandler, query string) ListResponse {
	t.Helper()
	req := withTestUser(httptest.NewRequest("GET", "/posts"+query, nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", query, rec.Code, rec.Body.String())
	}
	return decodeListResponse(t, rec.Body.String())
}

// TestNestedRepeatedInKeysUnion: every occurrence of a repeated
// ?rel.field_in= key must contribute values — the same union contract the
// flat path has. Reading only values[0] narrowed ?author.name_in=alice&
// ?author.name_in=bob to alice alone, silently dropping bob's posts.
func TestNestedRepeatedInKeysUnion(t *testing.T) {
	ch := nestedInHandler(t)

	single := nestedInList(t, ch, "?author.name_in=alice")
	if len(single.Data) != 1 {
		t.Fatalf("sanity: ?author.name_in=alice returned %d rows, want 1", len(single.Data))
	}

	double := nestedInList(t, ch, "?author.name_in=alice&author.name_in=bob")
	if len(double.Data) != 2 {
		t.Fatalf("?author.name_in=alice&author.name_in=bob returned %d rows, want 2 (union)", len(double.Data))
	}

	// Comma form and repeated form must agree.
	comma := nestedInList(t, ch, "?author.name_in=alice,bob")
	if len(comma.Data) != len(double.Data) {
		t.Fatalf("repeated keys (%d rows) disagree with comma form (%d rows)", len(double.Data), len(comma.Data))
	}
}

// TestNestedInListOverCapErrors: a nested ?rel.field_in= list must obey
// the same 1000-entry cap (and error shape) as the flat ?field_in= path.
// Without it, one request drives 1100+ bind parameters through the EXISTS
// subquery — uncapped memory/CPU per request, exactly what the flat cap
// exists to prevent.
func TestNestedInListOverCapErrors(t *testing.T) {
	ch := nestedInHandler(t)

	flatVals := strings.Repeat("x,", 1100) + "x"
	req := withTestUser(httptest.NewRequest("GET", "/posts?title_in="+flatVals, nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("sanity: flat 1101-entry list = %d, want 400", rec.Code)
	}

	req2 := withTestUser(httptest.NewRequest("GET", "/posts?author.name_in="+flatVals, nil), "u1")
	rec2 := httptest.NewRecorder()
	ch.List()(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("nested 1101-entry list = %d, want 400 (same cap as flat path). body=%s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), fmt.Sprint("max")) && !strings.Contains(rec2.Body.String(), "1000") {
		t.Errorf("nested cap error does not report the max: %s", rec2.Body.String())
	}
}
