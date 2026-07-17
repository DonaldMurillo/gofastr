package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestInclude_MultiValueINReturnsAllMatches pins that a scoped multi-value
// _in filter on an eager-loaded child returns EVERY matching child, not zero.
//
// Bug: parseScopedFilters emitted one ParsedFilter{OpIn} per piped value and
// filterClause rendered them as ANDed equality (status = $1 AND status = $2),
// which no single row can satisfy — the include came back empty.
func TestInclude_MultiValueINReturnsAllMatches(t *testing.T) {
	ddl := `
CREATE TABLE posts (
	id    TEXT PRIMARY KEY,
	title TEXT
);
CREATE TABLE comments (
	id      TEXT PRIMARY KEY,
	post_id TEXT NOT NULL,
	status  TEXT,
	body    TEXT
);
`
	postCfg := makeEntityConfig("posts", "posts", "",
		[]schema.Field{{Name: "title", Type: schema.String}},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{
				entity.HasMany("comments", "comments", "post_id"),
			}
		},
	)
	commentCfg := makeEntityConfig("comments", "comments", "",
		[]schema.Field{
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "status", Type: schema.String},
			{Name: "body", Type: schema.String},
		},
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	commentEnt := entity.Define(commentCfg.Table, commentCfg)
	commentEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, commentEnt)
	ch.Registry = reg

	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "title": "post"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c1", "post_id": "p1", "status": "published", "body": "pub"},
		{"id": "c2", "post_id": "p1", "status": "draft", "body": "draft"},
		{"id": "c3", "post_id": "p1", "status": "archived", "body": "arch"},
	})

	// Ask for comments whose status is published OR draft.
	req := makeRequest(t, RequestOpts{
		Method: http.MethodGet,
		Path:   "/posts?include=comments(status_in=published|draft)",
		UserID: "u1",
	})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("list+include returned %d (body=%s)", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"pub"`) {
		t.Errorf("multi-value _in dropped the published comment. Body: %s", body)
	}
	if !strings.Contains(body, `"draft"`) {
		t.Errorf("multi-value _in dropped the draft comment. Body: %s", body)
	}
	if strings.Contains(body, `"arch"`) {
		t.Errorf("multi-value _in leaked the archived (non-matching) comment. Body: %s", body)
	}
}

// TestInclude_MultiValueINNode exercises the loader directly so the
// generated SQL shape is covered independent of the HTTP plumbing.
func TestInclude_MultiValueINNode(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE posts (id TEXT PRIMARY KEY)`,
		`CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT, status TEXT)`,
	)
	seedRows(t, db, "posts", []map[string]any{{"id": "p1"}})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c1", "post_id": "p1", "status": "a"},
		{"id": "c2", "post_id": "p1", "status": "b"},
		{"id": "c3", "post_id": "p1", "status": "c"},
	})

	commentsEnt := entity.Define("comments", entity.EntityConfig{
		Name: "comments", Table: "comments",
		Fields: []schema.Field{
			{Name: "post_id", Type: schema.String},
			{Name: "status", Type: schema.String},
		},
	})
	node := &IncludeNode{
		Name:     "comments",
		Relation: entity.HasMany("comments", "comments", "post_id"),
		Target:   commentsEnt,
	}
	// Two-value IN: status IN ('a','b').
	parsed, err := parseScopedFilters("status_in=a|b", commentsEnt.GetFields(), "comments")
	if err != nil {
		t.Fatalf("parseScopedFilters: %v", err)
	}
	node.Filters = parsed

	result := map[string]map[string]any{"p1": {}}
	if err := loadIncludeNode(context.Background(), db, "posts", "id", node, []string{"p1"}, result); err != nil {
		t.Fatalf("loadIncludeNode: %v", err)
	}
	got, _ := result["p1"]["comments"].([]map[string]any)
	if len(got) != 2 {
		t.Fatalf("status_in=a|b returned %d comments, want 2: %v", len(got), got)
	}
}
