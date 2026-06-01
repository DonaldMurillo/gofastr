package crud

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestScopedInclude_ManyToManyFilter(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	// Scoped filter on a ManyToMany include exercises filterClauseQualified.
	req := httptest.NewRequest("GET", "/posts?include=tags(label=go)", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped m2m include = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeListResponse(t, rec.Body.String())
	for _, row := range resp.Data {
		if row["id"] == "p1" {
			tags, _ := row["tags"].([]any)
			if len(tags) != 1 {
				t.Errorf("scoped m2m tags = %d, want 1", len(tags))
			}
		}
	}
}

func TestNestedInclude_HasOneUnderBelongsTo(t *testing.T) {
	// posts → author (BelongsTo) → author HasOne settings → exercises the
	// default (non-slice) branch of rawRelationValue / formatRelationValueDeep
	// during the nested merge.
	db := setupDB(t,
		`CREATE TABLE o2users (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE o2posts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`,
		`CREATE TABLE o2settings (id TEXT PRIMARY KEY, user_id TEXT, theme TEXT)`,
	)
	seedRows(t, db, "o2users", []map[string]any{{"id": "u1", "name": "alice"}})
	seedRows(t, db, "o2posts", []map[string]any{{"id": "p1", "title": "t", "author_id": "u1"}})
	seedRows(t, db, "o2settings", []map[string]any{{"id": "s1", "user_id": "u1", "theme": "dark"}})

	settingsEnt := entity.Define("o2settings", entity.EntityConfig{
		Name: "o2settings", Table: "o2settings",
		Fields: []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "theme", Type: schema.String}},
	}.WithTimestamps(false))
	usersEnt := entity.Define("o2users", entity.EntityConfig{
		Name: "o2users", Table: "o2users",
		Fields:    []schema.Field{{Name: "name", Type: schema.String}},
		Relations: []entity.Relation{entity.HasOne("settings", "o2settings", "user_id")},
	}.WithTimestamps(false))
	postsEnt := entity.Define("o2posts", entity.EntityConfig{
		Name: "o2posts", Table: "o2posts",
		Fields:    []schema.Field{{Name: "title", Type: schema.String}, {Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "o2users", "author_id")},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"o2settings": settingsEnt, "o2users": usersEnt, "o2posts": postsEnt,
	}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = reg

	req := httptest.NewRequest("GET", "/o2posts?include=author.settings", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nested has-one include = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeListResponse(t, rec.Body.String())
	author, _ := resp.Data[0]["author"].(map[string]any)
	settings, _ := author["settings"].(map[string]any)
	if settings == nil || settings["theme"] != "dark" {
		t.Errorf("nested has-one settings not loaded: %+v", author)
	}
}

func TestMultipart_PlainValuesAndStrayFilePart(t *testing.T) {
	ch, db := covUploadHandler(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("caption", "hello")
	// A file part whose field name is NOT an Image/File field → skipped.
	fw, _ := mw.CreateFormFile("not_a_field", "x.bin")
	_, _ = fw.Write([]byte("ignored"))
	mw.Close()

	req := httptest.NewRequest("POST", "/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("multipart plain-values create = %d, body=%s", rec.Code, rec.Body.String())
	}
	var caption string
	row := rec.Body.String()
	_ = row
	if err := db.QueryRow("SELECT caption FROM media LIMIT 1").Scan(&caption); err != nil {
		t.Fatal(err)
	}
	if caption != "hello" {
		t.Errorf("caption = %q", caption)
	}
}

func TestParseMultipartBody_Direct_EmptyValue(t *testing.T) {
	ch, _ := covUploadHandler(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("caption", "x")
	mw.Close()
	req := httptest.NewRequest("POST", "/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	body, err := ch.parseMultipartBody(req)
	if err != nil {
		t.Fatalf("parseMultipartBody: %v", err)
	}
	if body["caption"] != "x" {
		t.Errorf("body = %+v", body)
	}
	_ = context.Background()
}
