package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestGenerateFieldValue_AllStrategies(t *testing.T) {
	if v := generateFieldValue(schema.AutoUUID); v == "" {
		t.Error("AutoUUID should produce a value")
	}
	if v := generateFieldValue(schema.AutoTimestamp); v == nil {
		t.Error("AutoTimestamp should produce a value")
	}
	if v := generateFieldValue(schema.AutoIncrement); v != 0 {
		t.Errorf("AutoIncrement placeholder = %v, want 0", v)
	}
	if v := generateFieldValue(schema.AutoNone); v != nil {
		t.Errorf("AutoNone = %v, want nil", v)
	}
}

func TestEntityFields(t *testing.T) {
	ch, _ := covNotesHandler(t)
	got := ch.entityFields()
	// id + title + body.
	if len(got) != 3 {
		t.Errorf("entityFields = %v", got)
	}
	if got[0] != "id" {
		t.Errorf("first field = %q", got[0])
	}
}

func TestRefreshFieldCache_NilEntity(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: "id", JSONCase: CaseCamel}
	ch.refreshFieldCache()
	if ch.visibleFieldsCache != nil {
		t.Error("nil entity should clear cache")
	}
	if ch.fieldCacheSignature() != 0 {
		t.Error("nil entity signature should be 0")
	}
}

// covCamelHandler builds a handler with snake_case DB columns + camelCase JSON.
func covCamelHandler(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	dbc := setupDB(t, `CREATE TABLE people (id TEXT PRIMARY KEY, full_name TEXT, age INTEGER)`)
	ent := entity.Define("people", entity.EntityConfig{
		Name: "people", Table: "people",
		Fields: []schema.Field{
			{Name: "full_name", Type: schema.String, Required: true},
			{Name: "age", Type: schema.Int},
		},
	}.WithTimestamps(false))
	ent.SetDB(dbc)
	return NewCrudHandler(ent, dbc), dbc // default CaseCamel
}

func TestCamelCase_CreateAndList(t *testing.T) {
	ch, _ := covCamelHandler(t)
	// Create with camelCase JSON key fullName → converted to full_name.
	req := withTestUser(httptest.NewRequest("POST", "/people", strings.NewReader(`{"fullName":"Jane Doe","age":30}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("camel create = %d, body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"fullName"`) {
		t.Errorf("response not camelCased: %s", body)
	}

	// List returns camelCased keys + projection mismatch path (jsonKeysFor).
	req = withTestUser(httptest.NewRequest("GET", "/people?fields=fullName", nil), "u1")
	rec = httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("camel list = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"fullName"`) {
		t.Errorf("list not camelCased: %s", rec.Body.String())
	}
}

func TestConvertMapKeys_Camel(t *testing.T) {
	ch, _ := covCamelHandler(t)
	out := ch.convertMapKeys(map[string]any{"full_name": "x"})
	if _, ok := out["fullName"]; !ok {
		t.Errorf("convertMapKeys camel = %v", out)
	}
	back := ch.unconvertMapKeys(map[string]any{"fullName": "x"})
	if _, ok := back["full_name"]; !ok {
		t.Errorf("unconvertMapKeys camel = %v", back)
	}
}

func TestConvertMapKeys_SnakeNoop(t *testing.T) {
	ch, _ := covNotesHandler(t) // CaseSnake
	in := map[string]any{"full_name": "x"}
	if got := ch.convertMapKeys(in); len(got) != 1 || got["full_name"] != "x" {
		t.Errorf("snake convert should be noop: %v", got)
	}
	if got := ch.unconvertMapKeys(in); got["full_name"] != "x" {
		t.Errorf("snake unconvert should be noop: %v", got)
	}
}

func TestUniqueViolation_Returns409(t *testing.T) {
	dbc := setupDB(t, `CREATE TABLE uq (id TEXT PRIMARY KEY, email TEXT UNIQUE)`)
	ent := entity.Define("uq", entity.EntityConfig{
		Name: "uq", Table: "uq",
		Fields: []schema.Field{{Name: "email", Type: schema.String, Unique: true}},
	}.WithTimestamps(false))
	ent.SetDB(dbc)
	ch := NewCrudHandler(ent, dbc).WithJSONCase(CaseSnake)

	mk := func() *httptest.ResponseRecorder {
		req := withTestUser(httptest.NewRequest("POST", "/uq", strings.NewReader(`{"email":"a@b.com"}`)), "u1")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		return rec
	}
	if rec := mk(); rec.Code != http.StatusCreated {
		t.Fatalf("first create = %d", rec.Code)
	}
	rec := mk()
	if rec.Code != http.StatusConflict {
		t.Fatalf("dup create = %d, want 409", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "email") {
		t.Error("409 body leaked violated column name")
	}
}

func TestIsUniqueViolation(t *testing.T) {
	for _, msg := range []string{
		"UNIQUE constraint failed: x.y",
		"pq: duplicate key value violates unique constraint",
		"Error 1062: Duplicate entry",
	} {
		if !isUniqueViolation(&covStrErr{msg}) {
			t.Errorf("should detect unique violation in %q", msg)
		}
	}
	if isUniqueViolation(nil) {
		t.Error("nil is not a unique violation")
	}
	if isUniqueViolation(&covStrErr{"some other error"}) {
		t.Error("unrelated error misclassified")
	}
}

type covStrErr struct{ s string }

func (e *covStrErr) Error() string { return e.s }

func TestUpdate_NoFieldsToUpdate(t *testing.T) {
	ch, _ := covNotesHandler(t)
	created, _ := ch.CreateOne(context.Background(), map[string]any{"title": "x"})
	id := created["id"].(string)
	// Body has only the (skipped) id field → no settable fields → 400.
	req := withTestUser(httptest.NewRequest("PUT", "/notes/"+id, strings.NewReader(`{"id":"`+id+`"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("no-fields update = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no fields to update") {
		t.Errorf("unexpected body: %s", rec.Body.String())
	}
}

func TestCreate_ValidationError(t *testing.T) {
	ch, _ := covNotesHandler(t) // title is Required
	req := withTestUser(httptest.NewRequest("POST", "/notes", strings.NewReader(`{"body":"no title"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("validation create = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "validation failed") {
		t.Errorf("body: %s", rec.Body.String())
	}
}

func TestCreate_MissingContentType(t *testing.T) {
	ch, _ := covNotesHandler(t)
	req := httptest.NewRequest("POST", "/notes", strings.NewReader(`{"title":"x"}`))
	req.Header.Del("Content-Type")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("missing content-type = %d, want 415", rec.Code)
	}
}

func TestUpdate_MissingID(t *testing.T) {
	ch, _ := covNotesHandler(t)
	req := withTestUser(httptest.NewRequest("PUT", "/notes/", strings.NewReader(`{"title":"x"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing id update = %d, want 400", rec.Code)
	}
}

func TestGet_MissingID(t *testing.T) {
	ch, _ := covNotesHandler(t)
	req := withTestUser(httptest.NewRequest("GET", "/notes/", nil), "u1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing id get = %d, want 400", rec.Code)
	}
}

func TestDelete_MissingID(t *testing.T) {
	ch, _ := covNotesHandler(t)
	req := withTestUser(httptest.NewRequest("DELETE", "/notes/", nil), "u1")
	rec := httptest.NewRecorder()
	ch.Delete()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing id delete = %d, want 400", rec.Code)
	}
}

func TestDelete_NotFound(t *testing.T) {
	ch, _ := covNotesHandler(t)
	req := withTestUser(httptest.NewRequest("DELETE", "/notes/nope", nil), "u1")
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	ch.Delete()(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing = %d, want 404", rec.Code)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	ch, _ := covNotesHandler(t)
	req := withTestUser(httptest.NewRequest("PUT", "/notes/nope", strings.NewReader(`{"title":"x"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "nope")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("update missing = %d, want 404", rec.Code)
	}
}

func TestInTx_AmbientTransaction(t *testing.T) {
	ch, dbc := covNotesHandler(t)
	tx, err := dbc.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	ctx := db.WithTx(context.Background(), tx)
	// inTx should reuse the ambient tx and NOT commit it.
	ran := false
	err = ch.inTx(ctx, func(c context.Context, h *CrudHandler) error {
		ran = true
		if _, ok := db.TxFromContext(c); !ok {
			t.Error("ambient tx missing from context")
		}
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("inTx ambient err=%v ran=%v", err, ran)
	}
	// Tx still open — a query must succeed against it.
	if _, err := tx.Exec(`SELECT 1`); err != nil {
		t.Errorf("ambient tx was closed by inTx: %v", err)
	}
}

func TestInTx_NonBeginnerExecutor(t *testing.T) {
	ch, _ := covNotesHandler(t)
	// Swap DB for an executor without BeginTx → fn runs against it directly.
	ch.DB = covNoTxExecutor{ch.DB}
	ran := false
	err := ch.inTx(context.Background(), func(c context.Context, h *CrudHandler) error {
		ran = true
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("non-beginner inTx err=%v ran=%v", err, ran)
	}
}

// covNoTxExecutor wraps a db.Executor but deliberately hides BeginTx.
type covNoTxExecutor struct{ inner db.Executor }

func (e covNoTxExecutor) QueryContext(ctx context.Context, q string, a ...any) (*sql.Rows, error) {
	return e.inner.QueryContext(ctx, q, a...)
}
func (e covNoTxExecutor) QueryRowContext(ctx context.Context, q string, a ...any) *sql.Row {
	return e.inner.QueryRowContext(ctx, q, a...)
}
func (e covNoTxExecutor) ExecContext(ctx context.Context, q string, a ...any) (sql.Result, error) {
	return e.inner.ExecContext(ctx, q, a...)
}

func TestRequireTenantContext(t *testing.T) {
	dbc := setupDB(t, `CREATE TABLE mt (id TEXT PRIMARY KEY, tenant_id TEXT, body TEXT)`)
	ent := entity.Define("mt", entity.EntityConfig{
		Name: "mt", Table: "mt", MultiTenant: true,
		Fields: []schema.Field{{Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(dbc)
	ch := NewCrudHandler(ent, dbc).WithJSONCase(CaseSnake)
	if err := ch.requireTenantContext(context.Background()); err == nil {
		t.Error("multi-tenant without tenant id should error")
	}
	// Non-multitenant entity is always fine.
	ch2, _ := covNotesHandler(t)
	if err := ch2.requireTenantContext(context.Background()); err != nil {
		t.Errorf("non-tenant requireTenantContext err = %v", err)
	}
}

func TestScanRowsOne_Error(t *testing.T) {
	ch, dbc := covNotesHandler(t)
	_, _ = ch.CreateOne(context.Background(), map[string]any{"title": "x"})
	rows, err := dbc.Query(`SELECT id, title FROM notes`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no rows")
	}
	// Mismatched column count → Scan error surfaced by scanRowsOne.
	if _, err := scanRowsOne(rows, []string{"id"}, func(s string) string { return s }); err == nil {
		t.Error("scanRowsOne with wrong col count should error")
	}
}

func TestPoolRoundTrip(t *testing.T) {
	s := borrowRowSlice()
	*s = append(*s, map[string]any{"a": 1})
	returnRowSlice(s)
	p := borrowPtrSlice(4)
	if len(*p) != 4 {
		t.Errorf("borrowPtrSlice len = %d", len(*p))
	}
	returnPtrSlice(p)
	// Re-borrow smaller, should reuse.
	p2 := borrowPtrSlice(2)
	returnPtrSlice(p2)
}
