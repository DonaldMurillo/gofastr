package crud

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The list route builds two queries, a COUNT and a data SELECT, and every
// predicate has to reach both. A change that dropped filter.ApplyToQuery from
// the data query alone left the whole crud suite green: the envelope's total
// came back correct because the count still filtered, while the rows were the
// unfiltered table. Nothing here asserted that `?field=value` narrows the ROWS,
// so the only thing that caught it was an admin battery test one package over.
//
// This pins both halves against each other. A divergence between them is the
// signature of exactly that class of edit, and it is worse than either query
// being wrong on its own: the response looks internally inconsistent only if
// you count the rows you were handed.
func TestListFilterNarrowsRowsAndTotalTogether(t *testing.T) {
	ddl := `
CREATE TABLE notes (
	id     TEXT PRIMARY KEY,
	kind   TEXT,
	title  TEXT
);
`
	cfg := makeEntityConfig("notes", "notes", "",
		[]schema.Field{
			{Name: "kind", Type: schema.String},
			{Name: "title", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Exposure = &entity.ExposureConfig{CRUD: boolPtrGate(true), Public: true}
		},
	)
	ch, db := setupSecurityTestHandler(t, cfg, ddl)
	seedRows(t, db, "notes", []map[string]any{
		{"id": "n1", "kind": "keep", "title": "first"},
		{"id": "n2", "kind": "drop", "title": "second"},
		{"id": "n3", "kind": "drop", "title": "third"},
	})

	req := makeRequest(t, RequestOpts{Method: http.MethodGet, Path: "/notes?kind=keep"})
	rr := httptest.NewRecorder()
	ch.List()(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /notes?kind=keep = %d: %s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data  []map[string]any `json:"data"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\n%s", err, rr.Body.String())
	}
	if len(env.Data) != 1 {
		t.Errorf("?kind=keep returned %d rows, want 1 — the filter did not reach the data query: %s",
			len(env.Data), rr.Body.String())
	}
	if env.Total != 1 {
		t.Errorf("?kind=keep reported total %d, want 1 — the filter did not reach the count query", env.Total)
	}
	if len(env.Data) != env.Total {
		t.Errorf("the envelope disagrees with itself: %d rows against a total of %d — one query carries the filter and the other does not",
			len(env.Data), env.Total)
	}
}
