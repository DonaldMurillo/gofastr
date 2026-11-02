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

// hiddenRelationWorld is public zusers and znotes. A note holds three
// relations to a user: author_id, a visible relation field; covert_editor,
// a belongs_to over the Hidden covert_editor_id column; and
// secret_owner_ref, a Hidden relation field, from which Define derives a
// belongs_to of the same name. A user has_many notes by author_id, so a
// dotted include can reach a note's relations from a user.
func hiddenRelationWorld(t *testing.T, db *sql.DB) (users, notes *CrudHandler) {
	t.Helper()
	for _, stmt := range []string{
		`CREATE TABLE zusers (id TEXT PRIMARY KEY, email TEXT)`,
		`CREATE TABLE znotes (id TEXT PRIMARY KEY, body TEXT, secret_owner_ref TEXT, covert_editor_id TEXT, author_id TEXT)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	public := &entity.ExposureConfig{Public: true}
	userEnt := entity.Define("zusers", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "email", Type: schema.String}},
		Relations: []entity.Relation{entity.HasMany("notes", "znotes", "author_id")},
		Exposure:  public,
	}.WithTimestamps(false))
	noteEnt := entity.Define("znotes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "body", Type: schema.String},
			{Name: "secret_owner_ref", Type: schema.Relation, To: "zusers", Hidden: true},
			{Name: "covert_editor_id", Type: schema.String, Hidden: true},
			{Name: "author_id", Type: schema.Relation, To: "zusers"},
		},
		Relations: []entity.Relation{entity.BelongsTo("covert_editor", "zusers", "covert_editor_id")},
		Exposure:  public,
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"zusers": userEnt, "znotes": noteEnt}}
	users = NewCrudHandler(userEnt, db).WithJSONCase(CaseSnake)
	users.Registry = reg
	notes = NewCrudHandler(noteEnt, db).WithJSONCase(CaseSnake)
	notes.Registry = reg
	pgSeed(t, db, "zusers", []map[string]any{
		{"id": "u-owner", "email": "owner-secret@x"},
		{"id": "u-author", "email": "author@x"},
	})
	pgSeed(t, db, "znotes", []map[string]any{
		{"id": "n1", "body": "b", "secret_owner_ref": "u-owner", "covert_editor_id": "u-owner", "author_id": "u-author"},
	})
	return users, notes
}

// Property: a Hidden field never leaves the server, and a relation behind
// one is not includable: ?include= on it put the related record, with the
// hidden key, in the response. Every include surface (list, cursor list,
// get, dotted and scoped includes, the in-process GetOne, ListAll and
// TypedQuery) and the nested filter (?rel.field=, a value oracle on the
// related record) answers it the way it answers a relation that does not
// exist, so the answer is no existence oracle either, and a near-miss hint
// never names one.
// Surfaces: include.go relationByName/includeNearMiss, nested_filter.go.
func TestHiddenRelationIsNotIncludable(t *testing.T) {
	eachDialect(t, func(t *testing.T, db *sql.DB) {
		users, notes := hiddenRelationWorld(t, db)
		get := func(ch *CrudHandler, target, id string) (int, string) {
			t.Helper()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.SetPathValue("id", id)
			ch.Get()(rec, req)
			return rec.Code, rec.Body.String()
		}
		// The answer a name that is no relation at all gets, with the
		// name taken out.
		unknown := func(code int, body, name string) string {
			return http.StatusText(code) + " " + strings.ReplaceAll(body, name, "NAME")
		}
		code, body := listBody(t, notes, "/znotes?include=nosuchrel")
		wantList := unknown(code, body, "nosuchrel")
		code, body = get(notes, "/znotes/n1?include=nosuchrel", "n1")
		wantGet := unknown(code, body, "nosuchrel")
		code, body = listBody(t, notes, "/znotes?include=nosuchrel(email=owner-secret@x)")
		wantScoped := unknown(code, body, "nosuchrel")
		code, body = listBody(t, notes, "/znotes?nosuchrel.email=owner-secret@x")
		wantNested := unknown(code, body, "nosuchrel")
		if code != http.StatusBadRequest {
			t.Fatalf("premise: an unknown nested-filter relation answered %d: %s", code, body)
		}

		for _, rel := range []string{"secret_owner_ref", "covert_editor"} {
			for _, tc := range []struct{ target, want string }{
				{"/znotes?include=" + rel, wantList},
				{"/znotes?include=" + rel + "&cursor=", wantList},
				{"/znotes?include=" + rel + "(email=owner-secret@x)", wantScoped},
				{"/znotes?" + rel + ".email=owner-secret@x", wantNested},
			} {
				code, body := listBody(t, notes, tc.target)
				if got := unknown(code, body, rel); got != tc.want {
					t.Errorf("GET %s answered %s\nwant the unknown-relation answer %s", tc.target, got, tc.want)
				}
			}
			code, body := get(notes, "/znotes/n1?include="+rel, "n1")
			if got := unknown(code, body, rel); got != wantGet {
				t.Errorf("GET /znotes/n1?include=%s answered %s\nwant the unknown-relation answer %s", rel, got, wantGet)
			}
			code, body = listBody(t, users, "/zusers?include=notes."+rel)
			if code != http.StatusBadRequest || strings.Contains(body, "owner-secret") {
				t.Errorf("GET /zusers?include=notes.%s answered %d: %s", rel, code, body)
			}

			ctx := context.Background()
			if row, err := notes.GetOne(ctx, "n1", []string{rel}); err == nil {
				t.Errorf("GetOne(include %s) = %v, want the unknown-include error", rel, row)
			}
			if rows, err := notes.ListAll(ctx, ListOptions{Includes: []string{rel}}); err == nil {
				t.Errorf("ListAll(include %s) = %v, want the unknown-include error", rel, rows)
			}
			type note struct {
				ID string `json:"id"`
			}
			if _, err := NewTypedQuery[note](notes).Include(rel).Find(ctx); err == nil {
				t.Errorf("TypedQuery.Include(%s).Find succeeded, want the unknown-include error", rel)
			}
		}

		// A near miss names only a relation the caller may include.
		for _, guess := range []string{"zusers", "zuser", "secret_owner", "covert_editor_id"} {
			_, body := listBody(t, notes, "/znotes?include="+guess)
			if strings.Contains(body, "secret_owner_ref") || strings.Contains(body, `covert_editor\"`) || strings.Contains(body, `"covert_editor"`) {
				t.Errorf("?include=%s hinted at a hidden relation: %s", guess, body)
			}
		}

		// The visible relation still loads.
		code, body = listBody(t, notes, "/znotes?include=author_id")
		if code != http.StatusOK || !strings.Contains(body, "author@x") {
			t.Fatalf("?include=author_id answered %d: %s", code, body)
		}
		if strings.Contains(body, "owner-secret") || strings.Contains(body, "u-owner") {
			t.Errorf("?include=author_id disclosed a hidden key: %s", body)
		}
	})
}

// llm.md documents the includes an entity answers; a relation behind a
// Hidden field is not one, and the document names no Hidden column.
func TestLLMMDNamesNoHiddenRelation(t *testing.T) {
	_, notes := hiddenRelationWorld(t, setupDB(t))
	doc := EntityLLMMD(notes.Entity)
	if !strings.Contains(doc, "`author_id`") {
		t.Fatalf("llm.md lost the visible relation:\n%s", doc)
	}
	for _, leak := range []string{"secret_owner_ref", "covert_editor"} {
		if strings.Contains(doc, leak) {
			t.Errorf("llm.md names the hidden %q:\n%s", leak, doc)
		}
	}
}
