package entityui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// The Filters dropdown holds one row per plain term of the filter
// (field, operator, value) and one blank row to add another. Submitting
// the rows writes the filter: the list narrows, the chips show the
// terms, and the links carry the filter param, never the row params.
func TestFilterRowsWriteTheFilter(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	b := x.ui.List("orders").QueryBox()
	empty := listHTML(t, b, x.ctx("/orders", ""))
	for _, want := range []string{`name="rf"`, `name="ro"`, `name="rv"`, `value="status">Status</option>`, `value="contains">contains</option>`} {
		if !strings.Contains(empty, want) {
			t.Errorf("the filter rows miss %q:\n%s", want, empty)
		}
	}
	if strings.Contains(empty, `value="memo"`) {
		t.Errorf("a NoQuery field is offered as a filter row")
	}
	// A new row starts on the entity's own first field, not the id.
	if i, j := strings.Index(empty, `value="name">Name`), strings.Index(empty, `value="id">Id`); i < 0 || j < i {
		t.Errorf("the system fields do not come last")
	}
	q := url.Values{"rf": {"status", ""}, "ro": {"=", "="}, "rv": {"paid", ""}}
	h := listHTML(t, b, x.ctx("/orders", "?"+q.Encode()))
	if strings.Contains(h, ">alpha<") || !strings.Contains(h, "zeta") {
		t.Errorf("the row did not narrow the list:\n%s", h)
	}
	if !strings.Contains(h, `status = &quot;paid&quot;`) {
		t.Errorf("no chip for the row's term:\n%s", h)
	}
	if strings.Contains(h, "rf=") || strings.Contains(h, "rv=") {
		t.Errorf("a link carries the row params:\n%s", h)
	}
	if !strings.Contains(h, "filter=status") {
		t.Errorf("no link carries the filter the rows wrote:\n%s", h)
	}
	// A row with an unknown field, a NoQuery field or an unknown
	// operator is dropped, never parsed.
	bad := url.Values{"rf": {"nope", "memo", "status"}, "ro": {"=", "=", "drop table"}, "rv": {"x", "m1", "paid"}}
	h = listHTML(t, b, x.ctx("/orders", "?"+bad.Encode()))
	if !strings.Contains(h, "alpha") || !strings.Contains(h, "zeta") {
		t.Errorf("a refused row narrowed the list:\n%s", h)
	}
	if strings.Contains(h, i18nui.Defaults[i18nui.KeyEntityFilterInvalidTitle]) {
		t.Errorf("a refused row reached the parser:\n%s", h)
	}
	// Rows and the box add up: the box keeps the or group.
	both := url.Values{"rf": {"status"}, "ro": {"!="}, "rv": {"open"}, "filter": {`name = "zeta" or name = "beta"`}}
	h = listHTML(t, b, x.ctx("/orders", "?"+both.Encode()))
	if strings.Contains(h, ">alpha<") || !strings.Contains(h, "zeta") {
		t.Errorf("rows and box did not both apply:\n%s", h)
	}
}
