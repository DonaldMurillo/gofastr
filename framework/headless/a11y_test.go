package headless

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func has(t *testing.T, got render.HTML, want, why string) {
	t.Helper()
	if !strings.Contains(string(got), want) {
		t.Errorf("%s\n  missing: %s\n  in: %s", why, want, got)
	}
}

func hasNot(t *testing.T, got render.HTML, unwanted, why string) {
	t.Helper()
	if strings.Contains(string(got), unwanted) {
		t.Errorf("%s\n  present: %s\n  in: %s", why, unwanted, got)
	}
}

func count(got render.HTML, sub string) int { return strings.Count(string(got), sub) }

// A hint or an error is on screen either way. Without
// aria-describedby it is on screen for sighted users only: a screen
// reader announces the label and stops, so the format rule and the
// reason the control is red never arrive.
//
// This is the regression that shipped once already — most of the text
// controls accepted DescribedBy and dropped it on the floor, so
// the field wired up a relationship to an attribute that was never
// written.
func TestEveryTextControlCarriesItsDescription(t *testing.T) {
	cases := map[string]render.HTML{
		"Input":    Input(InputProps{Name: "a", ID: "a", DescribedBy: "a-hint"}, nil),
		"Textarea": Textarea(TextareaProps{Name: "b", ID: "b", DescribedBy: "b-hint"}, nil),
		"Select": Select(SelectProps{Name: "c", ID: "c", DescribedBy: "c-hint",
			Options: []Option{{Value: "1", Label: "One"}}}, nil),
		"Password": Password(PasswordProps{Name: "d", ID: "d", DescribedBy: "d-hint"}, nil),
		"Color":    Color(ColorProps{Name: "e", ID: "e", DescribedBy: "e-hint"}, nil),
	}
	for name, got := range cases {
		has(t, got, `aria-describedby=`, name+" drops the description it was handed")
	}
}

// A field builds the relationship from the same ids it hands its
// control. The control is built by a callback for exactly this
// reason: it cannot be given an id that disagrees with the label's
// for=, or a describedby that disagrees with the hint's id.
func TestFieldWiresLabelHintAndErrorToTheControl(t *testing.T) {
	got := Field(FieldProps{Label: "Port", For: "port", Hint: "1–65535"}, nil,
		func(c FieldControl) render.HTML {
			return Input(InputProps{Name: "port", ID: c.ID, DescribedBy: c.DescribedBy}, nil)
		})
	has(t, got, `for="port"`, "the label does not point at the control")
	has(t, got, `id="port-hint"`, "the hint has no id to be referenced by")
	has(t, got, `aria-describedby="port-hint"`, "the control is not tied to its hint")

	// The error JOINS the hint in the description, ahead of it: the
	// hint is the rule the value must obey and the error is the
	// violation, so dropping the rule exactly when it was broken is
	// dropping it when it is needed most. The correction is read
	// first because it comes first.
	bad := Field(FieldProps{Label: "Port", For: "p2", Hint: "1–65535", Error: "Already in use."}, nil,
		func(c FieldControl) render.HTML {
			return Input(InputProps{Name: "p2", ID: c.ID, DescribedBy: c.DescribedBy, Invalid: c.Invalid}, nil)
		})
	has(t, bad, `aria-describedby="p2-error p2-hint"`, "the control is not tied to its error and its hint")
	has(t, bad, `aria-invalid="true"`, "an errored field does not mark its control invalid")

	// The error itself has to interrupt. A message that appears
	// silently is a message a screen reader user never learns about.
	has(t, bad, `role="alert"`, "the error is not announced")
}

// Required is stated to the parser, not only drawn. A red asterisk is
// a picture of a rule.
func TestRequiredIsAStateNotADecoration(t *testing.T) {
	got := Field(FieldProps{Label: "Name", For: "n", Required: true}, nil,
		func(c FieldControl) render.HTML {
			return Input(InputProps{Name: "n", ID: c.ID, Required: c.Required}, nil)
		})
	has(t, got, `required=""`, "a required field does not mark its control required")
}

// The label WRAPS the control. That is what makes the text a click
// target without a for/id pair to keep in sync, and it is why a 16px
// checkbox already clears WCAG 2.5.8 — the target is the row, not the
// box.
func TestChoiceLabelWrapsItsControl(t *testing.T) {
	got := Choice(ChoiceProps{Type: "checkbox", Name: "tls", Label: "Force HTTPS"}, nil)
	if !strings.HasPrefix(string(got), "<label") {
		t.Errorf("a choice is not wrapped by its label: %s", got)
	}
	has(t, got, `type="checkbox"`, "the choice is not a real checkbox")
}

// A switch submits like the checkbox it is and announces like the
// switch it looks like. role=switch is the one part that cannot be
// drawn.
func TestSwitchIsACheckboxThatSaysItIsASwitch(t *testing.T) {
	got := Switch(SwitchProps{Name: "auto", Label: "Auto-deploy"}, nil)
	has(t, got, `type="checkbox"`, "a switch that is not a checkbox does not submit")
	has(t, got, `role="switch"`, "the switch does not announce its shape")
}

// A set of choices with no question above it is as broken as an
// unlabelled input. fieldset/legend is the native pair and needs no
// aria at all.
func TestChoiceGroupCarriesItsQuestion(t *testing.T) {
	got := Group(GroupProps{Legend: "Restart policy"}, nil,
		Choice(ChoiceProps{Type: "radio", Name: "r", Value: "always", Label: "Always"}, nil))
	has(t, got, "<fieldset", "a choice group is not a fieldset")
	has(t, got, "<legend", "a choice group has no legend")
}

func TestPaginationIsNavigationWithACurrentPage(t *testing.T) {
	got := Pagination(PaginationProps{Page: 2, Pages: 5, Path: "/x", PageParam: "p",
		AriaLabel: "Pages", Island: fixtureIsland}, nil)
	has(t, got, "<nav", "pagination is not a nav landmark")
	has(t, got, `aria-current="page"`, "pagination does not mark the current page")
	if n := count(got, `aria-current="page"`); n != 1 {
		t.Errorf("exactly one page is current, found %d", n)
	}
}

// The explicit roles look redundant on a displayed <table> and are
// not: a cards collapse sets display:block on the table's elements,
// and a table element displayed as a block loses its implicit table
// semantics in Chromium and WebKit. The roles are what keep a
// collapsed table a table for assistive tech, and scope="col" is
// what ties each header to its column. Survey 2026-09-20 §4: nothing
// in the repo asserted these directly — they arrived from
// core-ui/html and were pinned only by meridian's axe runs.
func TestTableIsATableWithScopedColumnHeaders(t *testing.T) {
	got := Table(TableProps{
		Caption: "Applications",
		Columns: []Column{{Key: "name", Header: "Name", Sortable: true}, {Key: "env", Header: "Environment"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("blog"), "env": render.Text("production")}}},
	}, nil)
	// Two substrings bind every role to its element: the head with
	// both headers scoped, and the body row with both cells.
	has(t, got, `<table role="table"><caption id="table-applications-caption">Applications</caption>`+
		`<thead role="rowgroup"><tr role="row"><th aria-sort="none" role="columnheader" scope="col"><a href="?dir=asc&amp;sort=name">Name</a></th>`+
		`<th role="columnheader" scope="col">Environment</th></tr></thead>`,
		"the head lost a role, a scope, or the caption that names the table")
	has(t, got, `<tbody role="rowgroup"><tr role="row"><td data-label="Name" role="cell">blog</td><td data-label="Environment" role="cell">production</td></tr></tbody></table>`,
		"the body lost a role or a cell's label")
}

// The scroll region is keyboard-reachable (WCAG 2.1.1: a region that
// can only be scrolled with a mouse fails it, and axe reports
// scrollable-region-focusable) and, when the table has a caption,
// named by it: the focus stop announces what it is. tabindex is
// always 0 — the server cannot know whether this table overflows at
// the reader's width, so the region is reachable before it scrolls.
// Adrian Roselli's responsive-table pattern.
func TestTableScrollRegionIsFocusableAndNamedByItsCaption(t *testing.T) {
	got := Table(TableProps{ID: "apps", Caption: "Applications",
		Columns: []Column{{Key: "name", Header: "Name"}},
		Rows:    []Row{{Cells: map[string]render.HTML{"name": render.Text("blog")}}},
	}, nil)
	// One substring binds the three attributes to the one element
	// that wraps the table, and the caption's id to the name that
	// points at it: attributes render sorted, so the shape is exact.
	has(t, got, `<div aria-labelledby="apps-caption" data-hui-table-scroll="" role="region" tabindex="0"><table role="table"><caption id="apps-caption">Applications</caption>`,
		"the scroll region does not carry focus and the caption's name on the one element that wraps the table")

	unnamed := Table(TableProps{
		Columns: []Column{{Key: "name", Header: "Name"}}}, nil)
	hasNot(t, unnamed, "aria-labelledby", "a region with no caption carried a name pointing at nothing")
	has(t, unnamed, `<div data-hui-table-scroll="" role="region" tabindex="0"><table role="table">`, "the unnamed region is not the element wrapping the table")
}

func TestStepsSayWhichStepIsCurrent(t *testing.T) {
	got := Steps(StepsProps{Labels: []string{"Account", "Plan", "Pay"}, Current: 2}, nil)
	has(t, got, `aria-current=`, "the current step is not marked")
}

// Announcing is a decision with a cost in both directions, so the
// default is silence: an alert the server rendered with the page has
// not happened, and reading it as an event talks over the user on
// every load.
func TestAlertDoesNotAnnounceByDefault(t *testing.T) {
	quiet := Alert(AlertProps{Title: "3 apps need attention"}, nil)
	hasNot(t, quiet, `role="alert"`, "page furniture interrupts on load")
	hasNot(t, quiet, `role="status"`, "page furniture is announced on load")

	urgent := Alert(AlertProps{Title: "Deploy failed", Live: LiveAssertive}, nil)
	has(t, urgent, `role="alert"`, "something that must be heard is silent")

	polite := Alert(AlertProps{Title: "Backup finished", Live: LivePolite}, nil)
	has(t, polite, `role="status"`, "a completion is never announced")
}

// role="alert" already implies aria-live="assertive" and role="status"
// implies polite. Writing both is how a message gets announced twice.
func TestAlertDoesNotStateItsRoleTwice(t *testing.T) {
	got := Alert(AlertProps{Title: "Deploy failed", Live: LiveAssertive}, nil)
	hasNot(t, got, "aria-live=", "the live-region policy is stated twice")
}

// WCAG 1.4.1: colour may not be the only carrier of meaning. A red box
// is exactly that to anyone who cannot see the red, so the kind of
// message is also in the text — hidden visually, read aloud, ahead of
// the title.
func TestAlertToneSurvivesWithoutColour(t *testing.T) {
	got := Alert(AlertProps{Title: "Certificate expired", ToneWord: "Error"}, nil)
	has(t, got, "Error: ", "the tone exists only as colour")
	if strings.Index(string(got), "Error: ") > strings.Index(string(got), "Certificate expired") {
		t.Error("the tone word is read after the title it qualifies")
	}
}

// "Dismiss" three times in a row tells a screen reader user which
// nothing.
func TestAlertDismissNamesWhatItDismisses(t *testing.T) {
	got := Alert(AlertProps{Title: "Deploy failed", DismissHref: "/x", Island: fixtureIsland}, nil)
	has(t, got, `aria-label="Dismiss: Deploy failed"`, "the dismiss does not say what it closes")
}

// A <section> is a landmark only when it has an accessible name.
// Unnamed, it shows up in a screen reader's landmark list as an entry
// called "section" — so a page of six of them offers six identical,
// useless navigation targets. A region with nothing to be named by is
// better off as a div.
func TestSectionIsALandmarkOnlyWhenItIsNamed(t *testing.T) {
	named := Section(SectionProps{Title: "Running apps"}, nil, render.HTML("<p>x</p>"))
	has(t, named, "<section", "a titled region is not a landmark")
	has(t, named, "aria-labelledby=", "the landmark has no accessible name")
	has(t, named, "<h2", "the title is not a heading, so it is not a navigation target either")

	bare := Section(SectionProps{}, nil, render.HTML("<p>x</p>"))
	hasNot(t, bare, "<section", "an unnamed region still claims to be a landmark")
	hasNot(t, bare, "aria-labelledby=", "an unnamed region points at a name that does not exist")
}

// The name has to point at something that exists. A derived id keeps
// that true for a caller who never supplied one.
func TestSectionNameResolvesToItsHeading(t *testing.T) {
	got := string(Section(SectionProps{Title: "Running apps"}, nil))
	i := strings.Index(got, `aria-labelledby="`)
	if i < 0 {
		t.Fatal("no accessible name")
	}
	rest := got[i+len(`aria-labelledby="`):]
	id := rest[:strings.Index(rest, `"`)]
	if !strings.Contains(got, `id="`+id+`"`) {
		t.Errorf("aria-labelledby points at %q, which nothing in the markup has", id)
	}
}

// The heading level is a real decision, not a size. A section under an
// h1 that renders an h3 leaves a hole in the outline a screen reader
// user has to guess at.
func TestSectionHeadingLevelIsHonoured(t *testing.T) {
	has(t, Section(SectionProps{Title: "Apps", Level: 3}, nil), "<h3", "the requested level was ignored")
	has(t, Section(SectionProps{Title: "Apps"}, nil), "<h2", "the default level is not h2")
	// Out of range falls back rather than emitting <h9>, which is not
	// an element and would silently become an unknown inline tag.
	has(t, Section(SectionProps{Title: "Apps", Level: 9}, nil), "<h2", "an impossible level produced an invalid element")
}

// A separator between groups is meaningful and is an <hr>, which
// already means "thematic break" — no role to claim. A line drawn for
// looks is decoration, and announcing "separator" at every flourish on
// a page is noise a screen reader user cannot turn off.
func TestDividerSaysWhetherItMeansAnything(t *testing.T) {
	real := Divider(DividerProps{}, nil)
	has(t, real, "<hr", "a meaningful divider is not a thematic break")
	hasNot(t, real, `role="separator"`, "the role is restated over the element that already means it")

	deco := Divider(DividerProps{Decorative: true}, nil)
	has(t, deco, `aria-hidden="true"`, "a decorative line is announced")
	hasNot(t, deco, "<hr", "a decorative line uses a semantic element and then denies it")
}

// A labelled divider cannot be an <hr> — the element has no content
// model — so the role moves to whatever replaces it, and the two rules
// beside the label are decoration.
func TestLabelledDividerKeepsItsRoleAndHidesItsRules(t *testing.T) {
	got := Divider(DividerProps{Label: "or"}, nil)
	has(t, got, `role="separator"`, "a labelled divider lost its meaning")
	has(t, got, "or", "the label is not rendered")
	if n := count(got, `aria-hidden="true"`); n != 2 {
		t.Errorf("the two rules beside the label should be hidden, found %d hidden", n)
	}
}

// One lookup per axis, never a combined key: a class map keyed on
// "root--md--center" has to enumerate every gap crossed with every
// alignment, and the first pair nobody thought of renders unstyled.
func TestLayoutModifiersAreIndependent(t *testing.T) {
	sk := Classes{
		PartRoot:             "ds-stack",
		"root--gap-lg":       "ds-stack--gap-lg",
		"root--align-center": "ds-stack--align-center",
	}
	got := Stack(StackProps{Gap: "lg", Align: "center"}, sk)
	for _, cls := range []string{"ds-stack", "ds-stack--gap-lg", "ds-stack--align-center"} {
		has(t, got, cls, "a modifier was dropped, so one axis of the layout is unstyled")
	}
}

// A spinner with no label is a moving shape. role="status" is claimed
// only when it should interrupt — a spinner that arrived WITH the page
// has not happened, same rule as Alert.
func TestSpinnerSaysWhatItIsWaitingFor(t *testing.T) {
	quiet := Spinner(SpinnerProps{Label: "Loading apps"}, nil)
	has(t, quiet, "Loading apps", "the spinner has no accessible text")
	hasNot(t, quiet, `role="status"`, "a spinner on first paint announces itself over the page")

	loud := Spinner(SpinnerProps{Label: "Saving", Announce: true}, nil)
	has(t, loud, `role="status"`, "a spinner replacing content after an action is silent")

	// A spinner has no value, so it is not a progressbar: that role
	// promises a number this control does not have.
	hasNot(t, quiet, `role="progressbar"`, "a valueless spinner claims to be a progress bar")
}

// Every bar is hidden. A skeleton is a picture of content that does
// not exist, and eight empty boxes read aloud is worse than silence —
// it says what is loading once, politely, and then waits.
func TestSkeletonBarsAreNeverAnnounced(t *testing.T) {
	got := Skeleton(SkeletonProps{Label: "Loading apps", Lines: 3}, nil)
	if n := count(got, `aria-hidden="true"`); n != 3 {
		t.Errorf("all 3 bars should be hidden, found %d hidden", n)
	}
	has(t, got, "Loading apps", "nothing says what is loading")
	has(t, got, `role="status"`, "the loading message is never announced")
	// The last line of a paragraph is short; a block of equal bars
	// reads as a block rather than as text.
	has(t, got, "data-hui-skeleton-last", "the last bar is full width, so the block does not read as text")
}

// A form that fails validation changes nothing a screen reader can
// notice: focus stays put and the page looks the same to the
// accessibility tree. role="alert" is the one case where interrupting
// is right, because this genuinely just happened.
func TestValidationSummaryInterruptsAndCanTakeFocus(t *testing.T) {
	got := ValidationSummary(ValidationSummaryProps{ID: "port-errors", Errors: []FieldError{
		{For: "port", Message: "Port must be between 1 and 65535."},
	}}, nil)
	// Focusable by script, never by tab: the server sends focus here
	// after a failed submit, and it must not become a stop on the way
	// through the form.
	has(t, got, `tabindex="-1"`, "the summary cannot be focused after a failed submit")
	has(t, got, `href="#port"`, "the error does not lead to the field it is about")
}

// An empty summary that announces itself is a lie that interrupts.
// The ID is still required: a summary with no errors today is a
// summary a later render fills in, and the id must never depend on
// the error count.
func TestValidationSummaryWithNoErrorsRendersNothing(t *testing.T) {
	if got := ValidationSummary(ValidationSummaryProps{ID: "empty-errors"}, nil); got != "" {
		t.Errorf("an empty summary rendered %q, which would announce a problem that does not exist", got)
	}
}

// An error with no field to point at is still an error worth reading;
// it just cannot be a link.
func TestErrorWithoutAFieldIsStillListed(t *testing.T) {
	got := ValidationSummary(ValidationSummaryProps{ID: "session-errors", Errors: []FieldError{
		{Message: "Your session expired. Sign in and try again."},
	}}, nil)
	has(t, got, "session expired", "an error with no field id was dropped")
	hasNot(t, got, "href=", "an error with no field became a link to nowhere")
}

// Every href a component writes goes through the anchor policy, the
// summary's field links included. A For the policy refuses — control
// bytes, a backslash — is rendered as text rather than as a link
// nowhere should follow, the same fallback as a For that is empty.
func TestAnErrorWhoseFieldThePolicyRefusesIsText(t *testing.T) {
	got := ValidationSummary(ValidationSummaryProps{ID: "bad-errors", Errors: []FieldError{
		{For: "port\\evil", Message: "That field id is not a fragment."},
	}}, nil)
	has(t, got, "not a fragment", "the message was dropped")
	hasNot(t, got, "href=", "a For the anchor policy refuses became a link anyway")
}

// The ID is required because the title's id is derived from it: two
// summaries without ids on one page would share one title id, and
// both labels and both announcements would point at whichever heading
// the browser settled on.
func TestValidationSummaryRequiresAnID(t *testing.T) {
	mustRefuse(t, "a summary with no ID", func() {
		ValidationSummary(ValidationSummaryProps{Errors: []FieldError{{For: "x", Message: "m"}}}, nil)
	})
}

// The order is the content, so the list is ordered: a screen reader
// then announces position and count, which locates you in the history
// without seeing the line down the side.
func TestTimelineIsOrderedAndDated(t *testing.T) {
	got := Timeline(TimelineProps{Label: "Deploy history", Events: []Event{
		{Title: "Deployed", When: "3 days ago", Machine: "2026-09-08T10:02:00Z", Tone: "success"},
		{Title: "Build failed", When: "4 days ago", Machine: "2026-09-07T18:23:00Z", Tone: "danger"},
	}}, nil)
	has(t, got, "<ol", "a sequence of events is not an ordered list")
	// "3 days ago" is a string nothing can resolve to a moment.
	has(t, got, `datetime="2026-09-08T10:02:00Z"`, "the time is not machine-readable")
	has(t, got, "<time", "the timestamp is not a time element")
	if n := count(got, `aria-hidden="true"`); n != 2 {
		t.Errorf("the markers are a picture of the ordering and should be hidden, found %d hidden", n)
	}
}

// The control is a real file input inside its own label, so it works
// before any script: clickable anywhere in the zone, focusable,
// keyboard-operable, and it submits.
func TestUploadIsARealInputInALabel(t *testing.T) {
	got := FileUpload(FileUploadProps{Name: "backup", ID: "up", Label: "Drag a backup here"}, nil)
	has(t, got, `type="file"`, "the upload is not a file input")
	has(t, got, `for="up"`, "the zone is not a label for the input, so clicking it does nothing")
	has(t, got, `id="up"`, "there is no input for the label to point at")
	// A button inside a label swallows the label's click, and the
	// whole zone stops opening the picker.
	hasNot(t, got, "<button", "a button inside the label breaks the label")
}

// Choosing a file changes nothing a screen reader notices: the input's
// value is not read back and the names appear in silence.
func TestUploadAnnouncesWhatWasChosen(t *testing.T) {
	got := FileUpload(FileUploadProps{Name: "backup", ID: "up", Label: "Drag a backup here"}, nil)
	has(t, got, `role="status"`, "chosen files are never announced")
	hasNot(t, got, "aria-live", "the live-region policy is stated twice")
	has(t, got, "data-hui-drop-list", "there is nowhere to show the chosen names")
}

// The hint belongs to the field, not beside it: accepted types and a
// size limit are exactly what someone needs before choosing, and
// aria-describedby is what gets them read with the control.
func TestUploadHintIsTiedToTheInput(t *testing.T) {
	got := FileUpload(FileUploadProps{
		Name: "backup", ID: "up", Label: "Drag a backup here", Hint: ".tar.gz up to 2 GB"}, nil)
	has(t, got, `aria-describedby="up-accept"`, "the hint is not tied to the control")
	has(t, got, `id="up-accept"`, "the hint has no id to be referenced by")
	// "-accept", not "-hint": a Field wrapping this control derives
	// "<id>-hint" for its own hint, and two elements sharing an id
	// break both references.
	both := FileUpload(FileUploadProps{
		Name: "b", ID: "u2", Label: "L", Hint: "h", DescribedBy: "u2-hint"}, nil)
	has(t, both, `aria-describedby="u2-hint u2-accept"`, "one description replaced the other")
}

// A form that failed validation looks unchanged to the accessibility
// tree. The hook is what tells the runtime to move focus to the
// summary; without it the most common accessible-looking form is not
// one.
func TestFormWithErrorsAsksForFocus(t *testing.T) {
	withErrors := Form(FormProps{Action: "/apps", Errors: ValidationSummary(ValidationSummaryProps{
		ID: "apps-errors", Errors: []FieldError{{For: "port", Message: "Out of range."}}}, nil)}, nil)
	has(t, withErrors, "data-hui-form-errors", "nothing tells the runtime a submit failed")

	clean := Form(FormProps{Action: "/apps"}, nil)
	hasNot(t, clean, "data-hui-form-errors", "a form with no errors asks for focus it should not take")
}

// Without multipart a browser submits file inputs as names with no
// contents, which looks exactly like a server bug and is not one.
func TestMultipartFormSaysSo(t *testing.T) {
	has(t, Form(FormProps{Action: "/upload", Multipart: true}, nil), `enctype="multipart/form-data"`,
		"a form with an upload would submit empty files")
}

// Two labelled controls inside a named group make a screen reader read
// the group name before each one, so the name is opt-in.
func TestInputGroupAddsNoSemanticsByDefault(t *testing.T) {
	plain := InputGroup(InputGroupProps{}, nil, render.HTML("<input>"))
	hasNot(t, plain, "role=", "the group invented a role nobody asked for")

	named := InputGroup(InputGroupProps{Label: "Search apps"}, nil, render.HTML("<input>"))
	has(t, named, `role="group"`, "a named group is not a group")
	has(t, named, `aria-label="Search apps"`, "the group name was dropped")
}

// A message that is already in the document when it loads is not a
// change, so a live region does not announce it, and role="alert" at
// load time is reported inconsistently across screen readers. After a
// POST redirects — the shape of every action in a server-rendered app
// — the confirmation IS present at load. Focus is the reliable way to
// say it.
func TestAMessageThatIsAlreadyThereTakesFocus(t *testing.T) {
	got := Alert(AlertProps{Title: "blog restarted", Live: LivePolite, Focus: true}, nil)
	has(t, got, `tabindex="-1"`, "the message cannot take focus")
	has(t, got, "autofocus", "the message is never announced on a page that arrives carrying it")
	// Focusable, but not a tab stop: Tab from it carries on into the
	// page rather than back through it.
	hasNot(t, got, `tabindex="0"`, "the message joined the tab order")
	has(t, got, `role="status"`, "a message inserted later would announce nothing")
}

// The lifecycle's pending state belongs to the runtime alone: it is
// painted after a click and cleared when the response lands. A server
// that shipped it would render a button that is disabled and busy
// before anyone has touched it — and one that can never be clicked,
// because pending is exactly the state that ignores clicks.
func TestOptimisticActionsNeverShipPending(t *testing.T) {
	for name, got := range map[string]render.HTML{
		"OptimisticAction": OptimisticAction(OptimisticActionProps{
			Endpoint: "/follow", IdleLabel: "Follow", SuccessLabel: "Following"}, nil),
		"ToggleAction idle": ToggleAction(ToggleActionProps{
			Endpoint: "/follow", IdleLabel: "Follow", CommittedLabel: "Following"}, nil),
		"ToggleAction committed": ToggleAction(ToggleActionProps{
			Endpoint: "/follow", IdleLabel: "Follow", CommittedLabel: "Following",
			Committed: true}, nil),
	} {
		hasNot(t, got, `data-state="pending"`, name+": the server shipped a state only a click may paint")
		hasNot(t, got, "aria-busy", name+": busy is the runtime's word for an in-flight request; at load there is none")
		hasNot(t, got, "disabled", name+": a mutation button must be clickable the moment the page arrives")
	}
}

// A toggle IS its pressed state (WAI-ARIA): the label flips, but what
// a screen reader announces as the button's setting is aria-pressed,
// and it has to agree with the span the server chose to show. The
// two are built in different lines of the component; this is the test
// that they cannot disagree.
func TestToggleActionPressedMatchesItsShippedState(t *testing.T) {
	idle := ToggleAction(ToggleActionProps{
		Endpoint: "/watch", IdleLabel: "Watch", CommittedLabel: "Watching"}, nil)
	has(t, idle, `aria-pressed="false"`, "an idle toggle does not say it is unpressed")
	has(t, idle, `data-hui-action-done="" hidden=""`, "the committed label is not hidden behind the idle one")
	has(t, idle, `data-hui-action-idle="">Watch</span>`, "the idle label is hidden at rest")

	committed := ToggleAction(ToggleActionProps{
		Endpoint: "/watch", IdleLabel: "Watch", CommittedLabel: "Watching", Committed: true}, nil)
	has(t, committed, `aria-pressed="true"`, "a committed toggle does not say it is pressed")
	has(t, committed, `data-hui-action-idle="" hidden=""`, "the idle label is not hidden behind the committed one")
	has(t, committed, `data-hui-action-done="">Watching</span>`, "the committed label is hidden when it ships committed")

	// One-shot: OptimisticAction commits once and is not a pressed
	// control, so claiming pressed would announce a toggle contract
	// the runtime does not keep.
	hasNot(t, OptimisticAction(OptimisticActionProps{
		Endpoint: "/follow", IdleLabel: "Follow", SuccessLabel: "Following"}, nil),
		"aria-pressed", "a one-shot action claims a pressed state it never manages")
	// The done label is hidden and the idle one is not: SSR is idle.
	has(t, OptimisticAction(OptimisticActionProps{
		Endpoint: "/follow", IdleLabel: "Follow", SuccessLabel: "Following"}, nil),
		`data-hui-action-done="" hidden=""`, "the success label ships unhidden")
}

// The framework's runtime shakes the button on failure and announces
// nothing at all. Ours writes the failure into this span — so the span
// must be a status role (polite by implication, never stated twice),
// and the sentence it will write must already be on the root where
// the listener can read it without reaching into the flip.
func TestAFailedActionHasSomewhereToSayItAndSomethingToSay(t *testing.T) {
	got := OptimisticAction(OptimisticActionProps{
		Endpoint: "/save", IdleLabel: "Save", SuccessLabel: "Saved"}, nil)
	has(t, got, `role="status"`, "a failure has nowhere to be announced")
	hasNot(t, got, "aria-live", "role=status already means polite; stating it twice can announce twice")
	has(t, got, `data-hui-action-status`, "the runtime has no hook to write the announcement into")
	has(t, got, `data-hui-action-failed="Could not save. Try again."`,
		"the default failure sentence is not on the root for the runtime to read")

	custom := ToggleAction(ToggleActionProps{
		Endpoint: "/plan", IdleLabel: "Pro", CommittedLabel: "Pro ✓",
		FailedText: "Could not change plan. Try again."}, nil)
	has(t, custom, `data-hui-action-failed="Could not change plan. Try again."`,
		"the caller's failure sentence did not reach the root")
}

// Everything the lifecycle owns is off limits to ExtraAttrs, because
// the runtime rewrites exactly those attributes as the mutation moves
// and an extra that won one would desynchronise the button from its
// own state machine — type, data-state and aria-busy say things the
// framework's fetch has not earned, and a forged data-fui-* or
// data-hui-* binds behaviour to an element never built for it.
func TestActionExtraAttrsCannotStealTheLifecycle(t *testing.T) {
	hostile := html.Attrs{
		"type":                         "submit",
		"disabled":                     "",
		"data-state":                   "committed",
		"aria-busy":                    "true",
		"aria-pressed":                 "true",
		"data-fui-comp":                "forged",
		"data-fui-optimistic-endpoint": "//evil.example/x",
		"data-hui-action":              "forged",
		"data-hui-toast":               "forged",
		"class":                        "mine",
		"id":                           "stolen",
		"data-testid":                  "keep",
	}
	got := OptimisticAction(OptimisticActionProps{
		Endpoint: "/save", IdleLabel: "Save", SuccessLabel: "Saved",
		ExtraAttrs: hostile}, nil)
	has(t, got, `type="button"`, "an extra turned the action into a submit button")
	has(t, got, `data-state="idle"`, "an extra shipped a state the server did not know")
	hasNot(t, got, `data-state="committed"`, "an extra shipped a state the server did not know")
	hasNot(t, got, "aria-busy", "an extra claimed a request is in flight")
	hasNot(t, got, "//evil.example", "an extra redirected the mutation to another origin")
	hasNot(t, got, "forged", "an extra forged a runtime hook")
	hasNot(t, got, `"stolen"`, "an extra renamed the root")
	hasNot(t, got, "mine", "an extra replaced the class map's class")
	has(t, got, `data-testid="keep"`, "an ordinary attribute was dropped — then the escape hatch is not one")

	toggle := ToggleAction(ToggleActionProps{
		Endpoint: "/watch", IdleLabel: "Watch", CommittedLabel: "Watching",
		ExtraAttrs: hostile}, nil)
	has(t, toggle, `aria-pressed="false"`, "an extra flipped the pressed state the server did not set")
}

// The runtime's origin check drops a cross-origin URL silently, so a
// button wired to one looks fine and does nothing when clicked. That
// is the worst failure a control can have, so the same-origin rule is
// a render-time panic instead.
func TestAnActionRefusesAnEndpointItCanNeverFire(t *testing.T) {
	for _, fn := range []struct {
		name string
		do   func()
	}{
		{"OptimisticAction cross-origin", func() {
			OptimisticAction(OptimisticActionProps{Endpoint: "https://evil.example/f",
				IdleLabel: "a", SuccessLabel: "b"}, nil)
		}},
		{"OptimisticAction empty", func() {
			OptimisticAction(OptimisticActionProps{IdleLabel: "a", SuccessLabel: "b"}, nil)
		}},
		{"ToggleAction protocol-relative", func() {
			ToggleAction(ToggleActionProps{Endpoint: "//evil.example/f",
				IdleLabel: "a", CommittedLabel: "b"}, nil)
		}},
		{"ToggleAction untoggle cross-origin", func() {
			ToggleAction(ToggleActionProps{Endpoint: "/f", UntoggleEndpoint: "https://evil.example/u",
				IdleLabel: "a", CommittedLabel: "b"}, nil)
		}},
		{"method the runtime will not send", func() {
			OptimisticAction(OptimisticActionProps{Endpoint: "/f", Method: "GET",
				IdleLabel: "a", SuccessLabel: "b"}, nil)
		}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s rendered a button that can never fire", fn.name)
				}
			}()
			fn.do()
		}()
	}
}

// The label flip IS the state change: colour, fill and shake are all
// decoration on top of it, and a button whose two labels read the
// same announces nothing when it commits (WCAG 1.4.1). The fixtures
// assert it so a future case cannot quietly ship two identical
// labels and pass every structural check.
func TestTheLabelChangeIsTheSignalNotTheColour(t *testing.T) {
	got := string(OptimisticAction(OptimisticActionProps{
		Endpoint: "/follow", IdleLabel: "Follow", SuccessLabel: "Following"}, nil))
	has(t, render.HTML(got), `data-hui-action-idle="">Follow</span>`,
		"the idle label is not the fixture's own text")
	has(t, render.HTML(got), `data-hui-action-done="" hidden="">Following</span>`,
		"the done label does not differ from the idle one — colour alone would carry the state")
}

// Two labels that read the same leave colour as the only signal, so
// the component refuses them rather than rendering a button that
// announces nothing when it commits.
func TestActionsRefuseIdenticalLabels(t *testing.T) {
	for _, fn := range []struct {
		name string
		do   func()
	}{
		{"OptimisticAction", func() {
			OptimisticAction(OptimisticActionProps{Endpoint: "/f", IdleLabel: "Save", SuccessLabel: "Save"}, nil)
		}},
		{"OptimisticAction, case and space", func() {
			OptimisticAction(OptimisticActionProps{Endpoint: "/f", IdleLabel: "Save", SuccessLabel: " save "}, nil)
		}},
		{"ToggleAction", func() {
			ToggleAction(ToggleActionProps{Endpoint: "/f", IdleLabel: "Watch", CommittedLabel: "Watch"}, nil)
		}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s rendered two identical labels; the flip is the state change", fn.name)
				}
			}()
			fn.do()
		}()
	}
}
