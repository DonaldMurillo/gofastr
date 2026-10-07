package entityui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The columns menu: ?cols= names the shown columns in display order.
// The menu is a disclosure of checkbox items, each a link that rewrites
// cols with its field toggled, the title field pinned on, and a reset
// link that drops cols; the read asks only for the shown columns.

func columnsUI(t *testing.T) *testUI {
	t.Helper()
	return newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
}

// colOrder reports the order the table's headers carry for want, or
// -1. Only the DataTable's own <table> is searched, so the columns
// menu's checkboxes never satisfy a header assertion.
func colOrder(html string, want ...string) int {
	start := strings.Index(html, "<table")
	end := strings.Index(html, "</table>")
	if start < 0 || end < start {
		return -1
	}
	table := html[start:end]
	pos := 0
	for _, w := range want {
		i := strings.Index(table[pos:], ">"+w+"<")
		if i < 0 {
			return -1
		}
		pos += i + 1
	}
	return pos
}

func TestColsParamReordersAndHides(t *testing.T) {
	x := columnsUI(t)
	// Default order: name, status, amount, memo.
	html := listHTML(t, x.ui.List("orders").ColumnsMenu(), x.ctx("/orders", ""))
	if colOrder(html, "Name", "Status", "Amount") < 0 {
		t.Errorf("the default columns are not in schema order:\n%s", html)
	}
	// ?cols reorders and hides.
	html = listHTML(t, x.ui.List("orders").ColumnsMenu(), x.ctx("/orders", "?cols=status,name"))
	if colOrder(html, "Status", "Name") < 0 {
		t.Errorf("?cols did not reorder Status before Name:\n%s", html)
	}
	if colOrder(html, "Amount") >= 0 {
		t.Errorf("?cols=status,name still showed Amount:\n%s", html)
	}
	// Multiple param values (the checkbox form's spelling) parse too.
	html = listHTML(t, x.ui.List("orders").ColumnsMenu(), x.ctx("/orders", "?cols=status&cols=name"))
	if colOrder(html, "Status", "Name") < 0 {
		t.Errorf("the checkbox spelling cols=status&cols=name did not apply:\n%s", html)
	}
}

func TestColsParamKeepsTitleFirst(t *testing.T) {
	x := columnsUI(t)
	// The title field carries the record link; cols may not hide it.
	html := listHTML(t, x.ui.List("orders").ColumnsMenu(), x.ctx("/orders", "?cols=status"))
	if colOrder(html, "Name", "Status") < 0 {
		t.Errorf("cols left the title field out and it was not kept first:\n%s", html)
	}
	if !strings.Contains(html, `href="/orders/o1"`) {
		t.Errorf("the kept title column lost its record link:\n%s", html)
	}
}

func TestColsParamRefusedValuesIgnored(t *testing.T) {
	x := columnsUI(t)
	for _, q := range []string{
		"?cols=notafield",      // unknown
		"?cols=notafield,name", // unknown beside a good one
		"?cols=name,name",      // duplicate
		"?cols=",               // empty
	} {
		html := listHTML(t, x.ui.List("orders").ColumnsMenu(), x.ctx("/orders", q))
		if colOrder(html, "Name", "Status", "Amount", "Memo") < 0 {
			t.Errorf("%s: a refused cols param changed the columns instead of being ignored:\n%s", q, html)
		}
		if strings.Contains(html, "Filter not applied") {
			t.Errorf("%s: a refused cols param drew the filter warning:\n%s", q, html)
		}
	}
}

func TestColsParamHiddenColumnRefused(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": {
			Fields: fields(
				schema.Field{Name: "name", Type: schema.String, Required: true},
				schema.Field{Name: "secret", Type: schema.String, Hidden: true},
			),
			Exposure: &entity.ExposureConfig{Public: true},
		}},
		map[string][]map[string]any{"orders": {
			{"id": "o1", "name": "alpha"},
			{"id": "o2", "name": "zeta"},
		}},
	)
	html := listHTML(t, x.ui.List("orders").ColumnsMenu(), x.ctx("/orders", "?cols=secret,name"))
	if !strings.Contains(html, ">Name<") {
		t.Errorf("a Hidden column changed the shown columns:\n%s", html)
	}
	// The Hidden field never becomes a column or a checkbox: no label
	// of its own. (The refused param's text still round-trips through
	// the forms' hidden inputs, as every URL param does.)
	if strings.Contains(html, ">Secret<") {
		t.Errorf("the Hidden column reached the page:\n%s", html)
	}
}

func TestColumnsMenuRendersControls(t *testing.T) {
	x := columnsUI(t)
	b := x.ui.List("orders").ColumnsMenu()
	html := listHTML(t, b, x.ctx("/orders", "?filter=status+%3D+%22open%22&sort=name&cols=name,status"))
	for _, want := range []string{
		"Columns", // the menu's trigger
		// The title field is checked and cannot be turned off.
		`aria-checked="true" aria-disabled="true"`,
		// A shown column's row turns it off: cols without it.
		`aria-checked="true" class="fui-menu__item" href="/orders?cols=name&amp;filter=status+%3D+%22open%22&amp;sort=name"`,
		// A hidden column's row turns it on, after the shown ones.
		`aria-checked="false" class="fui-menu__item" href="/orders?cols=name%2Cstatus%2Camount&amp;filter=status+%3D+%22open%22&amp;sort=name"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the columns menu is missing %q:\n%s", want, html)
		}
	}
	if !strings.Contains(html, `role="menuitemcheckbox"`) {
		t.Errorf("the column rows are not checkbox menu items:\n%s", html)
	}
	// The reset link drops cols entirely and keeps the rest.
	reset := listResetHref(html)
	if reset == "" || strings.Contains(reset, "cols=") || !strings.Contains(reset, "sort=name") || !strings.Contains(reset, "filter=") {
		t.Errorf("the reset link does not drop only cols (got %q):\n%s", reset, html)
	}
}

func TestColumnsMenuOffByDefault(t *testing.T) {
	x := columnsUI(t)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?cols=status"))
	if strings.Contains(html, "Columns") {
		t.Errorf("the columns menu drew without ColumnsMenu():\n%s", html)
	}
	// The param does not apply either: the feature is off.
	if colOrder(html, "Name", "Status", "Amount") < 0 {
		t.Errorf("?cols applied without ColumnsMenu():\n%s", html)
	}
}

// listResetHref finds the reset link's href in the columns menu.
func listResetHref(html string) string {
	i := strings.Index(html, ">Reset<")
	if i < 0 {
		return ""
	}
	j := strings.LastIndex(html[:i], `href="`)
	if j < 0 {
		return ""
	}
	rest := html[j+len(`href="`):]
	k := strings.Index(rest, `"`)
	if k < 0 {
		return ""
	}
	return rest[:k]
}

// Unchecking the last column beside the pinned title leaves the title
// alone: the item links to cols naming only it, and that list draws
// just the title column.
func TestColumnsMenuReachesTitleOnly(t *testing.T) {
	x := columnsUI(t)
	html := listHTML(t, x.ui.List("orders").ColumnsMenu(), x.ctx("/orders", "?cols=name,status"))
	if !strings.Contains(html, `href="/orders?cols=name" role="menuitemcheckbox"`) {
		t.Fatalf("unchecking Status does not link to the title alone:\n%s", html)
	}
	html = listHTML(t, x.ui.List("orders").ColumnsMenu(), x.ctx("/orders", "?cols=name"))
	if colOrder(html, "Name") < 0 || colOrder(html, "Status") >= 0 || colOrder(html, "Amount") >= 0 {
		t.Errorf("cols=name did not draw the title column alone:\n%s", html)
	}
}

// The filter form carries the URL's other params as hidden inputs, every
// value of a repeated one: another list's cols=a&cols=b on the same page
// survives a search.
func TestFilterFormCarriesRepeatedParams(t *testing.T) {
	x := columnsUI(t)
	html := listHTML(t, x.ui.List("orders").ColumnsMenu(), x.ctx("/orders", "?tag=a&tag=b"))
	for _, v := range []string{"a", "b"} {
		if !strings.Contains(html, `<input data-cui-internal="" name="tag" type="hidden" value="`+v+`">`) {
			t.Errorf("the filter form dropped tag=%s:\n%s", v, html)
		}
	}
}
