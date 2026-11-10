package crud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestListEnvelopePageFollowsOffset: an explicit ?offset= replaces the
// page-derived skip, so the envelope's page is the page the first
// returned row sits on. It answered the ?page= sent (or 1), so a pager
// reading ?offset=40&limit=20 drew "page 1 of 3" over the third page's
// rows, and ?page=2&offset=0 named the first rows page 2.
func TestListEnvelopePageFollowsOffset(t *testing.T) {
	db := setupDB(t, `CREATE TABLE docs (id TEXT PRIMARY KEY, title TEXT)`)
	ent := entity.Define("docs", entity.EntityConfig{
		Name: "docs", Table: "docs",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	for i := range 7 {
		if _, err := db.Exec(`INSERT INTO docs (id, title) VALUES (?, ?)`, fmt.Sprintf("d%d", i), "t"); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query     string
		wantPage  int
		wantFirst string
	}{
		{"offset=4&limit=2", 3, "d4"},
		{"offset=5&limit=2", 3, "d5"}, // mid-page: the page that row is on
		{"page=2&offset=0&limit=2", 1, "d0"},
		{"offset=0&limit=2", 1, "d0"},
		{"page=3&limit=2", 3, "d4"},           // no offset: the page sent
		{"offset=-1&page=2&limit=2", 2, "d2"}, // a refused offset keeps the page
	} {
		// The buffered list and ?stream=true write the same envelope.
		for _, mode := range []string{"", "&stream=true"} {
			query := tc.query + mode
			req := withTestUser(httptest.NewRequest(http.MethodGet, "/docs?sort=id&"+query, nil), "u1")
			rec := httptest.NewRecorder()
			ch.List()(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("?%s = %d %s", query, rec.Code, rec.Body.String())
			}
			resp := decodeListResponse(t, rec.Body.String())
			if len(resp.Data) == 0 || resp.Data[0]["id"] != tc.wantFirst {
				t.Fatalf("?%s first row = %v, want %s", query, resp.Data, tc.wantFirst)
			}
			if resp.Page != tc.wantPage {
				t.Errorf("?%s envelope page = %d, want %d (first row %s)", query, resp.Page, tc.wantPage, tc.wantFirst)
			}
		}
	}
}
