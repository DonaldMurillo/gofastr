package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/patterns/pagination"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func htmlString(t *testing.T, h render.HTML) string {
	t.Helper()
	return string(h)
}

// swapDefault temporarily overrides an i18nui default and restores
// it via t.Cleanup. Used to prove components actually look the key
// up rather than emitting a parallel hardcoded string.
func swapDefault(t *testing.T, key i18nui.Key, val string) {
	t.Helper()
	prev := i18nui.Defaults[key]
	i18nui.Defaults[key] = val
	t.Cleanup(func() { i18nui.Defaults[key] = prev })
}

// Repeater default labels come from i18nui.Defaults.
func TestRepeaterUsesI18nDefaults(t *testing.T) {
	swapDefault(t, i18nui.KeyRepeaterAdd, "PROBE-ADD")
	out := htmlString(t, Repeater(RepeaterConfig{Name: "x"}))
	if !strings.Contains(out, "PROBE-ADD") {
		t.Fatalf("expected PROBE-ADD in output, got:\n%s", out)
	}
}

// Repeater AddLabel override still wins.
func TestRepeaterCustomAddLabelWins(t *testing.T) {
	out := htmlString(t, Repeater(RepeaterConfig{Name: "x", AddLabel: "Custom add"}))
	if !strings.Contains(out, "Custom add") {
		t.Fatalf("expected custom label, got:\n%s", out)
	}
	if strings.Contains(out, "Add item") {
		t.Fatalf("default leaked when override set, got:\n%s", out)
	}
}

// PasswordInput show toggle aria-label uses i18nui default.
func TestPasswordInputShowUsesI18n(t *testing.T) {
	swapDefault(t, i18nui.KeyPasswordInputShow, "PROBE-SHOW")
	out := htmlString(t, PasswordInput(PasswordInputConfig{Name: "p", ID: "p"}))
	if !strings.Contains(out, "PROBE-SHOW") {
		t.Fatalf("expected PROBE-SHOW in output, got:\n%s", out)
	}
}

// StepWizard Back/Submit labels use i18nui defaults.
func TestStepWizardLabelsUseI18n(t *testing.T) {
	swapDefault(t, i18nui.KeyStepWizardBack, "PROBE-BACK")
	swapDefault(t, i18nui.KeyStepWizardSubmit, "PROBE-SUBMIT")
	out := htmlString(t, StepWizard(StepWizardConfig{
		Steps: []StepWizardStep{
			{Heading: "A"},
			{Heading: "B"},
		},
		CurrentStep: 1,
		Action:      "/x",
	}))
	if !strings.Contains(out, "PROBE-BACK") {
		t.Fatalf("missing PROBE-BACK, got:\n%s", out)
	}
	if !strings.Contains(out, "PROBE-SUBMIT") {
		t.Fatalf("missing PROBE-SUBMIT, got:\n%s", out)
	}
}

// Lightbox nav/download aria-labels use i18nui defaults.
func TestLightboxLabelsUseI18n(t *testing.T) {
	swapDefault(t, i18nui.KeyLightboxPrev, "PROBE-PREV")
	swapDefault(t, i18nui.KeyLightboxNext, "PROBE-NEXT")
	swapDefault(t, i18nui.KeyLightboxDownload, "PROBE-DL")
	slot := &lightboxSlot{name: "lb", label: "x", navArrows: true, allowDownload: true}
	out := string(slot.Render())
	if !strings.Contains(out, "PROBE-PREV") || !strings.Contains(out, "PROBE-NEXT") {
		t.Fatalf("missing nav probes, got:\n%s", out)
	}
	if !strings.Contains(out, "PROBE-DL") {
		t.Fatalf("missing download probe, got:\n%s", out)
	}
}

// DataTable empty-state title + description come from i18nui defaults.
func TestDataTableEmptyStateI18n(t *testing.T) {
	swapDefault(t, i18nui.KeyTableNoResults, "PROBE-NOPE")
	swapDefault(t, i18nui.KeyTableEmptyDesc, "PROBE-DESC")
	out := htmlString(t, DataTable(DataTableConfig{
		Columns: []Column{{Key: "x", Header: "X"}},
	}))
	if !strings.Contains(out, "PROBE-NOPE") {
		t.Fatalf("missing empty title probe:\n%s", out)
	}
	if !strings.Contains(out, "PROBE-DESC") {
		t.Fatalf("missing empty desc probe:\n%s", out)
	}
}

// DataTable empty-state Title override wins over the i18nui default.
func TestDataTableEmptyTitleOverrideWins(t *testing.T) {
	swapDefault(t, i18nui.KeyTableNoResults, "PROBE-NOPE")
	out := htmlString(t, DataTable(DataTableConfig{
		Columns: []Column{{Key: "x", Header: "X"}},
		Empty:   EmptyStateConfig{Title: "Custom title"},
	}))
	if !strings.Contains(out, "Custom title") {
		t.Fatalf("custom title missing:\n%s", out)
	}
	if strings.Contains(out, "PROBE-NOPE") {
		t.Fatalf("default leaked when override set:\n%s", out)
	}
}

// DataTable sortable empty-header column uses TVars for its aria-label.
func TestDataTableSortAriaLabelTVars(t *testing.T) {
	swapDefault(t, i18nui.KeyTableSortBy, "Sortby {column}")
	out := htmlString(t, DataTable(DataTableConfig{
		Columns:         []Column{{Key: "name", Sortable: true}},
		SortHrefPattern: "?sort=%s&dir=%s",
		Rows:            []Row{{Cells: map[string]render.HTML{"name": render.Text("v")}}},
	}))
	if !strings.Contains(out, `aria-label="Sortby name"`) {
		t.Fatalf("missing TVars sort aria-label:\n%s", out)
	}
}

// DataTable threads i18n labels into the pagination nav.
func TestDataTableThreadsI18nPagination(t *testing.T) {
	swapDefault(t, i18nui.KeyPaginationLabel, "PROBE-PAG")
	swapDefault(t, i18nui.KeyPaginationPrevious, "PROBE-PREV")
	swapDefault(t, i18nui.KeyPaginationNext, "PROBE-NEXT")
	out := htmlString(t, DataTable(DataTableConfig{
		Columns: []Column{{Key: "x", Header: "X"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"x": render.Text("v")}}},
		Pagination: &pagination.Config{
			Total: 2, Current: 1, HrefPattern: "?p=%d",
		},
	}))
	for _, want := range []string{"PROBE-PAG", "PROBE-PREV", "PROBE-NEXT"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing pagination probe %q:\n%s", want, out)
		}
	}
}

// FilterToolbar uses i18n defaults for its landmark label + Apply.
func TestFilterToolbarUsesI18n(t *testing.T) {
	swapDefault(t, i18nui.KeyFilterToolbarLabel, "PROBE-LBL")
	swapDefault(t, i18nui.KeyFilterApply, "PROBE-APPLY")
	out := htmlString(t, FilterToolbar(FilterToolbarConfig{
		Action: "/list",
		Facets: []Facet{{
			Name: "f", Label: "F",
			Options: []FacetOption{{Value: "a", Label: "A"}},
		}},
	}))
	if !strings.Contains(out, "PROBE-LBL") {
		t.Fatalf("missing toolbar label probe:\n%s", out)
	}
	if !strings.Contains(out, "PROBE-APPLY") {
		t.Fatalf("missing apply probe:\n%s", out)
	}
}

// FilterToolbar "All <label>" select option uses TVars.
func TestFilterToolbarAllLabelTVars(t *testing.T) {
	swapDefault(t, i18nui.KeyFilterAll, "All {label}")
	out := htmlString(t, FilterToolbar(FilterToolbarConfig{
		Action: "/list",
		Facets: []Facet{{
			Name: "status", Label: "Statuses",
			Options: []FacetOption{{Value: "a", Label: "A"}},
		}},
	}))
	if !strings.Contains(out, "All Statuses") {
		t.Fatalf("missing TVars All label:\n%s", out)
	}
}

// Carousel "Go to slide N" dot aria-labels use TVars.
func TestCarouselGoToSlideTVars(t *testing.T) {
	swapDefault(t, i18nui.KeyCarouselGoTo, "Goto {slide}")
	out := htmlString(t, Carousel(CarouselConfig{
		Label:  "gallery",
		Slides: []CarouselSlide{{Content: render.Text("a")}, {Content: render.Text("b")}},
	}))
	if !strings.Contains(out, `aria-label="Goto 1"`) {
		t.Fatalf("missing goto slide 1:\n%s", out)
	}
	if !strings.Contains(out, `aria-label="Goto 2"`) {
		t.Fatalf("missing goto slide 2:\n%s", out)
	}
}
