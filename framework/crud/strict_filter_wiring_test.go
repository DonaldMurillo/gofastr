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
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// allowedFilterHandler builds a notes handler whose entity declares `region`
// as an extra allowed query param (consumed by a BeforeList hook).
func allowedFilterHandler(t *testing.T, allowed []string) (*CrudHandler, *sql.DB) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE notes (id TEXT PRIMARY KEY, owner TEXT NOT NULL, body TEXT)`); err != nil {
		t.Fatal(err)
	}
	ent := entity.Define("notes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "owner", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
		AllowedFilterParams: allowed,
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	return ch, db
}

// A custom query param the entity declares in AllowedFilterParams must reach
// the BeforeList hook (200), not be rejected by strict filter parsing (400).
// This is the escape hatch for host-consumed params that keeps typo
// protection intact (regression: strict parsing ran before BeforeList and
// 400ed the param before the hook could read it).
func TestList_AllowedFilterParamReachesHook(t *testing.T) {
	ch, _ := allowedFilterHandler(t, []string{"region"})

	sawRegion := ""
	ch.Hooks.RegisterHook(hook.BeforeList, func(ctx context.Context, data any) error {
		p := data.(*hook.ListPayload)
		sawRegion = p.Request.URL.Query().Get("region")
		return nil
	})

	req := withTestUser(httptest.NewRequest(http.MethodGet, "/notes?region=eu", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("declared param must not 400, got %d body=%s", rec.Code, rec.Body.String())
	}
	if sawRegion != "eu" {
		t.Fatalf("BeforeList hook did not see ?region=eu, got %q", sawRegion)
	}
}

// Without the declaration the same custom param still fails closed — the
// escape hatch is opt-in and narrow, not a blanket relaxation.
func TestList_UndeclaredCustomParamStillRejected(t *testing.T) {
	ch, _ := allowedFilterHandler(t, nil)
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/notes?region=eu", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("undeclared custom param want 400, got %d", rec.Code)
	}
}

// ?per_page is accepted as an alias for ?limit — both to set the page size
// and to survive strict filter parsing — so a client using the common
// per_page convention gets the size it asked for instead of a 400 or a
// silent default.
func TestParsePagination_PerPageAlias(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/notes?per_page=7", nil)
	_, perPage := parsePagination(req, 0)
	if perPage != 7 {
		t.Fatalf("per_page alias = %d, want 7", perPage)
	}
	// ?limit wins when both are present.
	req = httptest.NewRequest(http.MethodGet, "/notes?limit=5&per_page=7", nil)
	if _, pp := parsePagination(req, 0); pp != 5 {
		t.Fatalf("limit should win over per_page, got %d, want 5", pp)
	}
}

// End-to-end: a ?per_page= request no longer 400s under strict parsing.
func TestList_PerPageDoesNotReject(t *testing.T) {
	ch, _ := allowedFilterHandler(t, nil)
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/notes?per_page=10", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("per_page must not 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// explicitOffset parses a raw ?offset= row skip; anything malformed keeps the
// page-derived offset.
func TestExplicitOffset(t *testing.T) {
	cases := []struct {
		query   string
		want    int
		present bool
	}{
		{"", 0, false},
		{"?offset=0", 0, true},
		{"?offset=25", 25, true},
		{"?offset=-3", 0, false},
		{"?offset=abc", 0, false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/notes"+c.query, nil)
		got, ok := explicitOffset(r)
		if got != c.want || ok != c.present {
			t.Errorf("explicitOffset(%q) = (%d,%v), want (%d,%v)", c.query, got, ok, c.want, c.present)
		}
	}
}

// End-to-end: ?offset= is honored (a raw row skip, as the process-module
// broker sends it) rather than silently ignored — the regression F6 flagged.
func TestList_OffsetSkipsRows(t *testing.T) {
	ch, db := allowedFilterHandler(t, nil)
	if _, err := db.Exec(`INSERT INTO notes (id, owner, body) VALUES
		('n1','u1','a'), ('n2','u1','b'), ('n3','u1','c')`); err != nil {
		t.Fatal(err)
	}
	// owner scoping is off (no OwnerField), so all three rows are visible.
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/notes?sort=id&offset=2", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// Offset 2 with id-sorted rows skips n1,n2 → only n3 remains.
	if strings.Contains(body, `"n1"`) || strings.Contains(body, `"n2"`) {
		t.Fatalf("offset=2 did not skip rows: %s", body)
	}
	if !strings.Contains(body, `"n3"`) {
		t.Fatalf("offset=2 dropped the wrong rows: %s", body)
	}
}

// The STREAMING path must honor ?offset= too — otherwise ?offset=N&stream=true
// silently serves page 1 (the buffered path honors it; divergence caught by
// the GLM review).
func TestServeStreamingList_OffsetSkipsRows(t *testing.T) {
	ch, db := allowedFilterHandler(t, nil)
	if _, err := db.Exec(`INSERT INTO notes (id, owner, body) VALUES
		('n1','u1','a'), ('n2','u1','b'), ('n3','u1','c')`); err != nil {
		t.Fatal(err)
	}
	req := withTestUser(httptest.NewRequest(http.MethodGet, "/notes?sort=id&stream=true&offset=2", nil), "u1")
	rec := httptest.NewRecorder()
	cols := []string{"id", "owner", "body"}
	sorts := []filter.ParsedSort{{Field: "id"}}
	ch.ServeStreamingList(req.Context(), rec, req, cols, nil, nil, sorts, 1, 20, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, `"n1"`) || strings.Contains(body, `"n2"`) {
		t.Fatalf("stream offset=2 did not skip rows: %s", body)
	}
	if !strings.Contains(body, `"n3"`) {
		t.Fatalf("stream offset=2 dropped the wrong rows: %s", body)
	}
}
