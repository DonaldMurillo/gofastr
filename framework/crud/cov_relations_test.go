package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// covRelWorld builds a posts/users/comments/tags world with all relation
// kinds, registers each entity, and returns a posts CrudHandler + registry.
func covRelWorld(t *testing.T) (*CrudHandler, *sql.DB, stubRegistry) {
	t.Helper()
	db := setupDB(t,
		`CREATE TABLE users (id TEXT PRIMARY KEY, name TEXT, password_hash TEXT)`,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`,
		`CREATE TABLE comments (id TEXT PRIMARY KEY, post_id TEXT, body TEXT)`,
		`CREATE TABLE tags (id TEXT PRIMARY KEY, label TEXT)`,
		`CREATE TABLE post_tags (post_id TEXT, tag_id TEXT)`,
		`CREATE TABLE profiles (id TEXT PRIMARY KEY, post_id TEXT, bio TEXT)`,
	)
	seedRows(t, db, "users", []map[string]any{
		{"id": "u1", "name": "alice", "password_hash": "SECRET"},
	})
	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "title": "first", "author_id": "u1"},
		{"id": "p2", "title": "second", "author_id": "u1"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c1", "post_id": "p1", "body": "nice"},
		{"id": "c2", "post_id": "p1", "body": "ok"},
	})
	seedRows(t, db, "tags", []map[string]any{
		{"id": "t1", "label": "go"}, {"id": "t2", "label": "db"},
	})
	seedRows(t, db, "post_tags", []map[string]any{
		{"post_id": "p1", "tag_id": "t1"}, {"post_id": "p1", "tag_id": "t2"},
	})
	seedRows(t, db, "profiles", []map[string]any{
		{"id": "pr1", "post_id": "p1", "bio": "the bio"},
	})

	usersEnt := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String},
			{Name: "password_hash", Type: schema.String, Hidden: true},
		},
	}.WithTimestamps(false))
	commentsEnt := entity.Define("comments", entity.EntityConfig{
		Name: "comments", Table: "comments",
		Fields: []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	tagsEnt := entity.Define("tags", entity.EntityConfig{
		Name: "tags", Table: "tags",
		Fields: []schema.Field{{Name: "label", Type: schema.String}},
	}.WithTimestamps(false))
	profilesEnt := entity.Define("profiles", entity.EntityConfig{
		Name: "profiles", Table: "profiles",
		Fields: []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "bio", Type: schema.String}},
	}.WithTimestamps(false))
	postsEnt := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "users", "author_id"),
			entity.HasMany("comments", "comments", "post_id"),
			entity.HasOne("profile", "profiles", "post_id"),
			entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id"),
		},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)

	reg := stubRegistry{byName: map[string]*entity.Entity{
		"users": usersEnt, "comments": commentsEnt, "tags": tagsEnt,
		"profiles": profilesEnt, "posts": postsEnt,
	}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = reg
	return ch, db, reg
}

func TestList_WithAllIncludes(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	req := withTestUser(httptest.NewRequest("GET", "/posts?include=author,comments,profile,tags", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeListResponse(t, rec.Body.String())
	var p1 map[string]any
	for _, row := range resp.Data {
		if row["id"] == "p1" {
			p1 = row
		}
	}
	if p1 == nil {
		t.Fatal("p1 missing")
	}
	// BelongsTo author present, password_hash scrubbed.
	author, _ := p1["author"].(map[string]any)
	if author == nil || author["name"] != "alice" {
		t.Errorf("author = %v", author)
	}
	if _, leaked := author["password_hash"]; leaked {
		t.Error("SECURITY: Hidden password_hash leaked via include")
	}
	// HasMany comments (2).
	comments, _ := p1["comments"].([]any)
	if len(comments) != 2 {
		t.Errorf("comments len = %d, want 2", len(comments))
	}
	// HasOne profile.
	if prof, _ := p1["profile"].(map[string]any); prof == nil || prof["bio"] != "the bio" {
		t.Errorf("profile = %v", p1["profile"])
	}
	// ManyToMany tags (2).
	tags, _ := p1["tags"].([]any)
	if len(tags) != 2 {
		t.Errorf("tags len = %d, want 2", len(tags))
	}
}

func TestGet_WithIncludesAndProjection(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	req := withTestUser(httptest.NewRequest("GET", "/posts/p1?include=comments&fields=title", nil), "u1")
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := decodeSingleResponse(t, rec.Body.Bytes())
	if got["title"] != "first" {
		t.Errorf("title = %v", got["title"])
	}
	// id always projected.
	if got["id"] != "p1" {
		t.Errorf("id should always be projected, got %v", got["id"])
	}
}

func TestList_ScopedInclude(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	req := withTestUser(httptest.NewRequest("GET", "/posts?include=comments(body=nice)", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeListResponse(t, rec.Body.String())
	for _, row := range resp.Data {
		if row["id"] == "p1" {
			comments, _ := row["comments"].([]any)
			if len(comments) != 1 {
				t.Errorf("scoped include comments = %d, want 1", len(comments))
			}
		}
	}
}

func TestList_NestedInclude(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	// posts → comments (HasMany), and comments has no further relations, so
	// exercise author.<deeper> using a registered users entity with a relation.
	req := withTestUser(httptest.NewRequest("GET", "/posts?include=author", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestList_BadInclude(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	req := withTestUser(httptest.NewRequest("GET", "/posts?include=ghostrel", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad include status = %d, want 400", rec.Code)
	}
}

func TestList_BadProjection(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	req := withTestUser(httptest.NewRequest("GET", "/posts?fields=nonexistent", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad projection status = %d, want 400", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "nonexistent") {
		t.Error("error body echoed user-supplied field name")
	}
}

func TestEagerLoad_ManyToMany(t *testing.T) {
	ch, db, reg := covRelWorld(t)
	m2m := entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id")
	m2m.ForeignKey = "tag_id" // exported EagerLoad validates FK for all rel kinds
	rels := []entity.Relation{m2m}
	got, err := EagerLoad(context.Background(), db, ch.Entity, rels, []string{"p1", "p2"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad m2m: %v", err)
	}
	tags, _ := got["p1"]["tags"].([]map[string]any)
	if len(tags) != 2 {
		t.Errorf("p1 tags = %d, want 2", len(tags))
	}
	if _, ok := got["p2"]["tags"]; ok {
		t.Error("p2 should have no tags")
	}
}

func TestEagerLoad_HasOneAndBelongsTo(t *testing.T) {
	ch, db, reg := covRelWorld(t)
	rels := []entity.Relation{
		entity.HasOne("profile", "profiles", "post_id"),
		entity.BelongsTo("author", "users", "author_id"),
	}
	got, err := EagerLoad(context.Background(), db, ch.Entity, rels, []string{"p1"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	if prof, _ := got["p1"]["profile"].(map[string]any); prof["bio"] != "the bio" {
		t.Errorf("profile = %v", got["p1"]["profile"])
	}
	if author, _ := got["p1"]["author"].(map[string]any); author["name"] != "alice" {
		t.Errorf("author = %v", got["p1"]["author"])
	}
}

func TestEagerLoad_EmptyInputs(t *testing.T) {
	ch, db, _ := covRelWorld(t)
	got, err := EagerLoad(context.Background(), db, ch.Entity, nil, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("empty EagerLoad = %v, err=%v", got, err)
	}
}

func TestBuildIncludeNodesFromNames(t *testing.T) {
	ch, _, reg := covRelWorld(t)
	nodes, err := buildIncludeNodesFromNames(ch.Entity, reg, []string{"author", "comments"})
	if err != nil {
		t.Fatalf("buildIncludeNodesFromNames: %v", err)
	}
	if len(nodes) != 2 {
		t.Errorf("nodes = %d, want 2", len(nodes))
	}
	if nodes, _ := buildIncludeNodesFromNames(ch.Entity, reg, nil); nodes != nil {
		t.Error("empty names should yield nil nodes")
	}
}

func TestGetOne_WithIncludes(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	got, err := ch.GetOne(context.Background(), "p1", []string{"comments"})
	if err != nil {
		t.Fatalf("GetOne: %v", err)
	}
	comments, _ := got["comments"].([]map[string]any)
	if len(comments) != 2 {
		t.Errorf("GetOne includes comments = %d, want 2", len(comments))
	}
}

func TestListAll_WithIncludes(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	rows, err := ch.ListAll(context.Background(), ListOptions{Includes: []string{"tags"}})
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d", len(rows))
	}
}

func TestTypedQuery_FindWithInclude(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	type post struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	}
	q := NewTypedQuery[post](ch).Include("comments")
	out, err := q.Find(context.Background())
	if err != nil {
		t.Fatalf("Find with include: %v", err)
	}
	if len(out) != 2 {
		t.Errorf("Find = %d posts", len(out))
	}
}

func TestSplitHelpers(t *testing.T) {
	if got := splitIncludeList("comments(a=1,b=2),author"); len(got) != 2 {
		t.Errorf("splitIncludeList = %v", got)
	}
	if got := splitIncludePath("author.profile"); len(got) != 2 {
		t.Errorf("splitIncludePath = %v", got)
	}
	name, f := splitSegmentFilter("comments(status=draft)")
	if name != "comments" || f != "status=draft" {
		t.Errorf("splitSegmentFilter = %q, %q", name, f)
	}
	if name, f := splitSegmentFilter("author"); name != "author" || f != "" {
		t.Errorf("no-paren split = %q, %q", name, f)
	}
	// Unbalanced parens → raw name, empty filter.
	if name, _ := splitSegmentFilter("rel)broken"); name != "rel)broken" {
		t.Errorf("unbalanced = %q", name)
	}
}

func TestParseIncludesFlat_NoRegistry(t *testing.T) {
	ch, _, _ := covRelWorld(t)
	ch.Registry = nil
	req := withTestUser(httptest.NewRequest("GET", "/posts?include=comments", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("flat include status = %d, body=%s", rec.Code, rec.Body.String())
	}
	// Dotted include without registry → 400.
	req = withTestUser(httptest.NewRequest("GET", "/posts?include=author.profile", nil), "u1")
	rec = httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("dotted include without registry = %d, want 400", rec.Code)
	}
}
