package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func sweepDataPanics(t *testing.T, want string, fn func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil || !strings.Contains(fmt.Sprint(r), want) {
			t.Fatalf("want a panic holding %q, got %v", want, r)
		}
	}()
	fn()
}

func TestSweepDataCardsWrapEachCellInOneValue(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns:    []Column{{Key: "role", Header: "Role"}},
		Rows:       []Row{{Cells: map[string]render.HTML{"role": `Role <b>·</b> billing`}}},
		Responsive: ResponsiveCards,
	}))
	if !strings.Contains(h, `<span class="fui-data-table__value">Role <b>·</b> billing</span>`) {
		t.Errorf("a cards-mode cell is not one value element:\n%s", h)
	}
	h = string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "role", Header: "Role"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"role": "x"}}},
	}))
	if strings.Contains(h, "fui-data-table__value") {
		t.Errorf("a scroll-mode cell is wrapped:\n%s", h)
	}
}

func TestSweepDataTruncateExcludesWrapAndFit(t *testing.T) {
	for name, col := range map[string]Column{
		"wrap": {Key: "v", Truncate: true, Wrap: true},
		"fit":  {Key: "v", Truncate: true, Fit: true},
	} {
		t.Run(name, func(t *testing.T) {
			sweepDataPanics(t, "Truncate with Wrap or Fit", func() {
				DataTable(DataTableConfig{Columns: []Column{col}})
			})
		})
	}
}

func TestSweepDataTableStatusIsVisuallyHidden(t *testing.T) {
	h := string(DataTable(DataTableConfig{
		Columns: []Column{{Key: "a", Header: "A", Sortable: true}},
		Rows:    []Row{{Cells: map[string]render.HTML{"a": "x"}}},
	}))
	at := strings.Index(h, `role="status"`)
	if at < 0 {
		t.Fatalf("no status span:\n%s", h)
	}
	tag := h[strings.LastIndex(h[:at], "<"):at]
	if !strings.Contains(tag, "fui-visually-hidden") {
		t.Errorf("the status span is visible: %s", tag)
	}
}

func TestSweepDataPillIDsStayDistinct(t *testing.T) {
	h := string(FilterToolbar(FilterToolbarConfig{Action: "/x", Facets: []Facet{{
		Name: "car", Label: "Car", Kind: FacetPills,
		Options: []FacetOption{{Label: "Red Car", Value: "red car"}, {Label: "red car", Value: "Red Car"}},
	}}}))
	ids := map[string]int{}
	for _, part := range strings.Split(h, `id="`)[1:] {
		ids[part[:strings.Index(part, `"`)]]++
	}
	for id, n := range ids {
		if n > 1 {
			t.Errorf("id %q is drawn %d times:\n%s", id, n, h)
		}
	}
	if strings.Count(h, `type="radio"`) != 3 {
		t.Fatalf("no pills:\n%s", h)
	}
}

func TestSweepDataFilterToolbarRefusesNegativeApplied(t *testing.T) {
	sweepDataPanics(t, "Applied must not be negative", func() {
		FilterToolbar(FilterToolbarConfig{Action: "/x", Search: &FilterSearch{Name: "q"}, Applied: -1})
	})
}

func TestSweepDataChangeListNamesBothSides(t *testing.T) {
	h := string(ChangeList(ChangeListConfig{Changes: []Change{{Label: "Plan", From: "Free", To: ""}}}))
	del := h[strings.Index(h, "<del"):strings.Index(h, "</del>")]
	ins := h[strings.Index(h, "<ins"):strings.Index(h, "</ins>")]
	if !strings.Contains(del, `<span class="fui-visually-hidden"`) || !strings.Contains(del, ">from <") || !strings.Contains(del, "Free") {
		t.Errorf("the old value is not led by a hidden from: %s", del)
	}
	if !strings.Contains(ins, ">to <") || !strings.Contains(ins, "—") {
		t.Errorf("an empty new value does not draw the empty mark after a hidden to: %s", ins)
	}
	sweepDataPanics(t, "requires at least one Change", func() { ChangeList(ChangeListConfig{}) })
}

func TestSweepDataEntryListItemsKeepTheirMarker(t *testing.T) {
	css := entryListStyle.Entry().CSSFor(style.DefaultTheme())
	at := strings.Index(css, ".fui-entry-list__item {")
	if at < 0 {
		t.Fatalf("no item rule:\n%s", css)
	}
	rule := css[at : at+strings.Index(css[at:], "}")]
	if strings.Contains(rule, "display") {
		t.Errorf("the item rule sets display, which drops the rank marker: %s", rule)
	}
}

func TestSweepDataSortableListItemsIsBareRows(t *testing.T) {
	h := string(SortableListItems(SortableListConfig{Label: "Todo", Items: []SortableItem{{Key: "a", Label: "A"}}}))
	if strings.Contains(h, "data-cui-comp") || strings.Contains(h, "<ol") {
		t.Errorf("the conflict fragment is wrapped, not bare rows:\n%s", h)
	}
	if !strings.HasPrefix(strings.TrimSpace(h), "<li") {
		t.Errorf("the fragment does not start with a row:\n%s", h)
	}
}

func TestSweepDataFilterRowsBlankRowTakesFirstOperator(t *testing.T) {
	h := string(FilterRows(FilterRowsConfig{
		ID: "fr", FieldName: "f", OpName: "o", ValueName: "v",
		Legend: "Filters", FieldLabel: "Field", OpLabel: "Op", ValueLabel: "Value",
		Operators: []SelectOption{{Value: "eq", Text: "is"}, {Value: "ne", Text: "is not"}},
		Rows:      []FilterRow{{Field: "a", Op: "ne", Value: "x"}},
	}))
	blank := h[strings.Index(h, `id="fr-2-o"`):]
	blank = blank[:strings.Index(blank, "</select>")]
	if !strings.Contains(blank, `value="eq" selected`) && !strings.Contains(blank, `selected="" value="eq"`) {
		t.Errorf("the blank row's operator is not the first one:\n%s", blank)
	}
	if !strings.Contains(h, `id="fr-2-v"`) || strings.Contains(h, `id="fr-3-v"`) {
		t.Errorf("want the one row then exactly one blank row:\n%s", h)
	}
}

func TestSweepDataColumnPickerMovesOnlyShownColumns(t *testing.T) {
	h := string(ColumnPicker(ColumnPickerConfig{ID: "cols", Label: "Columns", Columns: []ColumnChoice{
		{Label: "Name", Shown: true, ToggleHref: "/l?hide=name", UpHref: "", DownHref: "/l?down=name"},
		{Label: "Email", ToggleHref: "/l?show=email", UpHref: "/l?up=email", DownHref: "/l?down=email"},
	}}))
	if strings.Count(h, "fui-colpick__move") != 2 {
		t.Errorf("want two move controls, for the shown column only:\n%s", h)
	}
	if strings.Contains(h, "up=email") || strings.Contains(h, "down=email") {
		t.Errorf("a hidden column drew move links:\n%s", h)
	}
	if !strings.Contains(h, `<span aria-disabled="true"`) && !strings.Contains(h, `aria-disabled="true" class="fui-colpick__move"`) {
		t.Errorf("an empty UpHref did not draw a disabled control:\n%s", h)
	}
}

func TestSweepDataSelectionCopyNeedsFloating(t *testing.T) {
	cfg := SelectionConfig{Copy: "/export", Bar: render.HTML(`<p>bar</p>`), Body: render.HTML(`<p>body</p>`)}
	if h := string(Selection(cfg)); strings.Contains(h, "fui-selection__copy") {
		t.Errorf("a docked bar drew the copy button:\n%s", h)
	}
	cfg.Floating = true
	if h := string(Selection(cfg)); !strings.Contains(h, "fui-selection__copy") {
		t.Errorf("a floating bar with Copy drew no copy button:\n%s", h)
	}
}
