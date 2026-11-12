package crud

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// pagination.ParseCursorPagination turned any direction it did not know
// into "forward", so ?direction=back (or a typo) paged forward with a
// 200 and the 400 crud keeps for it never fired. The docs say only
// forward and backward are accepted; anything else is now refused, and
// an absent direction is still forward.
func TestCursorDirectionJunkRefused(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) {
		if c.Pagination == nil {
			c.Pagination = &entity.PaginationConfig{}
		}
		c.Pagination.CursorField = "seq"
	}, 3)
	get := func(query string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		ch.List()(rec, withTestUser(httptest.NewRequest(http.MethodGet, "/items"+query, nil), "u1"))
		return rec
	}
	// A direction that does not decode (%ZZ, forward%, forward;x) is the
	// list's undecodable-query refusal (TestListRefusesUndecodableQuery).
	for _, d := range []string{"back", "Forward", "backwards", "forward%0d%0a", "%20forward"} {
		rec := get("?cursor=&direction=" + d)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "bad_direction") {
			t.Errorf("direction=%s = %d %s, want 400 bad_direction", d, rec.Code, rec.Body)
		}
	}
	for _, q := range []string{"?cursor=", "?cursor=&direction=", "?cursor=&direction=forward", "?cursor=&direction=backward"} {
		if rec := get(q); rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d %s, want 200", q, rec.Code, rec.Body)
		}
	}
}
