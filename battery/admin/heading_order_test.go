package admin

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
)

var headingTag = regexp.MustCompile(`<h([1-6])[\s>]`)

// headingSkip returns the first heading that rises more than one level
// past the one before it (axe's heading-order rule), or 0, 0.
func headingSkip(body string) (prev, next int) {
	last := 0
	for _, m := range headingTag.FindAllStringSubmatch(body, -1) {
		lvl, _ := strconv.Atoi(m[1])
		if last != 0 && lvl > last+1 {
			return last, lvl
		}
		last = lvl
	}
	return 0, 0
}

// An ops page whose table sits straight under the page header has no
// h2 above it, so the table's empty state must not open at h3: axe's
// heading-order rule fails Meridian's /admin/audit once the audit table
// exists and is empty.
func TestAdminEmptyOpsPagesKeepHeadingOrder(t *testing.T) {
	db := newDB(t)
	newAuditTable(t, db)
	q := newDBQueue(t, db)
	h := mountAdmin(t, Config{DB: db, Queue: q})
	for _, path := range []string{"/admin/audit", "/admin/queue"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d", path, rr.Code)
		}
		if prev, next := headingSkip(rr.Body.String()); next != 0 {
			t.Errorf("%s: an h%d follows an h%d", path, next, prev)
		}
	}
}
