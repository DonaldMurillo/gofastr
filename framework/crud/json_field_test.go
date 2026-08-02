package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// jsonFieldHandler builds a handler over a table with one schema.JSON
// column ("features") plus a plain string column, seeded with one row so
// the update path has something to hit.
func jsonFieldHandler(t *testing.T) *CrudHandler {
	t.Helper()
	db := setupDB(t, `CREATE TABLE policies (id TEXT PRIMARY KEY, name TEXT, features TEXT)`)
	ent := entity.Define("policies", entity.EntityConfig{
		Name: "policies", Table: "policies",
		Fields: []schema.Field{
			{Name: "id", Type: schema.UUID, AutoGenerate: schema.AutoUUID, ReadOnly: true},
			{Name: "name", Type: schema.String, Required: true},
			{Name: "features", Type: schema.JSON},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db)
}

func createPolicy(t *testing.T, ch *CrudHandler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := withTestUser(httptest.NewRequest(http.MethodPost, "/policies", strings.NewReader(body)), "u1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ch.Create()(rec, req)
	return rec
}

// dataField pulls one key out of the {"data": {...}} envelope.
func dataField(t *testing.T, raw, key string) any {
	t.Helper()
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("decode response %s: %v", raw, err)
	}
	return env.Data[key]
}

// A schema.JSON field carrying a decoded object must be writable. The
// handler used to bind the Go map straight to the driver, which
// database/sql rejects ("unsupported type map[string]interface {}, a
// map") — a 500 on every create that populated the field, and the only
// supported way to configure such a column.
func TestCreateWritesJSONObject(t *testing.T) {
	ch := jsonFieldHandler(t)

	rec := createPolicy(t, ch, `{"name":"pro","features":{"seats":5,"beta":true}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with a JSON object = %d, body=%s", rec.Code, rec.Body.String())
	}

	// The column must hold JSON text the database can parse back, not a
	// Go fmt rendering of the map.
	var stored string
	row := ch.Entity.DB.QueryRow(`SELECT features FROM policies`)
	if err := row.Scan(&stored); err != nil {
		t.Fatalf("read back column: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(stored), &decoded); err != nil {
		t.Fatalf("stored value %q is not JSON: %v", stored, err)
	}
	if decoded["beta"] != true {
		t.Errorf("stored JSON lost a key: %q", stored)
	}
}

// A JSON array is as valid a JSON value as an object; the metered-plan
// case in the field report is a list.
func TestCreateWritesJSONArray(t *testing.T) {
	ch := jsonFieldHandler(t)

	rec := createPolicy(t, ch, `{"name":"metered","features":[{"meter":"calls","cap":100}]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with a JSON array = %d, body=%s", rec.Code, rec.Body.String())
	}
}

// Round trip: what a client sends must be what it reads back. Returning
// the column as an opaque string would make every SDK re-parse it and
// would contradict the spec, which declares the field as an unconstrained
// JSON value rather than a string.
func TestJSONFieldRoundTrips(t *testing.T) {
	ch := jsonFieldHandler(t)

	rec := createPolicy(t, ch, `{"name":"pro","features":{"seats":5,"beta":true}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	got := dataField(t, rec.Body.String(), "features")
	obj, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("features came back as %T (%v), want a JSON object", got, got)
	}
	if obj["beta"] != true || obj["seats"] != float64(5) {
		t.Errorf("round trip changed the value: %v", obj)
	}
}

// The update path binds through a different code path (SET clause, not
// VALUES), so it needs its own proof.
func TestUpdateWritesJSONObject(t *testing.T) {
	ch := jsonFieldHandler(t)

	rec := createPolicy(t, ch, `{"name":"pro"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}
	id, _ := dataField(t, rec.Body.String(), "id").(string)
	if id == "" {
		t.Fatalf("create returned no id: %s", rec.Body.String())
	}

	req := withTestUser(httptest.NewRequest(http.MethodPut, "/policies/"+id,
		strings.NewReader(`{"features":{"seats":9}}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", id)
	urec := httptest.NewRecorder()
	ch.Update()(urec, req)
	if urec.Code != http.StatusOK {
		t.Fatalf("update with a JSON object = %d, body=%s", urec.Code, urec.Body.String())
	}
	obj, ok := dataField(t, urec.Body.String(), "features").(map[string]any)
	if !ok || obj["seats"] != float64(9) {
		t.Errorf("update did not round-trip the JSON value: %s", urec.Body.String())
	}
}

// An omitted key and an empty object stay distinguishable: omitted leaves
// the column NULL (JSON null on read), {} reads back as {}. Clients that
// need "unset" versus "set to nothing" can rely on that.
func TestJSONAbsentIsNullEmptyIsObject(t *testing.T) {
	ch := jsonFieldHandler(t)

	absent := createPolicy(t, ch, `{"name":"a"}`)
	if absent.Code != http.StatusCreated {
		t.Fatalf("create without the JSON field = %d, body=%s", absent.Code, absent.Body.String())
	}
	if v := dataField(t, absent.Body.String(), "features"); v != nil {
		t.Errorf("omitted JSON field read back as %#v, want null", v)
	}

	empty := createPolicy(t, ch, `{"name":"b","features":{}}`)
	if empty.Code != http.StatusCreated {
		t.Fatalf("create with an empty object = %d, body=%s", empty.Code, empty.Body.String())
	}
	obj, ok := dataField(t, empty.Body.String(), "features").(map[string]any)
	if !ok || len(obj) != 0 {
		t.Errorf("empty object read back as %#v, want {}", dataField(t, empty.Body.String(), "features"))
	}
}

// Pre-existing callers send JSON text as a string (the admin battery's
// textarea, the image-variants writer). That must keep working and must
// read back as the parsed value, not as a doubly-encoded string.
func TestJSONStringInputStaysJSONText(t *testing.T) {
	ch := jsonFieldHandler(t)

	rec := createPolicy(t, ch, `{"name":"legacy","features":"{\"seats\":3}"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create with stringified JSON = %d, body=%s", rec.Code, rec.Body.String())
	}
	obj, ok := dataField(t, rec.Body.String(), "features").(map[string]any)
	if !ok || obj["seats"] != float64(3) {
		t.Errorf("stringified JSON was not stored as JSON text: %s", rec.Body.String())
	}
}

// An eager-loaded relation row goes through the include loaders rather
// than the handler's own scanner, and it belongs to a different entity.
// Its JSON columns still have to arrive parsed — otherwise a field is an
// object on GET /comments and a string one relation hop away.
func TestJSONFieldDecodedThroughInclude(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE jposts (id TEXT PRIMARY KEY, title TEXT)`,
		`CREATE TABLE jcomments (id TEXT PRIMARY KEY, post_id TEXT, meta TEXT)`,
	)
	seedRows(t, db, "jposts", []map[string]any{{"id": "p1", "title": "post"}})
	seedRows(t, db, "jcomments", []map[string]any{
		{"id": "c1", "post_id": "p1", "meta": `{"votes":3}`},
	})

	commentsEnt := entity.Define("jcomments", entity.EntityConfig{
		Name: "jcomments", Table: "jcomments",
		Fields: []schema.Field{
			{Name: "post_id", Type: schema.String},
			{Name: "meta", Type: schema.JSON},
		},
	}.WithTimestamps(false))
	postsEnt := entity.Define("jposts", entity.EntityConfig{
		Name: "jposts", Table: "jposts",
		Fields:    []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{entity.HasMany("comments", "jcomments", "post_id")},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)

	ch := NewCrudHandler(postsEnt, db)
	ch.Registry = stubRegistry{byName: map[string]*entity.Entity{
		"jposts": postsEnt, "jcomments": commentsEnt,
	}}

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/jposts?include=comments", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list with include = %d, body=%s", rec.Code, rec.Body.String())
	}

	resp := decodeListResponse(t, rec.Body.String())
	comments, _ := resp.Data[0]["comments"].([]any)
	if len(comments) != 1 {
		t.Fatalf("expected one included comment: %+v", resp.Data[0])
	}
	meta, ok := comments[0].(map[string]any)["meta"].(map[string]any)
	if !ok || meta["votes"] != float64(3) {
		t.Errorf("included row's JSON column not decoded: %+v", comments[0])
	}
}

// A JSON column holding text that is not JSON (a legacy TEXT column
// promoted to schema.JSON) must not break the read: the raw string comes
// back rather than an error or a dropped field.
func TestJSONNonJSONTextReadsAsString(t *testing.T) {
	ch := jsonFieldHandler(t)
	seedRows(t, ch.Entity.DB, "policies", []map[string]any{
		{"id": "p1", "name": "legacy", "features": "not json at all"},
	})

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/policies/p1", nil), "u1")
	req.SetPathValue("id", "p1")
	rec := httptest.NewRecorder()
	ch.Get()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d, body=%s", rec.Code, rec.Body.String())
	}
	if v := dataField(t, rec.Body.String(), "features"); v != "not json at all" {
		t.Errorf("non-JSON text in a JSON column read back as %#v", v)
	}
}

// The JSON codec's edge inputs, exercised directly: the paths a handler
// reaches only with an empty result set, a NULL column, a Postgres
// driver's []byte, or a Go caller bypassing validation.
func TestJSONCodecEdgeInputs(t *testing.T) {
	// decodeJSONColumn leaves anything that is not JSON text alone.
	for _, tc := range []struct {
		name string
		in   any
	}{
		{"nil", nil},
		{"empty string", ""},
		{"empty bytes", []byte{}},
		{"already typed", 42},
		{"not json", "raw text"},
	} {
		if got := decodeJSONColumn(tc.in); !reflect.DeepEqual(got, tc.in) {
			t.Errorf("decodeJSONColumn(%s) rewrote %#v to %#v", tc.name, tc.in, got)
		}
	}

	// Postgres hands JSONB back as []byte, not string.
	obj, ok := decodeJSONColumn([]byte(`{"a":1}`)).(map[string]any)
	if !ok || obj["a"] != float64(1) {
		t.Errorf("[]byte JSON not decoded: %#v", decodeJSONColumn([]byte(`{"a":1}`)))
	}

	// A value that cannot be marshalled passes through, so the driver's
	// own error surfaces instead of a substituted one. Only reachable
	// from a Go caller that skipped validation.
	ch := make(chan int)
	if got := marshalJSONColumn(ch); got != any(ch) {
		t.Errorf("unmarshalable value was rewritten to %#v", got)
	}
}

// The decode helpers no-op on the shapes a read path hands them when
// there is nothing to do, and never rebuild the cache twice in a row.
func TestJSONDecodeNoOpsOnEmptyInput(t *testing.T) {
	ch := jsonFieldHandler(t)
	ch.decodeJSONFields(nil)
	ch.decodeJSONRows(nil)
	ch.decodeJSONRows([]map[string]any{})

	// Second call must hit the fresh-cache branch, not rebuild.
	ch.ensureFieldCache()
	sig := ch.visibleFieldSig
	ch.ensureFieldCache()
	if ch.visibleFieldSig != sig {
		t.Errorf("ensureFieldCache rebuilt a fresh cache")
	}

	// An entity with no JSON field short-circuits the row loop.
	plain := entity.Define("plain", entity.EntityConfig{
		Name: "plain", Table: "plain",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	plain.SetDB(ch.Entity.DB)
	row := map[string]any{"name": `{"not":"decoded"}`}
	NewCrudHandler(plain, ch.Entity.DB).decodeJSONRows([]map[string]any{row})
	if row["name"] != `{"not":"decoded"}` {
		t.Errorf("non-JSON field was decoded: %#v", row["name"])
	}
}

// The list path scans through the keyed/pooled scanners rather than
// ch.scanMany, so it decodes its own JSON columns — a separate branch
// from the single-record read.
func TestListDecodesJSONField(t *testing.T) {
	ch := jsonFieldHandler(t)
	if rec := createPolicy(t, ch, `{"name":"pro","features":{"seats":5}}`); rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, body=%s", rec.Code, rec.Body.String())
	}

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/policies", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeListResponse(t, rec.Body.String())
	if len(resp.Data) != 1 {
		t.Fatalf("expected one row: %+v", resp.Data)
	}
	obj, ok := resp.Data[0]["features"].(map[string]any)
	if !ok || obj["seats"] != float64(5) {
		t.Errorf("list did not decode the JSON column: %+v", resp.Data[0])
	}
}

// CrudHandler is exported, so a caller can build one as a struct literal
// and skip NewCrudHandler's cache build. The JSON codec must still work
// rather than silently no-op on an empty cache.
func TestLiteralHandlerDecodesJSONField(t *testing.T) {
	ch := jsonFieldHandler(t)
	seedRows(t, ch.Entity.DB, "policies", []map[string]any{
		{"id": "p1", "name": "seeded", "features": `{"seats":7}`},
	})

	bare := &CrudHandler{Entity: ch.Entity, DB: ch.Entity.DB, PrimaryKey: "id", JSONCase: CaseCamel}
	row, err := bare.GetOne(signedIn("u1"), "p1", nil)
	if err != nil {
		t.Fatalf("GetOne: %v", err)
	}
	obj, ok := row["features"].(map[string]any)
	if !ok || obj["seats"] != float64(7) {
		t.Errorf("literal-built handler did not decode the JSON column: %#v", row["features"])
	}
}
