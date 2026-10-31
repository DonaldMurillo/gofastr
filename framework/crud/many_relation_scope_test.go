package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A Many relation field holds one target id as a string, and Define
// derives no relation for it, so a write stored a target the caller
// cannot read with no check (2026-10-27 D43). It is checked as a
// belongs-to is: a create naming an unreadable target and an update
// moving to one are refused, and an update that keeps the stored id is
// not (D42).
func TestManyRelationTargetScoped(t *testing.T) {
	for _, tc := range []struct {
		name, method, id, body string
		want                   int
	}{
		{"create readable", http.MethodPost, "", `{"id":"p9","tag_ids":"t-open"}`, http.StatusCreated},
		{"create unreadable", http.MethodPost, "", `{"id":"p9","tag_ids":"t-hidden"}`, http.StatusNotFound},
		{"update moved to unreadable", http.MethodPatch, "p2", `{"title":"b","tag_ids":"t-hidden"}`, http.StatusNotFound},
		{"update keeps unreadable", http.MethodPatch, "p1", `{"title":"b","tag_ids":"t-hidden"}`, http.StatusOK},
		{"update moved to readable", http.MethodPatch, "p1", `{"title":"b","tag_ids":"t-open"}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := setupDB(t,
				`CREATE TABLE tags (id TEXT PRIMARY KEY, status TEXT)`,
				`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, tag_ids TEXT)`)
			tags := entity.Define("tags", entity.EntityConfig{
				Table:  "tags",
				Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "status", Type: schema.String}},
				Exposure: &entity.ExposureConfig{Public: true, ReadScope: &entity.ReadScopeConfig{
					Filter: []entity.RowPredicate{{Field: "status", Value: "active"}},
				}},
			}.WithTimestamps(false))
			posts := entity.Define("posts", entity.EntityConfig{
				Table: "posts",
				Fields: []schema.Field{
					{Name: "id", Type: schema.String},
					{Name: "title", Type: schema.String},
					{Name: "tag_ids", Type: schema.Relation, To: "tags", Many: true},
				},
				Exposure: &entity.ExposureConfig{Public: true},
			}.WithTimestamps(false))
			tags.SetDB(db)
			posts.SetDB(db)
			seedRows(t, db, "tags", []map[string]any{
				{"id": "t-open", "status": "active"},
				{"id": "t-hidden", "status": "archived"},
			})
			seedRows(t, db, "posts", []map[string]any{
				{"id": "p1", "title": "a", "tag_ids": "t-hidden"},
				{"id": "p2", "title": "a", "tag_ids": "t-open"},
			})
			ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
			ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"tags": tags, "posts": posts}}
			r := httptest.NewRequest(tc.method, "/posts/"+tc.id, strings.NewReader(tc.body))
			r.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			if tc.method == http.MethodPost {
				ch.Create()(rec, r)
			} else {
				r.SetPathValue("id", tc.id)
				ch.Update()(rec, r)
			}
			if rec.Code != tc.want {
				t.Fatalf("%s %s = %d %s, want %d", tc.method, tc.body, rec.Code, rec.Body.String(), tc.want)
			}
		})
	}
}
