package crud

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// The response schema the OpenAPI document registers marks every Required
// field required, and ?fields= documented a projection that could drop
// those keys, so a projected response failed the spec it was documented
// by (#669). The fix keeps the typed-client contract: a projection narrows
// the OPTIONAL columns, and the required ones (and the primary key) always
// ride along, so a client generated from the spec binds them to non-optional
// types and every documented response decodes.
func TestProjectionKeepsRequiredKeys(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("items", "items", "", []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "status", Type: schema.String, Required: true},
	}), `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT, status TEXT NOT NULL)`)
	seedRows(t, db, "items", []map[string]any{{"id": "i1", "title": "t", "status": "open"}})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items?fields=title", UserID: "alice"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list = %d (body=%s)", rr.Code, rr.Body.String())
	}
	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 1 {
		t.Fatalf("rows = %d, want 1", len(page.Data))
	}
	row := page.Data[0]
	keys := slices.Sorted(maps.Keys(row))
	// title was asked for; id (the key) and status (required) come too.
	if !slices.Equal(keys, []string{"id", "status", "title"}) {
		t.Fatalf("?fields=title row carried %v, want [id status title]", keys)
	}
	if row["status"] != "open" {
		t.Fatalf("required column lost its value: %v", row)
	}
}

// The same contract on a single-record Get: the projection parser is
// shared, so the get route keeps required keys too.
func TestGetProjectionKeepsRequiredKeys(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("items", "items", "", []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "status", Type: schema.String, Required: true},
	}), `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT, status TEXT NOT NULL)`)
	seedRows(t, db, "items", []map[string]any{{"id": "i1", "title": "t", "status": "open"}})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/items/i1?fields=title", UserID: "alice"})
	req.SetPathValue("id", "i1")
	rr := httptest.NewRecorder()
	ch.Get()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get = %d (body=%s)", rr.Code, rr.Body.String())
	}
	var env struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	keys := slices.Sorted(maps.Keys(env.Data))
	if !slices.Equal(keys, []string{"id", "status", "title"}) {
		t.Fatalf("?fields=title get carried %v, want [id status title]", keys)
	}
}

// A hidden required column is not in the response schema, so the
// projection must not resurrect it.
func TestProjectionRequiredKeysSkipHidden(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, makeEntityConfig("items", "items", "", []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "hash", Type: schema.String, Required: true, Hidden: true},
	}), `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT, hash TEXT NOT NULL)`)

	cols, err := ch.projectFromRequestQ(httptest.NewRequest(http.MethodGet, "/items?fields=title", nil).URL.Query())
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	if slices.Contains(cols, "hash") {
		t.Fatalf("projection resurrected the hidden required column: %v", cols)
	}
}
