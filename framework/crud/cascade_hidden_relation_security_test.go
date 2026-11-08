package crud

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A cascade_write relation the API hides (Relation.Hidden, or a Hidden
// field named after it) took nested writes: ?include= and the DSL refuse
// it as not found, yet a POST carrying its key created or linked rows
// through it (#669). The nested write is refused before any row is
// written, on create and update; WithServerWrites still writes
// through it, as it writes a Hidden field.
func TestCascadeRefusesHiddenRelation(t *testing.T) {
	cases := []struct {
		name  string
		rel   entity.Relation
		field *schema.Field
		value string
	}{
		{"has_many hidden", entity.HasMany("child_rows", "notes", "post_id").WithCascadeWrite(true), nil, `[{"id":"n1","body_text":"SECRET"}]`},
		{"has_one hidden", entity.HasOne("profile_row", "notes", "post_id").WithCascadeWrite(true), nil, `{"id":"n1","body_text":"SECRET"}`},
		{"belongs_to hidden", entity.BelongsTo("lead_row", "notes", "note_id").WithCascadeWrite(true), nil, `{"id":"n1","body_text":"SECRET"}`},
		{"many_to_many hidden", entity.ManyToMany("tag_rows", "notes", "post_notes", "post_id", "note_id").WithCascadeWrite(true), nil, `["seed"]`},
		{"has_many behind a hidden field", entity.HasMany("child_rows", "notes", "post_id").WithCascadeWrite(true),
			&schema.Field{Name: "child_rows", Type: schema.JSON, Hidden: true}, `[{"id":"n1","body_text":"SECRET"}]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rel := c.rel
			if c.field == nil {
				rel.Hidden = true
			}
			handler, database := hiddenCascadeFixture(t, rel, c.field)
			request := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"id":"p1","title":"t",`+`"`+handler.convertKey(rel.Name)+`":`+c.value+`}`))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.Create()(response, request)
			body := response.Body.String()
			if response.Code != http.StatusBadRequest || !strings.Contains(body, "cascade_relation_hidden") || strings.Contains(body, "SECRET") {
				t.Errorf("POST through a hidden relation = %d %s, want 400 cascade_relation_hidden", response.Code, body)
			}
			var posts, notes, links int
			if err := database.QueryRow(`SELECT (SELECT COUNT(*) FROM posts), (SELECT COUNT(*) FROM notes), (SELECT COUNT(*) FROM post_notes)`).Scan(&posts, &notes, &links); err != nil {
				t.Fatal(err)
			}
			if posts != 0 || notes != 1 || links != 0 {
				t.Errorf("refused write left posts=%d notes=%d links=%d, want 0, 1 (the seed), 0", posts, notes, links)
			}

			// Server code still writes through it.
			body2 := map[string]any{"id": "p2", "title": "t"}
			var nested any = map[string]any{"id": "n2", "body_text": "server"}
			switch rel.Type {
			case entity.RelHasMany:
				nested = []any{nested}
			case entity.RelManyToMany:
				nested = []any{"seed"}
			}
			body2[rel.Name] = nested
			if _, err := handler.CreateOne(WithServerWrites(context.Background()), body2); err != nil {
				t.Fatalf("server write through the hidden relation: %v", err)
			}

			// An update through it is refused the same way, before the
			// parent's own fields change.
			before := cascadeCounts(t, database)
			_, err := handler.UpdateOne(context.Background(), "p2", map[string]any{"title": "moved", rel.Name: nested})
			if err == nil || !hasViolation(err, rel.Name, "validation.cascade_relation_hidden") {
				t.Errorf("update through a hidden relation = %v, want cascade_relation_hidden", err)
			}
			var title string
			if err := database.QueryRow(`SELECT title FROM posts WHERE id = 'p2'`).Scan(&title); err != nil {
				t.Fatal(err)
			}
			if after := cascadeCounts(t, database); after != before || title != "t" {
				t.Errorf("refused update changed rows %v -> %v, title %q", before, after, title)
			}
		})
	}
}

func hiddenCascadeFixture(t *testing.T, rel entity.Relation, extra *schema.Field) (*CrudHandler, *sql.DB) {
	t.Helper()
	database := setupDB(t,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, note_id TEXT, child_rows TEXT)`,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, post_id TEXT, body_text TEXT)`,
		`CREATE TABLE post_notes (post_id TEXT, note_id TEXT, PRIMARY KEY (post_id, note_id))`,
	)
	notes := entity.Define("notes", entity.EntityConfig{
		Fields:   []schema.Field{{Name: "id", Type: schema.String}, {Name: "post_id", Type: schema.String}, {Name: "body_text", Type: schema.String}},
		Exposure: &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	fields := []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}, {Name: "note_id", Type: schema.String}}
	if extra != nil {
		fields = append(fields, *extra)
	}
	posts := entity.Define("posts", entity.EntityConfig{
		Fields:    fields,
		Relations: []entity.Relation{rel},
		Exposure:  &entity.ExposureConfig{Public: true},
	}.WithTimestamps(false))
	notes.SetDB(database)
	posts.SetDB(database)
	handler := NewCrudHandler(posts, database).WithJSONCase(CaseSnake)
	handler.Registry = stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "notes": notes}}
	seedRows(t, database, "notes", []map[string]any{{"id": "seed", "body_text": "seed"}})
	return handler, database
}

func cascadeCounts(t *testing.T, database *sql.DB) [3]int {
	t.Helper()
	var n [3]int
	if err := database.QueryRow(`SELECT (SELECT COUNT(*) FROM posts), (SELECT COUNT(*) FROM notes), (SELECT COUNT(*) FROM post_notes)`).Scan(&n[0], &n[1], &n[2]); err != nil {
		t.Fatal(err)
	}
	return n
}

func hasViolation(err error, field, code string) bool {
	var verr *ValidationError
	if !errors.As(err, &verr) {
		return false
	}
	for _, v := range verr.fields[field] {
		if v.Reason.Key() == code {
			return true
		}
	}
	return false
}
