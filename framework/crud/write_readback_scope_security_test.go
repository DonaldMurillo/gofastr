package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A write answers with the row it wrote. Before the fix that read-back
// ignored Exposure.ReadScope, so a caller who may write rows outside the
// scope (anonymous on a Public entity, or a role with the update permission
// but not the Unrestricted one) learned every column of a row GET answers
// 404 for, by sending a no-op PATCH. A restricted caller whose written row
// falls outside the scope now gets only the row's id back; the write itself
// is unchanged.

// readBackNotes builds notes{id,status,title,body} with a ReadScope that
// admits only status=published, seeded with a published n1 and a draft n2.
// acl nil means a Public entity.
func readBackNotes(t *testing.T, acl *entity.AccessControl) *CrudHandler {
	t.Helper()
	cfg := makeEntityConfig("notes", "notes", "", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "status", Type: schema.String},
		{Name: "title", Type: schema.String},
		{Name: "body", Type: schema.String},
	})
	exp := &entity.ExposureConfig{ReadScope: &entity.ReadScopeConfig{
		Filter:       []entity.RowPredicate{{Field: "status", Value: "published"}},
		Unrestricted: "notes:review",
	}}
	if acl == nil {
		exp.Public = true
		exp.ReadScope.Unrestricted = ""
	} else {
		exp.Access = *acl
	}
	cfg.Exposure = exp
	ch, db := setupSecurityTestHandler(t, cfg, `CREATE TABLE notes (id TEXT PRIMARY KEY, status TEXT, title TEXT, body TEXT)`)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "n1", "status": "published", "title": "public", "body": "pub"},
		{"id": "n2", "status": "draft", "title": "unannounced", "body": "DRAFT-SECRET"},
	})
	return ch
}

func roleReq(r *http.Request, perms ...access.Permission) *http.Request {
	r = withTestUser(r, "u-contrib")
	pol := access.NewRolePolicy()
	pol.Grant("contributor", perms...)
	ctx := access.WithPolicy(r.Context(), pol)
	ctx = access.WithRoles(ctx, []string{"contributor"})
	return r.WithContext(ctx)
}

func patchNote(t *testing.T, ch *CrudHandler, id, body string, decorate func(*http.Request) *http.Request) map[string]any {
	t.Helper()
	r := decorate(httptest.NewRequest(http.MethodPatch, "/notes/"+id, strings.NewReader(body)))
	r.Header.Set("Content-Type", "application/json")
	r.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	ch.Update()(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("PATCH /notes/%s = %d %s", id, rec.Code, rec.Body.String())
	}
	return decodeSingleResponse(t, rec.Body.Bytes())
}

func assertIDOnly(t *testing.T, label string, got map[string]any, id string) {
	t.Helper()
	if len(got) != 1 || got["id"] != id {
		t.Fatalf("%s: response = %v, want only {id: %s}", label, got, id)
	}
}

func anon(r *http.Request) *http.Request { return r }

func TestUpdateHidesReadScopedRowAnon(t *testing.T) {
	ch := readBackNotes(t, nil)
	assertIDOnly(t, "anon PATCH draft", patchNote(t, ch, "n2", `{"status":"draft"}`, anon), "n2")
	// Control: a row inside the scope still comes back whole.
	if got := patchNote(t, ch, "n1", `{"title":"public"}`, anon); got["body"] != "pub" {
		t.Fatalf("anon PATCH published: response = %v, want the full row", got)
	}
	// Unrestricted (any signed-in user when Unrestricted is empty) sees all.
	signedIn := func(r *http.Request) *http.Request { return withTestUser(r, "u1") }
	if got := patchNote(t, ch, "n2", `{"status":"draft"}`, signedIn); got["body"] != "DRAFT-SECRET" {
		t.Fatalf("signed-in PATCH draft: response = %v, want the full row", got)
	}
}

func TestUpdateHidesReadScopedRowRBAC(t *testing.T) {
	acl := &entity.AccessControl{Read: "notes:read", Create: "notes:edit", Update: "notes:edit", Delete: "notes:edit"}
	ch := readBackNotes(t, acl)
	contributor := func(r *http.Request) *http.Request { return roleReq(r, "notes:read", "notes:edit") }
	assertIDOnly(t, "contributor PATCH draft", patchNote(t, ch, "n2", `{"status":"draft"}`, contributor), "n2")
	reviewer := func(r *http.Request) *http.Request { return roleReq(r, "notes:read", "notes:edit", "notes:review") }
	if got := patchNote(t, ch, "n2", `{"status":"draft"}`, reviewer); got["body"] != "DRAFT-SECRET" {
		t.Fatalf("reviewer PATCH draft: response = %v, want the full row", got)
	}
}

func TestBatchUpdateHidesReadScopedRow(t *testing.T) {
	ch := readBackNotes(t, nil)
	r := httptest.NewRequest(http.MethodPatch, "/notes/_batch",
		strings.NewReader(`{"items":[{"id":"n1","title":"public"},{"id":"n2","status":"draft"}]}`))
	r.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.BatchUpdate()(rec, r)
	resp := decodeBatch(t, rec)
	if !resp.Committed {
		t.Fatalf("batch did not commit: %s", rec.Body.String())
	}
	if resp.Results[0].Data["body"] != "pub" {
		t.Fatalf("items[0] = %v, want the full published row", resp.Results[0].Data)
	}
	assertIDOnly(t, "batch items[1]", resp.Results[1].Data, "n2")
}

func TestInProcessWritesHideReadScopedRow(t *testing.T) {
	ch := readBackNotes(t, nil)
	ctx := context.Background()

	row, err := ch.UpdateOne(ctx, "n2", map[string]any{"status": "draft"})
	if err != nil {
		t.Fatal(err)
	}
	assertIDOnly(t, "UpdateOne", row, "n2")

	rows, err := ch.BatchUpdateMany(ctx, []string{"n2"}, []map[string]any{{"status": "draft"}})
	if err != nil {
		t.Fatal(err)
	}
	assertIDOnly(t, "BatchUpdateMany", rows[0], "n2")

	row, err = ch.UpsertOne(ctx, map[string]any{"id": "n2", "status": "draft"})
	if err != nil {
		t.Fatal(err)
	}
	assertIDOnly(t, "UpsertOne", row, "n2")

	// Control: an unrestricted caller gets the stored row.
	row, err = ch.UpsertOne(upsertSecurityContext("u1", ""), map[string]any{"id": "n2", "status": "draft"})
	if err != nil {
		t.Fatal(err)
	}
	if row["body"] != "DRAFT-SECRET" {
		t.Fatalf("signed-in UpsertOne = %v, want the full row", row)
	}
}
