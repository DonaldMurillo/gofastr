package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// The refusals. Each names the missing prop in the package's voice,
// because the reader of a panic is told what to fix, not only where.

func TestPageHeaderRefusesATitlelessHeader(t *testing.T) {
	refuse(t, "Title", func() { PageHeader(PageHeaderProps{}, nil) })
}

func TestEmptyStateRefusesATitlelessRegion(t *testing.T) {
	refuse(t, "Title", func() { EmptyState(EmptyStateProps{}, nil) })
}

func TestStatCardRefusesAnUnlabelledValue(t *testing.T) {
	refuse(t, "Label", func() { StatCard(StatCardProps{Value: "12"}, nil) })
	refuse(t, "Value", func() { StatCard(StatCardProps{Label: "Builds"}, nil) })
}

func TestDetailListRefusesAnEmptyRecord(t *testing.T) {
	refuse(t, "Row", func() { DetailList(DetailListProps{}, nil) })
	refuse(t, "Label", func() {
		DetailList(DetailListProps{Rows: []DetailRow{{Value: render.Text("x")}}}, nil)
	})
	refuse(t, "Value", func() {
		DetailList(DetailListProps{Rows: []DetailRow{{Label: "Name"}}}, nil)
	})
}

// A group error with no ID and no Legend has no stable name to hang
// the error's id from, so the complaint would render with nothing
// able to point at it.
func TestFieldsetRefusesAnUnnameableGroupError(t *testing.T) {
	refuse(t, "Error", func() {
		Fieldset(FieldsetProps{Error: "Pick a role."}, nil)
	})
}

// The a11y facts: what each new primitive tells assistive technology,
// asserted on the markup the primitives render.

// An empty state is a place the reader arrives at; a named region is
// what makes it findable. With an explicit ID the heading carries
// <ID>-title and the region is labelled by it; without one the region
// is named by an aria-label equal to the Title and the heading carries
// no id — two panels that share a title share no id.
func TestEmptyStateIsARegionNamedByItsHeading(t *testing.T) {
	got := EmptyState(EmptyStateProps{Title: "No apps yet", Description: "Deploy one."}, nil)
	has(t, got, `role="region"`, "an empty state is not a named region")
	has(t, got, `aria-label="No apps yet"`, "the region is not named")
	hasNot(t, got, "aria-labelledby", "a state with no ID claimed a labelledby reference")
	hasNot(t, got, "id=", "the heading carries an id nothing anchors to")
	// An explicit ID names the heading's id instead, and the region's
	// label resolves to the heading inside the same render.
	named := EmptyState(EmptyStateProps{Title: "No apps yet", ID: "apps-empty"}, nil)
	has(t, named, `aria-labelledby="apps-empty-title"`, "an explicit ID does not name the heading")
	has(t, named, `<h3 id="apps-empty-title">`, "the heading does not carry the referenced id")
	// Two empty states with one title on one render share no id.
	twice := group(
		EmptyState(EmptyStateProps{Title: "No results"}, nil),
		EmptyState(EmptyStateProps{Title: "No results"}, nil))
	if n := strings.Count(string(twice), "id="); n != 0 {
		t.Errorf("two titleless empty states emitted %d ids:\n%s", n, twice)
	}
	if n := strings.Count(string(twice), `aria-label="No results"`); n != 2 {
		t.Errorf("two empty states named their own regions %d times:\n%s", n, twice)
	}
}

// The header is a plain header: the banner role belongs to the
// top-level page header, and whether this one is that is the page's
// decision, not the component's.
func TestPageHeaderClaimsNoBannerRole(t *testing.T) {
	got := PageHeader(PageHeaderProps{Title: "Apps"}, nil)
	has(t, got, "<header>", "the element is not a header")
	hasNot(t, got, "role=", "the component claimed a role the page owns")
	hasNot(t, got, `<h2`, "the default heading level is not the page's h1")
	has(t, got, "<h1>Apps</h1>", "the title is not the page's h1")
}

// A label before its value is one fact; a value before its label is a
// number the reader has to wait to understand.
func TestStatCardReadsLabelFirst(t *testing.T) {
	got := string(StatCard(StatCardProps{Label: "Active users", Value: "12,483"}, nil))
	labelAt := strings.Index(got, "Active users")
	valueAt := strings.Index(got, "12,483")
	if labelAt == -1 || valueAt == -1 {
		t.Fatalf("the card does not carry its label and value:\n%s", got)
	}
	if labelAt > valueAt {
		t.Errorf("the value is read before the label that names it:\n%s", got)
	}
}

// The direction lands where a styled layer can key on it — the trend
// part's variant class and a data-direction attribute — and a value
// outside the vocabulary is refused rather than painted as a trend it
// is not.
func TestStatCardDirectionLandsOnTheTrend(t *testing.T) {
	got := StatCard(StatCardProps{Label: "Active users", Value: "12,483",
		Trend: "+12% vs. last week", Direction: "up"}, Classes{
		PartStatTrend:          "trend",
		Part("stat-trend--up"): "trend--up",
	})
	has(t, got, `class="trend trend--up"`, "the direction variant did not land on the trend part")
	has(t, got, `data-direction="up"`, "the direction is not data the sheet can key on")

	refuse(t, "Direction", func() {
		StatCard(StatCardProps{Label: "MRR", Value: "1", Trend: "+1", Direction: "sideways"}, nil)
	})
}

// The description list is the contract: a div between the dt and its
// dd keeps the pair addressable, and the elements still pair in
// reading order.
func TestDetailListRendersADescriptionList(t *testing.T) {
	got := DetailList(DetailListProps{Rows: []DetailRow{
		{Label: "Name", Value: render.Text("blog")},
		{Label: "Status", Value: render.Text("running")},
	}}, nil)
	has(t, got, "<dl>", "the record is not a description list")
	if n := strings.Count(string(got), "<dt>"); n != 2 {
		t.Errorf("rendered %d terms, wanted 2:\n%s", n, got)
	}
	if n := strings.Count(string(got), "<dd>"); n != 2 {
		t.Errorf("rendered %d values, wanted 2:\n%s", n, got)
	}
	// The value a row renders is markup, so a badge or a link rides in
	// the dd without the caller re-wrapping it.
	has(t, got, `<dd>blog</dd>`, "the value is not in the dd")
}

// The legend is a real legend in a real fieldset: the native group
// semantic, not a div pretending with aria.
func TestFieldsetRendersTheNativeGroupSemantic(t *testing.T) {
	got := Fieldset(FieldsetProps{Legend: "Notifications"}, nil, render.HTML("<input name=a>"))
	has(t, got, "<fieldset>", "the group is not a fieldset")
	has(t, got, "<legend>Notifications</legend>", "the legend is not a legend element")
	// The heading-less branch is a div, never an unlabelled fieldset.
	bare := Fieldset(FieldsetProps{}, nil, render.HTML("<input name=a>"))
	hasNot(t, bare, "fieldset", "a group with no legend rendered a fieldset nothing names")
	has(t, bare, "<div", "the heading-less group is not a plain div")
	// The group error is wired to the group by id, on the group
	// itself, where both the fields and the question are read from.
	errored := Fieldset(FieldsetProps{Legend: "Access", Error: "Pick at least one role."}, nil)
	has(t, errored, `aria-describedby="fieldset-access-error"`, "the group error is not described-by")
	has(t, errored, `<p id="fieldset-access-error">`, "the group error carries no id")
}

// The description under the legend is read with the group it
// explains: it carries an id from the same base as the error, and
// both ride the group's aria-describedby — the description first, the
// rule before the way it was broken.
func TestFieldsetDescribesItsGroupWithDescriptionAndError(t *testing.T) {
	got := Fieldset(FieldsetProps{Legend: "Access",
		Description: "Pick the roles blog may act with.",
		Error:       "Pick at least one role."}, nil)
	has(t, got, `<p id="fieldset-access-desc">`, "the description carries no id")
	has(t, got, `aria-describedby="fieldset-access-desc fieldset-access-error"`,
		"the group is not described by its description and its error")

	// The ID-or-Legend refusal covers a Description as well: with
	// neither, there is no base to hang either id from.
	refuse(t, "Description", func() {
		Fieldset(FieldsetProps{Description: "Pick one."}, nil)
	})
}
