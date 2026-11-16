package headless

import (
	"context"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	uiregistry "github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestSweepContentConfiguredHrefsAreRefused(t *testing.T) {
	refuse(t, "Href", func() { Card(CardProps{Title: "T", Href: "javascript:alert(1)"}, nil) })
	refuse(t, "Action", func() {
		Card(CardProps{Title: "T", Href: "/apps", Action: render.HTML(`<a href="/all">All</a>`)}, nil)
	})
	refuse(t, "Href", func() { StatCard(StatCardProps{Label: "L", Value: "1", Href: "//evil.example/x"}, nil) })
	refuse(t, "Action", func() {
		StepWizard(StepWizardProps{Action: "javascript:alert(1)", Steps: []WizardStep{{Heading: "S"}}}, nil)
	})
}

func TestSweepContentRPCSinksAreSameOrigin(t *testing.T) {
	refuse(t, "LazyPath", func() {
		Tree(TreeProps{ID: "t", Label: "T", LazySignalPrefix: "t",
			Nodes: []TreeNode{{ID: "v", Label: "v", LazyPath: "//evil.example/x"}}}, nil)
	})
	items := []SortableItem{{Key: "a", Label: "A"}}
	refuse(t, "RPCPath", func() {
		SortableList(SortableListProps{Label: "L", RPCPath: "https://evil.example/m", Items: items}, nil)
	})
	refuse(t, "ConflictRPC", func() {
		SortableList(SortableListProps{Label: "L", ConflictRPC: "//evil.example/c", Items: items}, nil)
	})
}

func TestSweepContentTreeIsOneTabStop(t *testing.T) {
	got := Tree(TreeProps{ID: "t", Label: "T", Nodes: []TreeNode{
		{ID: "a", Label: "A", Href: "/a"},
		{ID: "b", Label: "B", Children: []TreeNode{{ID: "c", Label: "C", Href: "/c"}}},
	}}, nil)
	if n := count(got, `tabindex="0"`); n != 1 {
		t.Errorf("%d elements are in the tab order, want the first row alone:\n%s", n, got)
	}
	has(t, got, `<a href="/a" tabindex="-1">`, "a row's link is a tab stop of its own")
	has(t, got, `<a href="/c" tabindex="-1">`, "a nested row's link is a tab stop of its own")
	has(t, got, `<button aria-hidden="true" data-hui-tree-toggle="" tabindex="-1" type="button">`,
		"the branch toggle is announced or reachable by Tab")
}

func TestSweepContentBehaviorsRegisterTheirMarkersAndRequires(t *testing.T) {
	for _, tc := range []struct {
		name, marker string
		requires     []string
	}{
		{TreeBehaviorName, "[data-hui-tree]", []string{"rpc"}},
		{SortableListBehaviorName, "[data-hui-sortable]", nil},
		{CarouselBehaviorName, "[data-hui-carousel]", nil},
		{WizardBehaviorName, "[data-hui-step-wizard]", nil},
	} {
		e, ok := uiregistry.LookupBehavior(tc.name)
		if !ok {
			t.Fatalf("%s is not registered", tc.name)
		}
		if !slices.Equal(e.Markers, []string{tc.marker}) {
			t.Errorf("%s markers = %v, want [%s]", tc.name, e.Markers, tc.marker)
		}
		if !slices.Equal(e.Requires, tc.requires) {
			t.Errorf("%s requires = %v, want %v", tc.name, e.Requires, tc.requires)
		}
	}
}

func TestSweepContentTableCarriesAnExplicitEmptyFilter(t *testing.T) {
	got := Table(TableProps{Path: "/apps",
		Query:   url.Values{"saved": {"S"}, "filter": {""}},
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}}}, nil)
	q := sortHrefs(t, got)[0].Query()
	if v, ok := q["filter"]; !ok || len(v) != 1 || v[0] != "" {
		t.Errorf("an explicit empty filter was not carried across the sort: %v", q)
	}
	if q.Get("saved") != "S" {
		t.Errorf("the saved view was dropped: %v", q)
	}
}

func TestSweepContentTableSortHrefMeetsTheAnchorPolicy(t *testing.T) {
	refuse(t, "anchor policy", func() {
		Table(TableProps{Path: "/apps%0Ax", Columns: []Column{{Key: "name", Header: "Name", Sortable: true}}}, nil)
	})
}

func TestSweepContentSelectAllHeaderIsNamedAndNeverSubmitted(t *testing.T) {
	bare := Table(TableProps{Columns: []Column{{Key: "pick", SelectAll: "ids"}}}, nil)
	has(t, bare, `<input aria-label="Select all rows" data-hui-table-select-all="ids" type="checkbox">`,
		"a header-less select-all is not named from Strings, or carries more than its hook")
	named := Table(TableProps{Columns: []Column{{Key: "pick", Header: "Pick", SelectAll: "ids"}}}, nil)
	has(t, named, `aria-label="Pick"`, "a select-all is not named by its column's Header")
	hasNot(t, named, "name=", "the select-all box would submit with the form")
}

func TestSweepContentPageSurfacesDefaultTheirHeadingLevel(t *testing.T) {
	has(t, PageHeader(PageHeaderProps{Title: "Apps", Level: 9}, nil), "<h1>", "an impossible PageHeader level did not fall back to h1")
	has(t, PageHeader(PageHeaderProps{Title: "Apps", Level: 2}, nil), "<h2>", "a PageHeader level was ignored")
	has(t, EmptyState(EmptyStateProps{Title: "None"}, nil), "<h3", "the EmptyState default is not h3")
	has(t, EmptyState(EmptyStateProps{Title: "None", Level: 7}, nil), "<h3", "an impossible EmptyState level did not fall back to h3")
	has(t, EmptyState(EmptyStateProps{Title: "None", Level: 2}, nil), "<h2", "an EmptyState level was ignored")
}

func TestSweepContentEyebrowsAreHidden(t *testing.T) {
	has(t, PageHeader(PageHeaderProps{Title: "Northwind", Eyebrow: "Customers"}, nil),
		`<p aria-hidden="true">Customers</p>`, "the page eyebrow is read beside the title")
	has(t, Section(SectionProps{Title: "Cluster", Eyebrow: "01 / Overview"}, nil),
		`<p aria-hidden="true">01 / Overview</p>`, "the section eyebrow is read beside the heading")
}

func TestSweepContentStatCardActionSitsOutsideTheLink(t *testing.T) {
	got := string(StatCard(StatCardProps{Label: "Invoices", Value: "75", Href: "/invoices",
		Icon: render.HTML(`<svg width="1" height="1"></svg>`), Action: render.HTML(`<button type="button">New</button>`)}, nil))
	open, closed, action := strings.Index(got, "<a "), strings.Index(got, "</a>"), strings.Index(got, "<button")
	if open < 0 || closed < open || action < closed {
		t.Errorf("the action is not after the label's link:\n%s", got)
	}
	has(t, render.HTML(got), `<span aria-hidden="true"><svg`, "the decorative icon is announced")
}

func TestSweepContentSelectionDrawsItsBar(t *testing.T) {
	bar, body := render.HTML(`<form id="bulk"></form>`), render.HTML(`<p>rows</p>`)
	refuse(t, "Bar", func() { Selection(SelectionProps{Body: body, Bar: " "}, nil) })
	refuse(t, "Body", func() { Selection(SelectionProps{Bar: bar}, nil) })

	above := string(Selection(SelectionProps{Bar: bar, Body: body}, nil))
	if strings.Index(above, "<form") > strings.Index(above, "<p>rows") {
		t.Errorf("a non-floating bar comes after the rows:\n%s", above)
	}
	hasNot(t, render.HTML(above), "data-hui-selection", "a non-floating selection loads the behaviour")

	floating := Selection(SelectionProps{Floating: true, Form: "bulk", Bar: bar, Body: body,
		Copy: render.HTML(`<button type="button">Copy</button>`)}, nil)
	f := string(floating)
	if strings.Index(f, "<p>rows") > strings.Index(f, "<form") {
		t.Errorf("a floating bar comes before the rows:\n%s", f)
	}
	has(t, floating, `data-hui-selection=""`, "a floating selection carries no behaviour marker")
	has(t, floating, `<span data-hui-selection-count="">0</span>`, "the count is missing")
	has(t, floating, `aria-label="Clear selection" data-cui-internal="" form="bulk" type="reset"`, "the clear control does not reset the rows' form")
	hasNot(t, floating, "data-hui-selection-copy", "a copy control without CopyURL was wired")
}

func TestSweepContentSortableRowIsTheOnlyControl(t *testing.T) {
	got := SortableList(SortableListProps{Label: "L", Items: []SortableItem{{Key: "a", Label: "A"}}}, nil)
	hasNot(t, got, "<button", "a focusable row nests a button")
	has(t, got, `<span aria-hidden="true"><svg`, "the grip is not a hidden decoration")
}

func TestSweepContentRenewSortableVersionEscapesAndSkipsEmpty(t *testing.T) {
	w := httptest.NewRecorder()
	RenewSortableVersion(w, "v 1/é")
	if got := w.Header().Get(sortableVersionHeader); got != "v%201%2F%C3%A9" {
		t.Errorf("the renewed version travels as %q, not percent-encoded", got)
	}
	empty := httptest.NewRecorder()
	RenewSortableVersion(empty, "")
	if _, ok := empty.Header()[sortableVersionHeader]; ok {
		t.Error("an empty version set the header")
	}
}

func TestSweepContentGalleryUnsafeSrcDegrades(t *testing.T) {
	got := Gallery(GalleryProps{Label: "P", Items: []GalleryItem{{Src: "javascript:alert(1)", Alt: "a"}}}, nil)
	hasNot(t, got, "javascript:", "an unsafe Src reached the markup")
	has(t, got, `href="#"`, "an unsafe Src did not degrade the link to #")
	has(t, got, `src="/__gofastr/blank.png"`, "an unsafe Src did not degrade the thumbnail to the blank stub")
}

func TestSweepContentDerivedIDsAreUnique(t *testing.T) {
	ctx := render.WithIDScope(context.Background())
	out := string(group(
		Section(SectionProps{Title: "Apps", Ctx: ctx}, nil),
		Section(SectionProps{Title: "Apps", Ctx: ctx}, nil),
		Section(SectionProps{Title: "Kept", ID: "kept", Ctx: ctx}, nil),
		Section(SectionProps{Title: "Other", ID: "section-taken", Ctx: ctx}, nil),
		Section(SectionProps{Title: "Taken", Ctx: ctx}, nil),
		Carousel(CarouselProps{Label: "Tour", Ctx: ctx, Slides: []CarouselSlide{{Content: render.Text("1")}}}, nil),
		Carousel(CarouselProps{Label: "Tour", Ctx: ctx, Slides: []CarouselSlide{{Content: render.Text("1")}}}, nil),
	))
	if n := strings.Count(out, `id="section-apps-title"`); n != 1 {
		t.Errorf("two sections with one title share an id (%d):\n%s", n, out)
	}
	if !strings.Contains(out, `id="kept-title"`) {
		t.Errorf("an explicit section ID was changed:\n%s", out)
	}
	seen := map[string]bool{}
	for _, part := range strings.Split(out, `id="`)[1:] {
		id := part[:strings.Index(part, `"`)]
		if seen[id] {
			t.Errorf("id %q appears twice:\n%s", id, out)
		}
		seen[id] = true
	}
}

func TestSweepContentStepWizardHeadingTakesScriptFocus(t *testing.T) {
	got := StepWizard(StepWizardProps{Action: "/setup", Steps: []WizardStep{{Heading: "Source"}}}, nil)
	has(t, got, `<h2 data-cui-internal="" tabindex="-1">Source</h2>`, "the step heading cannot take the module's focus after a swap")
}

func TestSweepContentJSONTreeSortsKeys(t *testing.T) {
	got := string(JSONTree(JSONTreeProps{Value: map[string]any{"zeta": 1, "alpha": 2, "mid": 3}}, nil))
	a, m, z := strings.Index(got, "alpha"), strings.Index(got, "mid"), strings.Index(got, "zeta")
	if a < 0 || a > m || m > z {
		t.Errorf("object keys are not in sorted order:\n%s", got)
	}
}
