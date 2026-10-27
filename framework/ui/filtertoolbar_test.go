package ui

import (
	"strconv"
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
		`data-cui-comp="ui-filter-toolbar"`,
		`<form`,
		`method="GET"`,
		`action="/customers"`,
		`role="search"`,
		`aria-label="Filters"`,
		// select facet composes ui.Select
		`data-cui-comp="ui-select"`,
		`name="status"`,
		`>Status<`,     // label text
		`>Open<`,       // option
		`>All Status<`, // auto-prepended clear option
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

// A facet value from the URL that no option lists selected nothing: the
// select showed "all" and the pills checked none while the toolbar
// counted the filter as applied, and Apply then dropped it. The value
// draws as its own chosen option, so the control shows it and Apply
// keeps it.
func TestFacetKeepsUnlistedValue(t *testing.T) {
	for _, kind := range []FacetKind{FacetSelect, FacetPills} {
		out := string(FilterToolbar(FilterToolbarConfig{
			Action: "/tickets",
			Facets: []Facet{{
				Name: "priority", Label: "Priority", Kind: kind, Value: "urgent",
				Options: []FacetOption{{Label: "Low", Value: "low"}, {Label: "High", Value: "high"}},
			}},
		}))
		tag := optionTagFor(out, "urgent")
		if kind == FacetPills {
			tag = inputTagFor(out, "urgent")
		}
		if !strings.Contains(tag, "selected") && !strings.Contains(tag, "checked") {
			t.Errorf("kind %v: the unlisted value is not the chosen one (%q):\n%s", kind, tag, out)
		}
		if !strings.Contains(out, ">urgent<") {
			t.Errorf("kind %v: the unlisted value is not shown:\n%s", kind, out)
		}
		for _, other := range []string{"", "low", "high"} {
			o := optionTagFor(out, other)
			if kind == FacetPills {
				o = inputTagFor(out, other)
			}
			if strings.Contains(o, "selected") || strings.Contains(o, "checked") {
				t.Errorf("kind %v: %q is chosen beside the unlisted value: %s", kind, other, o)
			}
		}
	}
	// A listed value draws once, as its own option.
	for _, kind := range []FacetKind{FacetSelect, FacetPills} {
		out := string(FilterToolbar(FilterToolbarConfig{
			Action: "/tickets",
			Facets: []Facet{{
				Name: "priority", Label: "Priority", Kind: kind, Value: "high",
				Options: []FacetOption{{Label: "Low", Value: "low"}, {Label: "High", Value: "high"}},
			}},
		}))
		if n := strings.Count(out, `value="high"`); n != 1 {
			t.Errorf("kind %v: the listed value draws %d times:\n%s", kind, n, out)
		}
	}
}

// A sort from the URL that no Sort option lists selected nothing, so
// the browser showed the first option and Apply submitted it in place
// of the active sort. The value draws as its own chosen option.
func TestSortKeepsUnlistedValue(t *testing.T) {
	sorts := []SortOption{{Label: "Newest", Value: "-created"}, {Label: "Name", Value: "name"}}
	out := string(FilterToolbar(FilterToolbarConfig{Action: "/tickets", Sort: sorts, SortValue: "-priority"}))
	if tag := optionTagFor(out, "-priority"); !strings.Contains(tag, "selected") {
		t.Errorf("the unlisted sort is not the chosen one (%q):\n%s", tag, out)
	}
	if !strings.Contains(out, ">-priority<") {
		t.Errorf("the unlisted sort is not shown:\n%s", out)
	}
	for _, other := range []string{"-created", "name"} {
		if tag := optionTagFor(out, other); strings.Contains(tag, "selected") {
			t.Errorf("%q is chosen beside the unlisted sort: %s", other, tag)
		}
	}
	for _, v := range []string{"", "name"} {
		out := string(FilterToolbar(FilterToolbarConfig{Action: "/tickets", Sort: sorts, SortValue: v}))
		if n := strings.Count(out, "<option"); n != 2 {
			t.Errorf("SortValue %q: %d options, want the two listed:\n%s", v, n, out)
		}
	}
}

// optionTagFor returns the <option ...> open tag containing value="<val>".
func optionTagFor(html, val string) string {
	for seg := range strings.SplitSeq(html, "<option") {
		if !strings.Contains(seg, `value="`+val+`"`) {
			continue
		}
		if before, _, ok := strings.Cut(seg, ">"); ok {
			return before
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
		`fui-filter-toolbar__pill`,
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
		`name="sort"`, // default sort field name
		`>Sort by<`,   // default sort label
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
		t.Run("case-"+strconv.Itoa(i), func(t *testing.T) {
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
// search field (no facets), a common minimal list-screen filter.
func TestFilterToolbarSearchOnly(t *testing.T) {
	out := string(FilterToolbar(FilterToolbarConfig{
		Action: "/q",
		Search: &FilterSearch{Name: "q"},
	}))
	if !strings.Contains(out, `name="q"`) {
		t.Errorf("expected search field, got: %s", out)
	}
}

func TestFilterToolbarExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := FilterToolbar(FilterToolbarConfig{
		Action: "/list",
		Sort:   []SortOption{{Value: "x", Label: "X"}},
		ExtraAttrs: map[string]string{
			"data-test": "hook", "action": "javascript:alert(1)", "method": "evil", "Class": "evil",
		},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("form root missing data-test:\n%s", root)
	}
	if !strings.Contains(root, `action="/list"`) || !strings.Contains(root, `method="GET"`) {
		t.Errorf("form action/method lost their framework values:\n%s", root)
	}
	for _, banned := range []string{"evil", "javascript:"} {
		if strings.Contains(root, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, root)
		}
	}
}

func TestFilterToolbarHiddenCarriesRequestState(t *testing.T) {
	out := string(FilterToolbar(FilterToolbarConfig{
		Action: "/orders",
		Search: &FilterSearch{Name: "q"},
		Hidden: []HiddenField{{Name: "view", Value: "open"}, {Name: "tab", Value: "due_sort=amount"}},
	}))
	for _, want := range []string{
		`<input data-cui-internal="" name="view" type="hidden" value="open">`,
		`name="tab"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("hidden input %q missing:\n%s", want, out)
		}
	}
	// A hidden field duplicating a control's name would submit twice with
	// the browser's last value winning, so it is a render-time refusal.
	defer func() {
		if recover() == nil {
			t.Error("a Hidden name colliding with a control must panic")
		}
	}()
	FilterToolbar(FilterToolbarConfig{
		Action: "/orders",
		Search: &FilterSearch{Name: "q"},
		Hidden: []HiddenField{{Name: "q", Value: "stale"}},
	})
}

// The sort control's defaulted name ("sort") is owned like a named
// one: a carried sort=old beside it would submit twice.
func TestFilterHiddenRefusesDefaultSortName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a Hidden sort beside the defaulted sort control must panic")
		}
	}()
	FilterToolbar(FilterToolbarConfig{
		Action: "/orders",
		Sort:   []SortOption{{Label: "Newest", Value: "new"}},
		Hidden: []HiddenField{{Name: "sort", Value: "old"}},
	})
}

// A search with a Shortcut takes focus on that chord: the facet carries
// the shortcut hooks, aimed at its input, and a chip shows the key.
func TestFilterSearchShortcut(t *testing.T) {
	h := string(FilterToolbar(FilterToolbarConfig{
		Action: "/orders",
		Search: &FilterSearch{Name: "q", Shortcut: "/"},
	}))
	for _, want := range []string{`data-hui-shortcut-focus="/"`, `data-hui-shortcut-target="#filter-search-q"`, `data-cui-comp="ui-shortcut-hint"`} {
		if !strings.Contains(h, want) {
			t.Errorf("search shortcut misses %q:\n%s", want, h)
		}
	}
	plain := string(FilterToolbar(FilterToolbarConfig{Action: "/orders", Search: &FilterSearch{Name: "q"}}))
	if strings.Contains(plain, "data-hui-shortcut") {
		t.Errorf("a search with no Shortcut binds one:\n%s", plain)
	}
}
