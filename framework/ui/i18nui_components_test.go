package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

func htmlString(t *testing.T, h render.HTML) string {
	t.Helper()
	return string(h)
}

// swapCatalog backs the default translator swapDefault installs; a test
// that swaps several phrases adds them all to one catalog.
var swapCatalog *i18n.MapCatalog

// swapDefault makes the process default translator render key as val
// for the rest of the test, restoring the previous one via t.Cleanup.
// Used to prove components actually look the phrase up rather than
// emitting a parallel hardcoded string.
func swapDefault(t *testing.T, key i18n.Phrase, val string) {
	t.Helper()
	if swapCatalog == nil {
		prev := i18n.Default()
		swapCatalog = i18n.NewMapCatalog()
		i18n.SetDefault(i18n.NewTranslator(swapCatalog, "en"))
		t.Cleanup(func() {
			i18n.SetDefault(prev)
			swapCatalog = nil
		})
	}
	swapCatalog.Set("en", key.Key(), i18n.Message{Text: val})
}

// Repeater default labels come from i18nui phrases.
func TestRepeaterUsesI18nDefaults(t *testing.T) {
	swapDefault(t, i18nui.RepeaterAdd, "PROBE-ADD")
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
	swapDefault(t, i18nui.PasswordInputShow, "PROBE-SHOW")
	out := htmlString(t, PasswordInput(PasswordInputConfig{Name: "p", ID: "p"}))
	if !strings.Contains(out, "PROBE-SHOW") {
		t.Fatalf("expected PROBE-SHOW in output, got:\n%s", out)
	}
}

// StepWizard Back/Submit labels use i18nui defaults.
func TestStepWizardLabelsUseI18n(t *testing.T) {
	swapDefault(t, i18nui.StepWizardBack, "PROBE-BACK")
	swapDefault(t, i18nui.StepWizardSubmit, "PROBE-SUBMIT")
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
	swapDefault(t, i18nui.LightboxPrev, "PROBE-PREV")
	swapDefault(t, i18nui.LightboxNext, "PROBE-NEXT")
	swapDefault(t, i18nui.LightboxDownload, "PROBE-DL")
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
	swapDefault(t, i18nui.TableNoResults, "PROBE-NOPE")
	swapDefault(t, i18nui.TableEmptyDesc, "PROBE-DESC")
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
	swapDefault(t, i18nui.TableNoResults, "PROBE-NOPE")
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
	swapDefault(t, i18nui.TableSortBy, "Sortby {{column}}")
	out := htmlString(t, DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Sortable: true}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("v")}}},
	}))
	if !strings.Contains(out, `aria-label="Sortby name"`) {
		t.Fatalf("missing TVars sort aria-label:\n%s", out)
	}
}

// DataTable's sort announcement resolves the three table keys through
// i18nui: the sentence and both direction words, substituted by name
// on the server.
func TestDataTableAnnouncementI18n(t *testing.T) {
	swapDefault(t, i18nui.TableSortedBy, "PROBE-SORTED {{column}} {{direction}}")
	swapDefault(t, i18nui.TableDirAscending, "PROBE-ASC")
	swapDefault(t, i18nui.TableDirDescending, "PROBE-DESC")
	out := htmlString(t, DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("v")}}},
		SortBy:  "name",
	}))
	if !strings.Contains(out, `data-hui-table-announcement="PROBE-SORTED Name PROBE-ASC"`) {
		t.Fatalf("missing announcement probes:\n%s", out)
	}
	desc := htmlString(t, DataTable(DataTableConfig{
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("v")}}},
		SortBy:  "name", SortDir: SortDesc,
	}))
	if !strings.Contains(desc, `data-hui-table-announcement="PROBE-SORTED Name PROBE-DESC"`) {
		t.Fatalf("missing descending probe:\n%s", desc)
	}
}

// DataTable threads i18n labels into the pagination nav.
func TestDataTableThreadsI18nPagination(t *testing.T) {
	swapDefault(t, i18nui.PaginationLabel, "PROBE-PAG")
	swapDefault(t, i18nui.PaginationPrevious, "PROBE-PREV")
	swapDefault(t, i18nui.PaginationNext, "PROBE-NEXT")
	out := htmlString(t, DataTable(DataTableConfig{
		Columns: []Column{{Key: "x", Header: "X"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"x": render.Text("v")}}},
		Pagination: &PaginationConfig{
			Pages: 2, Page: 1,
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
	swapDefault(t, i18nui.FilterToolbarLabel, "PROBE-LBL")
	swapDefault(t, i18nui.FilterApply, "PROBE-APPLY")
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
	swapDefault(t, i18nui.FilterAll, "All {{label}}")
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

// Carousel "Go to slide N" dot aria-labels use TVars. The words moved
// to the headless Strings table with the primitive: the key the bridge
// reads is the Hui one ({{n}}-shaped).
func TestCarouselGoToSlideTVars(t *testing.T) {
	swapDefault(t, i18nui.HuiCarouselGoToSlide, "Goto {{n}}")
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
