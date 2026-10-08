package entityui

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// The query box: the filter typed by hand. A text control named the
// list's filter param rides the list's GET form; submitting navigates
// and the server parses the text with the same parser the chips use, so
// a bad text keeps the "filter did not apply" warning instead of
// failing the page.

func TestQueryBoxFiltersAndRoundTrips(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	b := x.ui.List("orders").QueryBox()
	html := listHTML(t, b, x.ctx("/orders", ""))
	// The box is named the list's filter param, prefilled, labelled, and
	// its reference names the queryable fields — not the NoQuery memo.
	for _, want := range []string{
		`name="filter"`,
		"Filter",
		`>name</code> <code class="fui-code" data-cui-comp="ui-code">status</code> <code class="fui-code" data-cui-comp="ui-code">amount</code>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the query box is missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "memo") {
		t.Errorf("the help text named the NoQuery field memo:\n%s", html)
	}
	// A typed filter narrows: only the open order remains, prefilled.
	filtered := listHTML(t, b, x.ctx("/orders", "?filter=status+%3D+%22open%22"))
	if strings.Contains(filtered, "zeta") {
		t.Errorf("the typed filter did not narrow the list:\n%s", filtered)
	}
	if !strings.Contains(filtered, "alpha") {
		t.Errorf("the typed filter dropped the matching row:\n%s", filtered)
	}
	if !strings.Contains(filtered, `value="status = &quot;open&quot;"`) {
		t.Errorf("the box did not prefill the filter text:\n%s", filtered)
	}
	// The chip still reads the same param, so both stay in sync.
	if !strings.Contains(filtered, `status = &quot;open&quot;`) {
		t.Errorf("the filter chip is not in sync with the box:\n%s", filtered)
	}
}

// The box says how to write a filter: an example built from the
// entity's own fields that the parser accepts as written, the operators
// and the joining words, each as code.
func TestQueryBoxExplainsSyntax(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	b := x.ui.List("orders").QueryBox()
	page := html.UnescapeString(listHTML(t, b, x.ctx("/orders", "")))
	const example = `status = "open" and amount > 100`
	for _, want := range []string{
		`placeholder="` + example + `"`,
		"Quote text",
		">" + example + "</code>",
		">!=</code>", ">>=</code>", ">contains</code>", ">in [a, b]</code>",
		">and</code>", ">or</code>", ">( )</code>",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the query box help is missing %q:\n%s", want, page)
		}
	}
	got := listHTML(t, b, x.ctx("/orders", "?filter="+url.QueryEscape(example)))
	if strings.Contains(got, "Filter not applied") {
		t.Errorf("the example does not parse:\n%s", got)
	}

	// An entity with text fields only gets a contains example that
	// parses too, never one naming the id.
	notes := newTestUI(t,
		map[string]entity.EntityConfig{"notes": {
			Fields:   fields(schema.Field{Name: "id", Type: schema.String}, schema.Field{Name: "title", Type: schema.String}),
			Exposure: &entity.ExposureConfig{Public: true},
		}},
		map[string][]map[string]any{"notes": {{"id": "n1", "title": "a note"}}},
	)
	nb := notes.ui.List("notes").QueryBox()
	page = html.UnescapeString(listHTML(t, nb, notes.ctx("/notes", "")))
	const plain = `title contains "a"`
	if !strings.Contains(page, `placeholder="`+plain+`"`) {
		t.Errorf("want the %q example:\n%s", plain, page)
	}
	got = listHTML(t, nb, notes.ctx("/notes", "?filter="+url.QueryEscape(plain)))
	if strings.Contains(got, "Filter not applied") || !strings.Contains(got, "a note") {
		t.Errorf("the text-only example does not parse:\n%s", got)
	}
}

// An enum whose first value the quotes would have to escape is left out
// of the example, as are a relation and a date; nothing left, no example.
func TestQueryExampleSkipsUnquotable(t *testing.T) {
	got := queryExample([]schema.Field{
		{Name: "kind", Type: schema.Enum, Values: []string{`say "hi"`, "plain"}},
		{Name: "owner_id", Type: schema.Relation},
		{Name: "active", Type: schema.Bool},
	})
	if got != "active = true" {
		t.Errorf("queryExample = %q, want %q", got, "active = true")
	}
	if got := queryExample([]schema.Field{{Name: "due", Type: schema.Date}}); got != "" {
		t.Errorf("a date-only entity got the example %q", got)
	}
}

func TestQueryBoxKeyedUsesNamespacedParam(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders").Key("due").QueryBox(), x.ctx("/orders", ""))
	if !strings.Contains(html, `name="due_filter"`) {
		t.Errorf("a keyed list's query box is not namespaced:\n%s", html)
	}
}

func TestQueryBoxBadTextWarnsNotFails(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders").QueryBox(), x.ctx("/orders", "?filter=status+%3D"))
	if !strings.Contains(html, "Filter not applied") {
		t.Errorf("an unparseable filter lost its warning:\n%s", html)
	}
	for _, row := range []string{"alpha", "zeta"} {
		if !strings.Contains(html, row) {
			t.Errorf("the unfiltered list lost row %s:\n%s", row, html)
		}
	}
}

// The tools are collapsibles in one exclusive group, so they take one
// panel's height at most; the query box opens only while a filter is set.
// The list's tools are one row and one form: the search, the facets
// and the typed filter submit together through the Filters dropdown's
// one Apply, and the columns menu sits on the same row.
func TestListToolsShareOneRow(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	b := x.ui.List("orders").QueryBox().ColumnsMenu()
	html := listHTML(t, b, x.ctx("/orders", "?filter=status+%3D+%22open%22"))
	form := regexp.MustCompile(`(?s)<form[^>]*id="eui-orders-toolbar".*?</form>`).FindString(html)
	if form == "" {
		t.Fatalf("no toolbar form:\n%s", html)
	}
	for _, want := range []string{
		`name="filter"`,              // the typed filter
		`data-hui-menu=`,             // the columns menu
		`class="fui-dropmenu__count`, // the Filters badge
	} {
		if !strings.Contains(form, want) {
			t.Errorf("the toolbar form is missing %q:\n%s", want, form)
		}
	}
	// The typed filter is the form's own field, never also a hidden copy.
	if n := strings.Count(form, `name="filter"`); n != 1 {
		t.Errorf("want one filter field in the form, got %d:\n%s", n, form)
	}
	if n := strings.Count(html, "fui-filter-toolbar__apply"); n != 1 {
		t.Errorf("want one Apply on the page, got %d:\n%s", n, html)
	}
	if strings.Contains(html, "fui-collapsible") {
		t.Errorf("a tool still renders as a collapsible section:\n%s", html)
	}
	if strings.Count(html, "<form") != 1 {
		t.Errorf("want the one toolbar form, got %d forms:\n%s", strings.Count(html, "<form"), html)
	}
}

func TestQueryBoxOffByDefault(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": ordersConfig()},
		map[string][]map[string]any{"orders": ordersRows()},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", ""))
	if strings.Contains(html, `name="filter"`) {
		t.Errorf("the query box rendered without QueryBox():\n%s", html)
	}
}
