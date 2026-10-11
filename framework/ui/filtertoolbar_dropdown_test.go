package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func dropdownToolbar(extra ...func(*FilterToolbarConfig)) string {
	cfg := FilterToolbarConfig{
		Action:   "/invoices",
		Dropdown: true,
		Search:   &FilterSearch{Name: "q", Value: "acme"},
		Facets: []Facet{{Name: "status", Label: "Status", Value: "paid",
			Options: []FacetOption{{Label: "Paid", Value: "paid"}, {Label: "Open", Value: "open"}}}},
		Extra:   []render.HTML{render.HTML(`<input name="filter" aria-label="Filter">`)},
		Applied: 1,
		Tools:   []render.HTML{render.HTML(`<a href="/invoices?cols=amount">Columns</a>`)},
	}
	for _, f := range extra {
		f(&cfg)
	}
	return string(FilterToolbar(cfg))
}

// In dropdown mode the row is the search, the Filters button and the
// tools; the facets, the extra field and the one Apply/Reset pair are
// in the panel, all inside the one form.
func TestFilterToolbarDropdownLayout(t *testing.T) {
	out := dropdownToolbar()
	if strings.Count(out, "<form") != 1 {
		t.Fatalf("want one form:\n%s", out)
	}
	panel := strings.Index(out, `class="fui-filter-toolbar__panel"`)
	if panel < 0 {
		t.Fatalf("no Filters panel:\n%s", out)
	}
	for _, inside := range []string{`name="status"`, `name="filter"`, "fui-filter-toolbar__apply", "fui-filter-toolbar__reset"} {
		if i := strings.Index(out, inside); i < panel {
			t.Errorf("%q is not inside the Filters panel:\n%s", inside, out)
		}
	}
	search := strings.Index(out, `name="q"`)
	if search < 0 || search > strings.Index(out, "fui-filter-toolbar__filters") {
		t.Errorf("the search is not in the row before the Filters button:\n%s", out)
	}
	if strings.Count(out, "fui-filter-toolbar__apply") != 1 {
		t.Errorf("want exactly one Apply:\n%s", out)
	}
	// One facet set plus one applied extra.
	if !strings.Contains(out, `<span class="fui-dropmenu__count" data-cui-internal="">2</span>`) {
		t.Errorf("the Filters badge does not count 2:\n%s", out)
	}
	if t2 := strings.Index(out, "fui-filter-toolbar__tools"); t2 < strings.Index(out, "fui-filter-toolbar__filters") {
		t.Errorf("the tools are not after the Filters button:\n%s", out)
	}
}

// Nothing to put in the panel: no Filters button, and no Apply either.
func TestFilterToolbarDropdownSearchOnly(t *testing.T) {
	out := dropdownToolbar(func(c *FilterToolbarConfig) {
		c.Facets, c.Extra, c.Applied = nil, nil, 0
	})
	if strings.Contains(out, "fui-filter-toolbar__filters") || strings.Contains(out, "fui-filter-toolbar__apply") {
		t.Errorf("an empty panel drew a Filters button:\n%s", out)
	}
	if !strings.Contains(out, `name="q"`) {
		t.Errorf("the search is missing:\n%s", out)
	}
}

func TestFilterToolbarToolsRefuseAForm(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a tool holding a <form> did not panic")
		}
	}()
	dropdownToolbar(func(c *FilterToolbarConfig) {
		c.Tools = []render.HTML{render.HTML(`<FORM method="post"></FORM>`)}
	})
}
