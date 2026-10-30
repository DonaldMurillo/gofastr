package entityui

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The rows bind the whole query box, however its or is spelled. The box
// was grouped only when it held " or " with spaces, but the parser also
// reads `"zeta"or(...)`, so the row's and bound only the first operand
// and alpha, an open order, showed past the row status != "open"
// (2026-10-27 bug bash D27).
func TestFilterRowsBindUnspacedOr(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	b := x.ui.List("orders").QueryBox()
	q := url.Values{"rf": {"status"}, "ro": {"!="}, "rv": {"open"}, "filter": {`name = "zeta"or(name = "alpha")`}}
	h := listHTML(t, b, x.ctx("/orders", "?"+q.Encode()))
	if !strings.Contains(h, "zeta") {
		t.Fatalf("the filter matched nothing:\n%s", h)
	}
	if strings.Contains(h, ">alpha<") {
		t.Errorf("an open order showed past the row status != open:\n%s", h)
	}
	// With no row to join, the box's text comes back as typed, a bad
	// one included: it is grouped only beside rows.
	q = url.Values{"rf": {"status"}, "ro": {"="}, "rv": {""}, "filter": {`status =`}}
	h = listHTML(t, b, x.ctx("/orders", "?"+q.Encode()))
	if !strings.Contains(h, `value="status ="`) {
		t.Errorf("the box did not keep its text as typed:\n%s", h)
	}
}

// A term the rows would change on Apply stays in the box. The rows trim
// a value and drop an empty or over-long one, so a row drawn for
// name = "" or name = " zeta " lost the filter, or its spaces, when the
// page was applied unchanged (2026-10-27 bug bash D31).
func TestFilterRowsKeepUnrowableTerms(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	b := x.ui.List("orders").QueryBox()
	long := strings.Repeat("z", maxFilterRowValue+1)
	for _, v := range []string{"", " zeta ", long} {
		text := `name = "` + v + `"`
		h := listHTML(t, b, x.ctx("/orders", "?filter="+url.QueryEscape(text)))
		if want := `value="` + strings.ReplaceAll(text, `"`, "&quot;") + `"`; !strings.Contains(h, want) {
			t.Errorf("filter %q: the box does not hold the term:\n%s", text, h)
		}
		if strings.Contains(h, `value="`+v+`"`) || strings.Contains(h, `value="`+strings.TrimSpace(v)+`"`) {
			t.Errorf("filter %q: the term was drawn as a row:\n%s", text, h)
		}
	}
	// A plain value is still a row.
	h := listHTML(t, b, x.ctx("/orders", "?filter="+url.QueryEscape(`name = "zeta"`)))
	if !strings.Contains(h, `value="zeta"`) {
		t.Errorf("a plain term is not a row:\n%s", h)
	}
}
