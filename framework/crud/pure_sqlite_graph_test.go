package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	gosqlite "github.com/DonaldMurillo/gofastr/sqlite"
)

func TestPureSQLiteRelatedCRUDGraph(t *testing.T) {
	db, err := gosqlite.Open()
	if err != nil {
		t.Fatalf("open pure sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, ddl := range []string{
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT NOT NULL)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT NOT NULL, author_id TEXT NOT NULL)`,
		`CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT NOT NULL, body TEXT NOT NULL)`,
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("schema %q: %v", ddl, err)
		}
	}

	users := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users",
		Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
	}.WithTimestamps(false))
	comments := entity.Define("comments", entity.EntityConfig{
		Name: "comments", Table: "comments",
		Fields: []schema.Field{
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts", CursorField: "title",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "author_id", Type: schema.String, Required: true},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id"),
			entity.HasMany("comments", "comments", "post_id"),
		},
	}.WithTimestamps(false))
	for _, ent := range []*entity.Entity{users, comments, posts} {
		ent.SetDB(db)
	}
	registry := stubRegistry{byName: map[string]*entity.Entity{
		"users": users, "comments": comments, "posts": posts,
	}}
	userCRUD := NewCrudHandler(users, db).WithJSONCase(CaseSnake)
	commentCRUD := NewCrudHandler(comments, db).WithJSONCase(CaseSnake)
	postCRUD := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	postCRUD.Registry = registry

	ctx := context.Background()
	alice, err := userCRUD.CreateOne(ctx, map[string]any{"name": "alice"})
	if err != nil {
		t.Fatalf("create author: %v", err)
	}
	authorID := alice["id"].(string)

	first, err := postCRUD.CreateOne(ctx, map[string]any{"title": "alpha", "author_id": authorID})
	if err != nil {
		t.Fatalf("create post: %v", err)
	}
	firstID := first["id"].(string)
	if _, err := commentCRUD.CreateOne(ctx, map[string]any{"post_id": firstID, "body": "excellent"}); err != nil {
		t.Fatalf("create comment: %v", err)
	}

	updated, err := postCRUD.UpdateOne(ctx, firstID, map[string]any{"title": "alpha-updated"})
	if err != nil {
		t.Fatalf("update post: %v", err)
	}
	if updated["title"] != "alpha-updated" {
		t.Fatalf("updated title = %v", updated["title"])
	}

	upserted, err := postCRUD.UpsertOne(ctx, map[string]any{
		"id": firstID, "title": "alpha-upserted", "author_id": authorID,
	})
	if err != nil {
		t.Fatalf("upsert post: %v", err)
	}
	if upserted["title"] != "alpha-upserted" {
		t.Fatalf("upserted title = %v", upserted["title"])
	}

	batch, err := postCRUD.BatchCreateMany(ctx, []map[string]any{
		{"title": "beta", "author_id": authorID},
		{"title": "gamma", "author_id": authorID},
	})
	if err != nil {
		t.Fatalf("batch create: %v", err)
	}
	batchIDs := []string{batch[0]["id"].(string), batch[1]["id"].(string)}
	batch, err = postCRUD.BatchUpdateMany(ctx, batchIDs, []map[string]any{
		{"title": "beta-updated"},
		{"title": "gamma-updated"},
	})
	if err != nil {
		t.Fatalf("batch update: %v", err)
	}
	if batch[1]["title"] != "gamma-updated" {
		t.Fatalf("batch update result = %v", batch[1])
	}

	graph, err := postCRUD.GetOne(ctx, firstID, []string{"author", "comments"})
	if err != nil {
		t.Fatalf("eager graph: %v", err)
	}
	author, _ := graph["author"].(map[string]any)
	if author["name"] != "alice" {
		t.Fatalf("eager author = %v", graph["author"])
	}
	children, _ := graph["comments"].([]map[string]any)
	if len(children) != 1 || children[0]["body"] != "excellent" {
		t.Fatalf("eager comments = %v", graph["comments"])
	}

	nested, err := postCRUD.ListAll(ctx, ListOptions{
		NestedFilters: []NestedFilter{
			{Relation: "author", Field: "name", Op: filter.OpEq, Value: "alice"},
		},
	})
	if err != nil {
		t.Fatalf("nested relation predicate: %v", err)
	}
	if len(nested) != 3 {
		t.Fatalf("nested relation rows = %d, want 3", len(nested))
	}
	if _, err := postCRUD.ListAll(ctx, ListOptions{
		NestedFilters: []NestedFilter{
			{Relation: "author", Field: "unknown", Op: filter.OpEq, Value: "x"},
		},
	}); err == nil {
		t.Fatal("unknown related field was not rejected")
	}

	firstPage := httptest.NewRecorder()
	firstRequest := withTestUser(httptest.NewRequest(http.MethodGet, "/posts?cursor=&limit=2", nil), authorID)
	postCRUD.List()(firstPage, firstRequest)
	if firstPage.Code != http.StatusOK {
		t.Fatalf("cursor first page status = %d, body=%s", firstPage.Code, firstPage.Body.String())
	}
	var page struct {
		Data    []map[string]any `json:"data"`
		Cursor  string           `json:"cursor"`
		HasMore bool             `json:"hasMore"`
	}
	if err := json.Unmarshal(firstPage.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode cursor page: %v", err)
	}
	if len(page.Data) != 2 || page.Cursor == "" || !page.HasMore {
		t.Fatalf("cursor first page = %+v", page)
	}
	nextPage := httptest.NewRecorder()
	nextRequest := withTestUser(httptest.NewRequest(http.MethodGet, "/posts?cursor="+page.Cursor+"&limit=2", nil), authorID)
	postCRUD.List()(nextPage, nextRequest)
	if nextPage.Code != http.StatusOK {
		t.Fatalf("cursor next page status = %d, body=%s", nextPage.Code, nextPage.Body.String())
	}

	deleted, err := postCRUD.BatchDeleteMany(ctx, batchIDs)
	if err != nil {
		t.Fatalf("batch delete: %v", err)
	}
	if len(deleted) != 2 {
		t.Fatalf("batch deleted = %v", deleted)
	}
	if err := postCRUD.DeleteOne(ctx, firstID); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	count, err := postCRUD.CountAll(ctx, ListOptions{})
	if err != nil {
		t.Fatalf("count after deletes: %v", err)
	}
	if count != 0 {
		t.Fatalf("posts after deletes = %d, want 0", count)
	}
}
