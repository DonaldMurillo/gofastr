package ui

import (
	"strings"
	"testing"
)

func TestFilterToolbarBasic(t *testing.T) {
	out := string(FilterToolbar(FilterToolbarConfig{
		Action: "/customers",
		Facets: []Facet{{
			Name:  "status",
			Label: "Status",
			Value: "open",
			Options: []FacetOption{
				{Label: "Open", Value: "open"},
				{Label: "Closed", Value: "closed"},
			},
		}},
	}))
	wants := []string{
		`data-fui-comp="ui-filter-toolbar"`,
		`<form`,
		`method="GET"`,
		`action="/customers"`,
		`role="search"`,
		`aria-label="Filters"`,
		// select facet composes ui.Select
		`data-fui-comp="ui-select"`,
		`name="status"`,
		`>Status<`,        // label text
		`>Open<`,          // option
		`>All Status<`,    // auto-prepended clear option
		// Apply submit + Reset link compose ui.Button / ui.LinkButton
		`type="submit"`,
		`>Apply<`,
		`>Reset<`,
		`href="/customers"`, // Reset clears all params
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("FilterToolbar missing %q\nout: %s", w, out)
		}
	}
}

func TestFilterToolbarSelectSelected(t *testing.T) {
	out := string(FilterToolbar(FilterToolbarConfig{
		Action: "/tickets",
		Facets: []Facet{{
			Name:  "priority",
			Label: "Priority",
			Value: "high",
			Options: []FacetOption{
				{Label: "Low", Value: "low"},
				{Label: "High", Value: "high"},
			},
		}},
	}))
	// The <option value="high"> must carry selected; the clear option must not.
	highOpt := optionTagFor(out, "high")
	if !strings.Contains(highOpt, "selected") {
		t.Errorf("expected high option selected, got: %s", highOpt)
	}
	if strings.Contains(optionTagFor(out, "low"), "selected") {
		t.Errorf("did not expect low option selected")
	}
}

// optionTagFor returns the <option ...> open tag containing value="<val>".
func optionTagFor(html, val string) string {
	for _, seg := range strings.Split(html, "<option") {
		if !strings.Contains(seg, `value="`+val+`"`) {
			continue
		}
		if end := strings.Index(seg, ">"); end >= 0 {
			return seg[:end]
		}
	}
	return ""
}

func TestFilterToolbarPills(t *testing.T) {
	out := string(FilterToolbar(FilterToolbarConfig{
		Action: "/tickets",
		Facets: []Facet{{
			Name:  "state",
			Label: "State",
			Kind:  FacetPills,
			Value: "waiting",
			Options: []FacetOption{
				{Label: "Open", Value: "open"},
				{Label: "Waiting On Customer", Value: "waiting"},
			},
		}},
	}))
	wants := []string{
		`<fieldset`,
		`<legend`,
		`>State<`,
		`type="radio"`,
		`name="state"`,
		`value="waiting"`,
		`Waiting On Customer`,
		`ui-filter-toolbar__pill`,
		`>All<`, // auto-prepended clear pill
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("FilterToolbar pills missing %q\nout: %s", w, out)
		}
	}
	// The checked radio must be the "waiting" value.
	waitingTag := inputTagFor(out, "waiting")
	if !strings.Contains(waitingTag, "checked") {
		t.Errorf("expected waiting pill checked, got: %s", waitingTag)
	}
	if strings.Contains(inputTagFor(out, "open"), "checked") {
		t.Errorf("did not expect open pill checked")
	}
}

func TestFilterToolbarSearchAndSort(t *testing.T) {
	out := string(FilterToolbar(FilterToolbarConfig{
		Action: "/customers",
		Facets: []Facet{{
			Name:    "plan",
			Label:   "Plan",
			Options: []FacetOption{{Label: "Pro", Value: "pro"}},
		}},
		Search: &FilterSearch{Name: "q", Value: "acme", Placeholder: "Search customers…"},
		Sort: []SortOption{
			{Label: "Newest", Value: "created_desc"},
			{Label: "Name A–Z", Value: "name_asc"},
		},
		SortValue: "name_asc",
	}))
	wants := []string{
		`name="q"`,
		`value="acme"`,
		`Search customers…`,
		`name="sort"`,     // default sort field name
		`>Sort by<`,       // default sort label
		`>Newest<`,
		`>Name A–Z<`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("FilterToolbar search/sort missing %q\nout: %s", w, out)
		}
	}
	if !strings.Contains(optionTagFor(out, "name_asc"), "selected") {
		t.Errorf("expected sort value name_asc selected")
	}
}

func TestFilterToolbarHideReset(t *testing.T) {
	out := string(FilterToolbar(FilterToolbarConfig{
		Action:    "/x",
		HideReset: true,
		Facets: []Facet{{
			Name:    "a",
			Label:   "A",
			Options: []FacetOption{{Label: "One", Value: "1"}},
		}},
	}))
	if strings.Contains(out, ">Reset<") {
		t.Errorf("expected no Reset link when HideReset set")
	}
}

func TestFilterToolbarCustomLabels(t *testing.T) {
	out := string(FilterToolbar(FilterToolbarConfig{
		Action:     "/x",
		Label:      "Refine results",
		ApplyLabel: "Filter",
		ResetLabel: "Clear",
		Facets: []Facet{{
			Name:    "a",
			Label:   "A",
			Options: []FacetOption{{Label: "One", Value: "1"}},
		}},
	}))
	for _, w := range []string{`aria-label="Refine results"`, `>Filter<`, `>Clear<`} {
		if !strings.Contains(out, w) {
			t.Errorf("FilterToolbar custom label missing %q", w)
		}
	}
}

func TestFilterToolbarPanics(t *testing.T) {
	cases := []func(){
		func() { // no Action
			FilterToolbar(FilterToolbarConfig{Facets: []Facet{{Name: "a", Label: "A", Options: []FacetOption{{Label: "x", Value: "x"}}}}})
		},
		func() { // no facets, no search, no sort → empty toolbar is useless
			FilterToolbar(FilterToolbarConfig{Action: "/x"})
		},
		func() { // facet without Name
			FilterToolbar(FilterToolbarConfig{Action: "/x", Facets: []Facet{{Label: "A", Options: []FacetOption{{Label: "x", Value: "x"}}}}})
		},
		func() { // facet without options
			FilterToolbar(FilterToolbarConfig{Action: "/x", Facets: []Facet{{Name: "a", Label: "A"}}})
		},
	}
	for i, fn := range cases {
		t.Run("case-"+itoaSmall(i), func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("expected panic")
				}
			}()
			fn()
		})
	}
}

// TestFilterToolbarSearchOnly proves the toolbar is valid with only a
// search field (no facets) — a common minimal list-screen filter.
func TestFilterToolbarSearchOnly(t *testing.T) {
	out := string(FilterToolbar(FilterToolbarConfig{
		Action: "/q",
		Search: &FilterSearch{Name: "q"},
	}))
	if !strings.Contains(out, `name="q"`) {
		t.Errorf("expected search field, got: %s", out)
	}
}
