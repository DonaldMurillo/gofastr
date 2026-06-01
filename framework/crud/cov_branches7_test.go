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
)

// TestThreeLevelNestedInclude drives posts → author → org → divisions,
// exercising the grandchild recursion in recurseLoadOnRawRows plus the
// slice-typed branches of rawRelationValue / deepConvertMap.
func TestThreeLevelNestedInclude(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE l3users (id TEXT PRIMARY KEY, name TEXT, org_id TEXT)`,
		`CREATE TABLE l3orgs (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE l3div (id TEXT PRIMARY KEY, org_id TEXT, label TEXT)`,
		`CREATE TABLE l3posts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`,
	)
	seedRows(t, db, "l3users", []map[string]any{{"id": "u1", "name": "alice", "org_id": "o1"}})
	seedRows(t, db, "l3orgs", []map[string]any{{"id": "o1", "name": "Acme"}})
	seedRows(t, db, "l3div", []map[string]any{
		{"id": "d1", "org_id": "o1", "label": "Eng"},
		{"id": "d2", "org_id": "o1", "label": "Sales"},
	})
	seedRows(t, db, "l3posts", []map[string]any{{"id": "p1", "title": "t", "author_id": "u1"}})

	divEnt := entity.Define("l3div", entity.EntityConfig{
		Name: "l3div", Table: "l3div",
		Fields: []schema.Field{{Name: "org_id", Type: schema.String}, {Name: "label", Type: schema.String}},
	}.WithTimestamps(false))
	orgEnt := entity.Define("l3orgs", entity.EntityConfig{
		Name: "l3orgs", Table: "l3orgs",
		Fields:    []schema.Field{{Name: "name", Type: schema.String}},
		Relations: []entity.Relation{entity.HasMany("divisions", "l3div", "org_id")},
	}.WithTimestamps(false))
	usersEnt := entity.Define("l3users", entity.EntityConfig{
		Name: "l3users", Table: "l3users",
		Fields:    []schema.Field{{Name: "name", Type: schema.String}, {Name: "org_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("org", "l3orgs", "org_id")},
	}.WithTimestamps(false))
	postsEnt := entity.Define("l3posts", entity.EntityConfig{
		Name: "l3posts", Table: "l3posts",
		Fields:    []schema.Field{{Name: "title", Type: schema.String}, {Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "l3users", "author_id")},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"l3div": divEnt, "l3orgs": orgEnt, "l3users": usersEnt, "l3posts": postsEnt,
	}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseCamel)
	ch.Registry = reg

	req := httptest.NewRequest("GET", "/l3posts?include=author.org.divisions", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("3-level include = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeListResponse(t, rec.Body.String())
	author := resp.Data[0]["author"].(map[string]any)
	org := author["org"].(map[string]any)
	divisions, _ := org["divisions"].([]any)
	if len(divisions) != 2 {
		t.Fatalf("grandchild divisions = %d, want 2: %+v", len(divisions), org)
	}
}

func TestEagerLoad_HasManyMultipleChildren(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE emposts (id TEXT PRIMARY KEY)`,
		`CREATE TABLE emcomments (id TEXT PRIMARY KEY, post_id TEXT, body TEXT)`,
	)
	seedRows(t, db, "emposts", []map[string]any{{"id": "p1"}})
	seedRows(t, db, "emcomments", []map[string]any{
		{"id": "c1", "post_id": "p1", "body": "one"},
		{"id": "c2", "post_id": "p1", "body": "two"},
	})
	ent := entity.Define("emposts", entity.EntityConfig{Name: "emposts", Table: "emposts"}.WithTimestamps(false))
	rels := []entity.Relation{entity.HasMany("comments", "emcomments", "post_id")}
	got, err := EagerLoad(context.Background(), db, ent, rels, []string{"p1"})
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	comments, _ := got["p1"]["comments"].([]map[string]any)
	if len(comments) != 2 {
		t.Errorf("multi-child HasMany = %d, want 2", len(comments))
	}
}

func TestEagerLoad_BelongsToNoMatch(t *testing.T) {
	// Source rows whose FK doesn't resolve → empty fkValues / no target rows.
	db := setupDB(t,
		`CREATE TABLE bnposts (id TEXT PRIMARY KEY, author_id TEXT)`,
		`CREATE TABLE bnusers (id TEXT PRIMARY KEY, name TEXT)`,
	)
	// Post with an empty author_id → loader produces no fkValues.
	seedRows(t, db, "bnposts", []map[string]any{{"id": "p1", "author_id": ""}})
	ent := entity.Define("bnposts", entity.EntityConfig{
		Name: "bnposts", Table: "bnposts",
		Fields: []schema.Field{{Name: "author_id", Type: schema.String}},
	}.WithTimestamps(false))
	rels := []entity.Relation{entity.BelongsTo("author", "bnusers", "author_id")}
	got, err := EagerLoad(context.Background(), db, ent, rels, []string{"p1"})
	if err != nil {
		t.Fatalf("EagerLoad: %v", err)
	}
	if _, ok := got["p1"]["author"]; ok {
		t.Error("unmatched belongs-to should not attach an author")
	}
}

func TestEagerLoad_SoftDeleteManyToMany(t *testing.T) {
	// M2M target soft-deletable → exercises the qualified soft-delete branch.
	db := setupDB(t,
		`CREATE TABLE smposts (id TEXT PRIMARY KEY)`,
		`CREATE TABLE smtags (id TEXT PRIMARY KEY, label TEXT, deleted_at TEXT)`,
		`CREATE TABLE smpivot (post_id TEXT, tag_id TEXT)`,
	)
	seedRows(t, db, "smposts", []map[string]any{{"id": "p1"}})
	seedRows(t, db, "smtags", []map[string]any{
		{"id": "t1", "label": "live", "deleted_at": nil},
		{"id": "t2", "label": "gone", "deleted_at": "2026-01-01"},
	})
	seedRows(t, db, "smpivot", []map[string]any{
		{"post_id": "p1", "tag_id": "t1"}, {"post_id": "p1", "tag_id": "t2"},
	})
	tagsEnt := entity.Define("smtags", entity.EntityConfig{
		Name: "smtags", Table: "smtags", SoftDelete: true,
		Fields: []schema.Field{{Name: "label", Type: schema.String}},
	}.WithTimestamps(false))
	postsEnt := entity.Define("smposts", entity.EntityConfig{Name: "smposts", Table: "smposts"}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"smtags": tagsEnt, "smposts": postsEnt}}
	m2m := entity.ManyToMany("tags", "smtags", "smpivot", "post_id", "tag_id")
	m2m.ForeignKey = "tag_id"
	got, err := EagerLoad(context.Background(), db, postsEnt, []entity.Relation{m2m}, []string{"p1"}, reg)
	if err != nil {
		t.Fatalf("EagerLoad m2m soft-delete: %v", err)
	}
	tags, _ := got["p1"]["tags"].([]map[string]any)
	if len(tags) != 1 {
		t.Fatalf("soft-deleted M2M tag leaked: %d tags", len(tags))
	}
}

func TestInclude_ScopedSoftDeleteManyToMany(t *testing.T) {
	// HTTP include path: M2M with a soft-deletable target → loadIncludeNode
	// M2M soft-delete branch + loadManyToManyFiltered.
	db := setupDB(t,
		`CREATE TABLE imposts (id TEXT PRIMARY KEY, title TEXT)`,
		`CREATE TABLE imtags (id TEXT PRIMARY KEY, label TEXT, deleted_at TEXT)`,
		`CREATE TABLE impivot (post_id TEXT, tag_id TEXT)`,
	)
	seedRows(t, db, "imposts", []map[string]any{{"id": "p1", "title": "t"}})
	seedRows(t, db, "imtags", []map[string]any{
		{"id": "t1", "label": "live", "deleted_at": nil},
		{"id": "t2", "label": "gone", "deleted_at": "2026-01-01"},
	})
	seedRows(t, db, "impivot", []map[string]any{
		{"post_id": "p1", "tag_id": "t1"}, {"post_id": "p1", "tag_id": "t2"},
	})
	tagsEnt := entity.Define("imtags", entity.EntityConfig{
		Name: "imtags", Table: "imtags", SoftDelete: true,
		Fields: []schema.Field{{Name: "label", Type: schema.String}},
	}.WithTimestamps(false))
	postsEnt := entity.Define("imposts", entity.EntityConfig{
		Name: "imposts", Table: "imposts",
		Fields:    []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{entity.ManyToMany("tags", "imtags", "impivot", "post_id", "tag_id")},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{"imtags": tagsEnt, "imposts": postsEnt}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = reg

	req := httptest.NewRequest("GET", "/imposts?include=tags", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	resp := decodeListResponse(t, rec.Body.String())
	tags, _ := resp.Data[0]["tags"].([]any)
	if len(tags) != 1 {
		t.Fatalf("soft-deleted M2M include leaked: %d tags", len(tags))
	}
}

func TestCursor_BackwardComposite(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorFields = []string{"seq", "id"} }, 6)
	// First forward page to obtain a composite cursor.
	rec := httptest.NewRecorder()
	ch.List()(rec, httptest.NewRequest("GET", "/items?cursor=&limit=2", nil))
	var page struct {
		Cursor string `json:"cursor"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	// Walk backward with the composite cursor → backward op branch.
	rec = httptest.NewRecorder()
	ch.List()(rec, httptest.NewRequest("GET", "/items?cursor="+page.Cursor+"&direction=backward&limit=2", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("backward composite cursor = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestEagerFiltered_InvalidFilterField(t *testing.T) {
	// No-registry flat include can't carry scoped filters, so drive
	// loadIncludeNode directly with an unsafe filter field on a node.
	db := setupDB(t, `CREATE TABLE efc (id TEXT PRIMARY KEY)`)
	node := &IncludeNode{
		Name:     "comments",
		Relation: entity.HasMany("comments", "efcomments", "post_id"),
		Filters:  []filter.ParsedFilter{{Field: "bad field", Op: filter.OpEq, Value: "x"}},
	}
	err := loadIncludeNode(context.Background(), db, "efc", "id", node, []string{"1"}, map[string]map[string]any{"1": {}})
	if err == nil {
		t.Error("unsafe filter field should error in loadIncludeNode")
	}
}
