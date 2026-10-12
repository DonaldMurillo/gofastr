package crud

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// An Image/File column holds a storage key, and EraseUserData deletes every
// key in the erased user's rows. The JSON and in-process write paths used to
// store any relative value a caller sent, so u1 could copy u2's key (it is
// published in u2's /uploads/<key> URLs) into its own row, erase itself, and
// delete u2's object. The <field>_variants storage_ref entries were not
// checked at all. A relative value is now accepted only when this write saved
// it, when it is already the row's value, or from trusted server code.

const (
	provForeignKey     = "uploads/prov_media/photo/victim_1_aa.png"
	provForeignVariant = "uploads/prov_media/photo/victim_1_aa_w320.png"
	provOwnKey         = "uploads/prov_media/photo/own_1_bb.png"
	provOwnVariants    = `[{"storage_ref":"uploads/prov_media/photo/own_1_bb_w320.png"}]`
)

// provWorld seeds u2's row v1 (the victim) and u1's row a1 on an
// owner-scoped entity with an Image field and its variants column.
func provWorld(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	ddl := `CREATE TABLE prov_media (id TEXT PRIMARY KEY, owner_id TEXT, caption TEXT, photo TEXT, photo_variants TEXT)`
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("prov_media", "prov_media", "owner_id", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "owner_id", Type: schema.String},
		{Name: "caption", Type: schema.String},
		{Name: "photo", Type: schema.Image},
		{Name: "photo_variants", Type: schema.JSON},
	}), ddl)
	ch.Storage = upload.NewLocalStorage(t.TempDir())
	seedRows(t, db, "prov_media", []map[string]any{
		{"id": "v1", "owner_id": "u2", "photo": provForeignKey,
			"photo_variants": `[{"storage_ref":"` + provForeignVariant + `"}]`},
		{"id": "a1", "owner_id": "u1", "photo": provOwnKey, "photo_variants": provOwnVariants},
	})
	return ch, db
}

func provJSON(t *testing.T, method, id string, body map[string]any) *http.Request {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := withTestUser(httptest.NewRequest(method, "/prov_media/"+id, bytes.NewReader(raw)), "u1")
	req.Header.Set("Content-Type", "application/json")
	if id != "" {
		req.SetPathValue("id", id)
	}
	return req
}

// provRefused asserts a 400 validation answer naming field.
func provRefused(t *testing.T, rr *httptest.ResponseRecorder, field string) {
	t.Helper()
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rr.Code, rr.Body.String())
	}
	var out struct {
		Fields map[string][]string `json:"fields"`
	}
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	if len(out.Fields[field]) == 0 {
		t.Fatalf("400 does not name %q: %s", field, rr.Body.String())
	}
}

func provCol(t *testing.T, db *sql.DB, id, col string) string {
	t.Helper()
	var v sql.NullString
	err := db.QueryRow("SELECT "+col+" FROM prov_media WHERE id = ?", id).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "<no row>"
	}
	if err != nil {
		t.Fatal(err)
	}
	return v.String
}

func provIsValidation(t *testing.T, err error, field string) {
	t.Helper()
	ve, ok := errors.AsType[*ValidationError](err)
	if !ok {
		t.Fatalf("err = %v, want a ValidationError on %q", err, field)
	}
	if len(ve.Fields()[field]) == 0 {
		t.Fatalf("ValidationError does not name %q: %v", field, ve.Fields())
	}
}

func TestJSONCreateRefusesForeignKey(t *testing.T) {
	ch, db := provWorld(t)
	rr := httptest.NewRecorder()
	ch.Create()(rr, provJSON(t, http.MethodPost, "", map[string]any{"id": "x", "photo": provForeignKey}))
	provRefused(t, rr, "photo")
	if got := provCol(t, db, "x", "photo"); got != "<no row>" {
		t.Fatalf("row x stored photo %q", got)
	}
}

func TestJSONCreateRefusesForeignVariant(t *testing.T) {
	for name, v := range map[string]any{
		"string": `[{"storage_ref":"` + provForeignVariant + `"}]`,
		"array":  []any{map[string]any{"storage_ref": provForeignVariant}},
	} {
		t.Run(name, func(t *testing.T) {
			ch, db := provWorld(t)
			rr := httptest.NewRecorder()
			ch.Create()(rr, provJSON(t, http.MethodPost, "", map[string]any{"id": "x", "photo_variants": v}))
			provRefused(t, rr, "photo_variants")
			if got := provCol(t, db, "x", "photo_variants"); got != "<no row>" {
				t.Fatalf("row x stored variants %q", got)
			}
		})
	}
}

func TestJSONUpdateRefusesForeignKey(t *testing.T) {
	ch, db := provWorld(t)
	rr := httptest.NewRecorder()
	ch.Update()(rr, provJSON(t, http.MethodPatch, "a1", map[string]any{"photo": provForeignKey}))
	provRefused(t, rr, "photo")
	if got := provCol(t, db, "a1", "photo"); got != provOwnKey {
		t.Fatalf("a1 photo = %q, want %q", got, provOwnKey)
	}
}

func TestJSONUpdateKeepsCurrentKey(t *testing.T) {
	ch, db := provWorld(t)
	rr := httptest.NewRecorder()
	ch.Update()(rr, provJSON(t, http.MethodPatch, "a1", map[string]any{
		"photo": provOwnKey, "photo_variants": provOwnVariants, "caption": "kept",
	}))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rr.Code, rr.Body.String())
	}
	if got := provCol(t, db, "a1", "caption"); got != "kept" {
		t.Fatalf("caption = %q", got)
	}
}

func TestJSONWriteAllowsExternalURL(t *testing.T) {
	ch, db := provWorld(t)
	rr := httptest.NewRecorder()
	ch.Create()(rr, provJSON(t, http.MethodPost, "", map[string]any{
		"id":             "x",
		"photo":          "https://cdn.example.com/a.png",
		"photo_variants": `[{"storage_ref":"https://cdn.example.com/a_w320.png"}]`,
	}))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}
	if got := provCol(t, db, "x", "photo"); got != "https://cdn.example.com/a.png" {
		t.Fatalf("photo = %q", got)
	}
}

// provMultipart builds a create carrying a PNG part for photo plus fields.
func provMultipart(t *testing.T, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("id", "x")
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	fw, _ := mw.CreateFormFile("photo", "pic.png")
	_, _ = fw.Write(pngBytes())
	mw.Close()
	req := withTestUser(httptest.NewRequest(http.MethodPost, "/prov_media", &buf), "u1")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestMultipartKeepsItsSavedKey(t *testing.T) {
	ch, db := provWorld(t)
	rr := httptest.NewRecorder()
	ch.Create()(rr, provMultipart(t, nil))
	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rr.Code, rr.Body.String())
	}
	if got := provCol(t, db, "x", "photo"); got == "<no row>" || got == "" {
		t.Fatalf("photo not stored: %q", got)
	}
}

func TestMultipartRefusesForeignVariant(t *testing.T) {
	ch, db := provWorld(t)
	rr := httptest.NewRecorder()
	ch.Create()(rr, provMultipart(t, map[string]string{
		"photo_variants": `[{"storage_ref":"` + provForeignVariant + `"}]`,
	}))
	provRefused(t, rr, "photo_variants")
	if got := provCol(t, db, "x", "photo"); got != "<no row>" {
		t.Fatalf("row x stored photo %q", got)
	}
}

func TestInProcessCreateRefusesKey(t *testing.T) {
	ch, _ := provWorld(t)
	_, err := ch.CreateOne(ctxWithUser("u1"), map[string]any{"id": "x", "photo": provForeignKey})
	provIsValidation(t, err, "photo")

	fresh := "uploads/prov_media/photo/host_saved.png"
	if _, err := ch.CreateOne(WithUploadedKeys(ctxWithUser("u1"), fresh),
		map[string]any{"id": "y", "photo": fresh}); err != nil {
		t.Fatalf("attested key refused: %v", err)
	}
	if _, err := ch.CreateOne(WithServerWrites(ctxWithUser("u1")),
		map[string]any{"id": "z", "photo": "uploads/seed/logo.png"}); err != nil {
		t.Fatalf("server write refused: %v", err)
	}
}

func TestInProcessUpdateRefusesKey(t *testing.T) {
	ch, db := provWorld(t)
	_, err := ch.UpdateOne(ctxWithUser("u1"), "a1", map[string]any{
		"photo_variants": []any{map[string]any{"storage_ref": provForeignVariant}},
	})
	provIsValidation(t, err, "photo_variants")
	if got := provCol(t, db, "a1", "photo_variants"); got != provOwnVariants {
		t.Fatalf("a1 variants = %q", got)
	}
}

func TestUpsertRefusesForeignKey(t *testing.T) {
	ch, db := provWorld(t)
	_, err := ch.UpsertOne(ctxWithUser("u1"), map[string]any{"id": "a1", "photo": provForeignKey})
	provIsValidation(t, err, "photo")
	_, err = ch.UpsertOne(ctxWithUser("u1"), map[string]any{"id": "new", "photo": provForeignKey})
	provIsValidation(t, err, "photo")
	if _, err := ch.UpsertOne(ctxWithUser("u1"), map[string]any{
		"id": "a1", "photo": provOwnKey, "caption": "upserted",
	}); err != nil {
		t.Fatalf("upsert keeping current key: %v", err)
	}
	if got := provCol(t, db, "a1", "caption"); got != "upserted" {
		t.Fatalf("caption = %q", got)
	}
}

type provRow struct{}

func TestUpdateAllRefusesStorageKey(t *testing.T) {
	ch, db := provWorld(t)
	_, err := NewTypedQuery[provRow](ch).
		Where(entity.NewStringColumn("id").Eq("a1")).
		UpdateAll(ctxWithUser("u1"), map[string]any{"photo": provForeignKey})
	provIsValidation(t, err, "photo")
	if got := provCol(t, db, "a1", "photo"); got != provOwnKey {
		t.Fatalf("a1 photo = %q", got)
	}
}

// TestUpsertCurrentKeyIsCallerScoped: "the row's current value" is read
// under the caller's write scope. A hook that derives the conflict key from
// the body (here: the caption names the row) must not let u1 borrow u2's
// key by naming u2's row in the id it sent.
func TestUpsertCurrentKeyIsCallerScoped(t *testing.T) {
	ch, db := provWorld(t)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.BeforeCreate, func(_ context.Context, data any) error {
		m := data.(map[string]any)
		if c, ok := m["caption"].(string); ok && c != "" {
			m["id"] = c
		}
		return nil
	})
	_, err := ch.UpsertOne(ctxWithUser("u1"), map[string]any{
		"id": "v1", "caption": "a1", "photo": provForeignKey,
	})
	provIsValidation(t, err, "photo")
	if got := provCol(t, db, "a1", "photo"); got != provOwnKey {
		t.Fatalf("a1 photo = %q, want %q", got, provOwnKey)
	}
}

// TestVariantsRefuseUnsafeScheme: a variants storage_ref renders into the
// same <img srcset> as the field, so the scheme allow-list covers it even
// for a trusted server write.
func TestVariantsRefuseUnsafeScheme(t *testing.T) {
	ch, _ := provWorld(t)
	_, err := ch.CreateOne(WithServerWrites(ctxWithUser("u1")), map[string]any{
		"id": "x", "photo_variants": `[{"storage_ref":"javascript:alert(1)"}]`,
	})
	provIsValidation(t, err, "photo_variants")
}
