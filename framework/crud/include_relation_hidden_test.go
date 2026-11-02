package crud

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A has_many whose foreign key is Hidden on the target stays includable
// (a user's own notes); Relation.Hidden is the opt-out, and a relation
// marked Hidden answers ?include= as a relation that does not exist.
func TestRelationHiddenRefusesInclude(t *testing.T) {
	eachDialect(t, func(t *testing.T, db *sql.DB) {
		for _, stmt := range []string{
			`CREATE TABLE husers (id TEXT PRIMARY KEY, email TEXT)`,
			`CREATE TABLE hnotes (id TEXT PRIMARY KEY, body TEXT, owner_ref TEXT)`,
		} {
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("ddl: %v", err)
			}
		}
		public := &entity.ExposureConfig{Public: true}
		hidden := entity.HasMany("owned_notes", "hnotes", "owner_ref")
		hidden.Hidden = true
		userEnt := entity.Define("husers", entity.EntityConfig{
			Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "email", Type: schema.String}},
			Relations: []entity.Relation{entity.HasMany("notes", "hnotes", "owner_ref"), hidden},
			Exposure:  public,
		}.WithTimestamps(false))
		noteEnt := entity.Define("hnotes", entity.EntityConfig{
			Fields: []schema.Field{
				{Name: "id", Type: schema.String},
				{Name: "body", Type: schema.String},
				{Name: "owner_ref", Type: schema.String, Hidden: true},
			},
			Exposure: public,
		}.WithTimestamps(false))
		reg := stubRegistry{byName: map[string]*entity.Entity{"husers": userEnt, "hnotes": noteEnt}}
		users := NewCrudHandler(userEnt, db)
		users.Registry = reg
		pgSeed(t, db, "husers", []map[string]any{{"id": "u1", "email": "u@x"}})
		pgSeed(t, db, "hnotes", []map[string]any{{"id": "n1", "body": "mine", "owner_ref": "u1"}})

		get := func(include string) (int, string) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/husers/u1?include="+include, nil)
			req.SetPathValue("id", "u1")
			users.Get()(rec, req)
			return rec.Code, rec.Body.String()
		}
		if code, body := get("notes"); code != http.StatusOK || !strings.Contains(body, "mine") {
			t.Fatalf("include=notes (no opt-out) = %d %s; want 200 with the note", code, body)
		}
		code, body := get("owned_notes")
		if code != http.StatusBadRequest || strings.Contains(body, "mine") {
			t.Fatalf("include=owned_notes (Hidden) = %d %s; want 400 and no note", code, body)
		}
		_, unknown := get("no_such_relation")
		if strings.ReplaceAll(body, "owned_notes", "X") != strings.ReplaceAll(unknown, "no_such_relation", "X") {
			t.Fatalf("hidden refusal differs from an unknown name:\n%s\n%s", body, unknown)
		}
	})
}
