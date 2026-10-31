package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// A two-level cascade write answers with the grandchildren nested under
// the children, and the response ran only the children's read hooks: the
// nodes it built had no Children, so a field the attachments entity masks
// came back raw (2026-10-27 D36). Each written level now runs its own
// entity's hooks, to-many and to-one alike.
func TestCascadeResponseRunsGrandchildHooks(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`,
		`CREATE TABLE notes (id TEXT PRIMARY KEY, post_id TEXT, body TEXT)`,
		`CREATE TABLE attachments (id TEXT PRIMARY KEY, note_id TEXT, secret TEXT)`,
	)
	public := &entity.ExposureConfig{Public: true}
	id := schema.Field{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true}
	posts := entity.Define("posts", entity.EntityConfig{
		Fields:    []schema.Field{id, {Name: "title", Type: schema.String}},
		Relations: []entity.Relation{entity.HasMany("notes", "notes", "post_id").WithCascadeWrite(true)},
		Exposure:  public,
	}.WithTimestamps(false))
	notes := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{id, {Name: "post_id", Type: schema.String}, {Name: "body", Type: schema.String}},
		Relations: []entity.Relation{
			entity.HasMany("attachments", "attachments", "note_id").WithCascadeWrite(true),
			entity.HasOne("cover", "attachments", "note_id").WithCascadeWrite(true),
		},
		Exposure: public,
	}.WithTimestamps(false))
	attachments := entity.Define("attachments", entity.EntityConfig{
		Fields:   []schema.Field{id, {Name: "note_id", Type: schema.String}, {Name: "secret", Type: schema.String}},
		Exposure: public,
	}.WithTimestamps(false))
	hooks := map[string]*hook.HookRegistry{"attachments": maskField("secret")}
	ch := NewCrudHandler(posts, db).WithJSONCase(CaseSnake)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "notes": notes, "attachments": attachments}}
	ch.ChildHooks = func(name string) *hook.HookRegistry { return hooks[name] }

	if _, err := db.Exec(`INSERT INTO posts (id, title) VALUES ('p1', 'old')`); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"has_many grandchild": `{"title":"t","notes":[{"body":"b","attachments":[{"secret":"GRANDCHILD-SECRET"}]}]}`,
		"has_one grandchild":  `{"title":"t","notes":[{"body":"b","cover":{"secret":"GRANDCHILD-SECRET"}}]}`,
	} {
		for _, method := range []string{http.MethodPost, http.MethodPatch} {
			t.Run(method+" "+name, func(t *testing.T) {
				target, handle, want := "/posts", ch.Create(), http.StatusCreated
				if method == http.MethodPatch {
					target, handle, want = "/posts/p1", ch.Update(), http.StatusOK
				}
				r := httptest.NewRequest(method, target, strings.NewReader(body))
				r.Header.Set("Content-Type", "application/json")
				if method == http.MethodPatch {
					r.SetPathValue("id", "p1")
				}
				rec := httptest.NewRecorder()
				handle(rec, r)
				if rec.Code != want {
					t.Fatalf("%s = %d %s", method, rec.Code, rec.Body.String())
				}
				if strings.Contains(rec.Body.String(), "GRANDCHILD-SECRET") {
					t.Fatalf("the response served a grandchild field its entity masks: %s", rec.Body.String())
				}
				if !strings.Contains(rec.Body.String(), "MASKED") {
					t.Fatalf("the response dropped the grandchild instead of masking it: %s", rec.Body.String())
				}
			})
		}
	}
	// The stored row keeps its value: the mask is a read-side view.
	var stored int
	if err := db.QueryRow(`SELECT COUNT(*) FROM attachments WHERE secret = 'GRANDCHILD-SECRET'`).Scan(&stored); err != nil || stored != 4 {
		t.Fatalf("stored attachments with the raw value = %d (%v), want 4", stored, err)
	}
}

// attachedRows reads every shape an attachment takes, a write hook's []any
// included, and hands a map shared by two parents to the hooks once.
func TestAttachedRowsShapesAndDedupe(t *testing.T) {
	shared := map[string]any{"id": "s"}
	a, b, c := map[string]any{"id": "a"}, map[string]any{"id": "b"}, map[string]any{"id": "c"}
	rows := []map[string]any{
		{"rel": shared},
		{"rel": shared},
		{"rel": []map[string]any{a, nil}},
		{"rel": []any{b, "not a row", nil}},
		{"rel": c, "other": []any{map[string]any{"id": "x"}}},
		{"rel": nil},
		{},
	}
	var got []string
	for _, m := range attachedRows(rows, "rel") {
		got = append(got, m["id"].(string))
	}
	if strings.Join(got, ",") != "s,a,b,c" {
		t.Fatalf("attachedRows = %v, want [s a b c]", got)
	}
}
