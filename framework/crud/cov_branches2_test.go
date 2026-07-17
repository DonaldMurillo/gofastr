package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestNestedInclude_HasManyUnderBelongsTo exercises the recursive nested
// loader where the deeper relation is HasMany — the branch that attaches a
// []map[string]any slice and deep-converts it.
//
//	posts → author (BelongsTo users) → posts_by_author (HasMany)
func TestNestedInclude_HasManyUnderBelongsTo(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE husers (id TEXT PRIMARY KEY, full_name TEXT)`,
		`CREATE TABLE hposts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`,
		`CREATE TABLE hbooks (id TEXT PRIMARY KEY, author_id TEXT, book_title TEXT)`,
	)
	seedRows(t, db, "husers", []map[string]any{{"id": "u1", "full_name": "Alice A"}})
	seedRows(t, db, "hposts", []map[string]any{{"id": "p1", "title": "post", "author_id": "u1"}})
	seedRows(t, db, "hbooks", []map[string]any{
		{"id": "b1", "author_id": "u1", "book_title": "Book One"},
		{"id": "b2", "author_id": "u1", "book_title": "Book Two"},
	})

	booksEnt := entity.Define("hbooks", entity.EntityConfig{
		Name: "hbooks", Table: "hbooks",
		Fields: []schema.Field{{Name: "author_id", Type: schema.String}, {Name: "book_title", Type: schema.String}},
	}.WithTimestamps(false))
	usersEnt := entity.Define("husers", entity.EntityConfig{
		Name: "husers", Table: "husers",
		Fields:    []schema.Field{{Name: "full_name", Type: schema.String}},
		Relations: []entity.Relation{entity.HasMany("books", "hbooks", "author_id")},
	}.WithTimestamps(false))
	postsEnt := entity.Define("hposts", entity.EntityConfig{
		Name: "hposts", Table: "hposts",
		Fields:    []schema.Field{{Name: "title", Type: schema.String}, {Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "husers", "author_id")},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"hbooks": booksEnt, "husers": usersEnt, "hposts": postsEnt,
	}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseCamel) // camel to exercise deep convert
	ch.Registry = reg

	req := withTestUser(httptest.NewRequest("GET", "/hposts?include=author.books", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nested has-many include = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeListResponse(t, rec.Body.String())
	author, _ := resp.Data[0]["author"].(map[string]any)
	if author == nil {
		t.Fatalf("author missing: %+v", resp.Data[0])
	}
	books, _ := author["books"].([]any)
	if len(books) != 2 {
		t.Fatalf("nested books = %d, want 2: %+v", len(books), author)
	}
	// Deep camel conversion: book_title → bookTitle.
	b0 := books[0].(map[string]any)
	if _, ok := b0["bookTitle"]; !ok {
		t.Errorf("nested keys not camel-converted: %+v", b0)
	}
}

func TestMarshalEntity_Error(t *testing.T) {
	// A value containing a channel can't be JSON-marshalled.
	type bad struct {
		Ch chan int `json:"ch"`
	}
	if _, err := MarshalEntity(&bad{Ch: make(chan int)}); err == nil {
		t.Error("MarshalEntity of unmarshalable value should error")
	}
}

func TestUnmarshalEntity_Error(t *testing.T) {
	// A row with a channel value fails json.Marshal inside unmarshalRowToStruct.
	row := map[string]any{"ch": make(chan int)}
	var dest map[string]any
	if err := UnmarshalEntity(row, &dest); err == nil {
		t.Error("UnmarshalEntity of unmarshalable row should error")
	}
}

func TestTypedFind_UnmarshalError(t *testing.T) {
	// Title column is TEXT but struct expects an int → json.Unmarshal fails
	// when decoding the row into the struct.
	db := setupDB(t, `CREATE TABLE tq (id TEXT PRIMARY KEY, n TEXT)`)
	ent := entity.Define("tq", entity.EntityConfig{
		Name: "tq", Table: "tq",
		Fields: []schema.Field{{Name: "n", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	_, _ = ch.CreateOne(context.Background(), map[string]any{"n": "not-an-int"})

	type row struct {
		ID string `json:"id"`
		N  int    `json:"n"` // mismatched type
	}
	if _, err := NewTypedQuery[row](ch).Find(context.Background()); err == nil {
		t.Error("Find with type-mismatched struct should error on unmarshal")
	}
}

func TestSanitizeDefault_ShortStringAndFloat(t *testing.T) {
	if got := sanitizeDefault("short"); got != "short" {
		t.Errorf("short string = %q", got)
	}
	if got := sanitizeDefault(3.14); got != "3.14" {
		t.Errorf("float = %q", got)
	}
}

func TestIsSafeMediaURL_BareFilenameWithDot(t *testing.T) {
	// Hits the '.' delimiter branch (relative path before any colon).
	if !isSafeMediaURL("photo.png") {
		t.Error("bare filename with dot should be safe")
	}
	// Query/hash delimiters before colon → relative.
	if !isSafeMediaURL("path?x=1") {
		t.Error("relative path with query should be safe")
	}
}
