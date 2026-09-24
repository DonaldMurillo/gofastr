package headless

// Strings: every string a component says, in one typed table.
//
// The headless layer is the accessibility half of the system, and a
// surprising amount of what it guarantees is wording: the tone said
// before an alert's title, the name of the button that reveals a
// password, "Previous" on a pager's disabled end. A
// French reader needs those guarantees in French, and a key map
// ("dismiss.label" → "Fermer") would put the finding of a missing
// string on a rendered page. So the words are a typed struct: one
// field per string, a doc comment saying its shape, and a field left
// empty falling back to its English default at RUNTIME — a partial
// translation is safe, and the miss is a stray English word on a
// French page, not a compile error. The probe golden
// (spec_golden_strings.txt) is what catches a component saying a
// word no field carries. The framework's own translated strings live
// in framework/i18nui as keys; framework/ui's StringsFor(ctx) is the
// layer above that resolves each field from those keys once per
// request.
//
// The field is Strings on every component's props: nil means the
// English defaults below, which is what the goldens pin.
// Nothing here fetches, caches or guesses a language.
//
// Plurals are two fields, One and Many, chosen by the component and
// never built by appending "s". Formats keep their verbs: a
// translation may reorder the words but must keep the placeholders,
// which the gates in words_test.go hold.

import (
	"reflect"
	"strings"
)

// Strings are the strings the components say. Every field defaults to
// the English the components rendered before this type existed, so a
// nil or partially-set value is safe; DefaultWords returns them all
// and ProbeWords returns one probe token per field.
//
// Fields holding a %s or %d are format strings, applied with
// fmt.Sprintf at the site that owns the numbers. {n} is substituted
// by the runtime, not the server: those strings travel as data-*
// attributes and the count is written in when it is known.
type Strings struct {
	// ─── said by more than one component ───────────────────────────
	//
	// The same English word doing the same job in several components
	// is one field, so one translation serves every speaker of it.

	// DismissTitled names a dismiss control whose target follows it:
	// an Alert's, a SystemBanner's. The title follows the colon and a
	// space; a format taking it as %s.
	DismissTitled string
	// RemoveLabelled names a remove control by what it removes: a
	// Tag's chip. A format taking the value's name as %s.
	RemoveLabelled string

	// ─── OptimisticAction / ToggleAction ───────────────────────────

	// ActionFailed is what a failed mutation announces when the caller
	// supplied nothing better. A sentence, not a word: it says the
	// save did not happen and that trying again is allowed.
	ActionFailed string

	// ─── Color ──────────────────────────────────────────────────────

	// PickColor names the colour swatch, which carries no visible
	// label. A format taking the field's name as %s.
	PickColor string

	// ─── Password ───────────────────────────────────────────────────

	// ShowPassword and HidePassword name the reveal button in each of
	// its two states. Imperative, not rendered visually; they also
	// travel as data so the runtime can swap them.
	ShowPassword string
	HidePassword string
	// RevealShow is the reveal button's visible text, and RevealHide
	// is what the runtime swaps it to. One word each; RevealHide is
	// never rendered by the server.
	RevealShow string
	RevealHide string

	// ─── Pagination ─────────────────────────────────────────────────

	// Previous and Next label the pager's ends, which are disabled
	// anchors rather than absent ones. One word each; they carry no
	// arrows, so a translation may add its own.
	Previous string
	Next     string

	// ─── SystemBanner, Alert ────────────────────────────────────────

	// ToneInfo, ToneSuccess, ToneWarning and ToneDanger are the tone
	// said before a title — a SystemBanner's, and an Alert's when the
	// caller passes one as ToneWord — because the title says WHAT
	// happened and the
	// tone is the only thing saying how serious it is. One word each,
	// the one a reader understands rather than the stylesheet's name
	// for the colour ("Error", not "Danger"); the colon and space that
	// follow are added in the assembly.
	ToneInfo    string
	ToneSuccess string
	ToneWarning string
	ToneDanger  string

	// ─── Upload ──────────────────────────────────────────────────────

	// FileSelected says one file was chosen, with {name} where the
	// file's name goes. The runtime writes the name in when it knows
	// it; the server never renders this string with a value in it,
	// because before the reader picks there is no value to say.
	FileSelected string
	// FilesSelected says several files were chosen, with {n} for the
	// count and {names} for the joined list. Substituted by the runtime
	// like FileSelected's {name}, for the same reason: neither the
	// count nor the names exist until the reader has picked.
	FilesSelected string

	// ─── Lightbox viewer ────────────────────────────────────────────

	// LightboxViewerLabel is the accessible name of the open image
	// viewer: what a screen reader says when the viewer takes focus.
	// A short noun phrase; the image's own alt travels beside it.
	LightboxViewerLabel string
	// LightboxPrevious and LightboxNext name the viewer's nav buttons.
	// They name the action on this surface ("Previous image"), not the
	// pager's bare word: a reader inside a viewer needs to know what
	// stepping moves.
	LightboxPrevious string
	LightboxNext     string
	// LightboxDownload names the anchor that saves the image being
	// viewed.
	LightboxDownload string

	// ─── Table ───────────────────────────────────────────────────────

	// TableSortBy names a sort control whose column carries no
	// visible header text: an actions or icon column that can still
	// be sorted. A format taking the column's Key as {column}; the
	// token is written in at render, when the anchor is built —
	// unlike the {n} the runtime substitutes, the key is known on
	// the server.
	TableSortBy string
	// TableSortedBy says what a changed table is now sorted by, with
	// {column} where the column's header goes and {direction} where
	// the direction word goes. Rendered into data-hui-table-announcement
	// at render and copied into the status after a swap, so a
	// translated page announces in its own language.
	TableSortedBy string
	// SortAscending and SortDescending are the direction words
	// TableSortedBy carries. One lowercase word each: they sit inside
	// a sentence, after a comma.
	SortAscending  string
	SortDescending string

	// ─── Counter ─────────────────────────────────────────────────────

	// CounterLabel names the counter group, which carries no visible
	// label of its own. One noun.
	CounterLabel string
	// CounterDecrement and CounterIncrement name the counter's two
	// buttons. One word each; the counter's value is the context.
	CounterDecrement string
	CounterIncrement string

	// ─── BackToTop ───────────────────────────────────────────────────

	// BackToTop names the back-to-top anchor, whose glyph carries no
	// words. A short imperative.
	BackToTop string

	// ─── NumberInput ─────────────────────────────────────────────────

	// NumberDecrement and NumberIncrement name the stepper's two
	// buttons, which need the field's name to tell them from any
	// other stepper's. Formats taking the label as %s.
	NumberDecrement string
	NumberIncrement string

	// ─── Slider and RangeSlider ──────────────────────────────────────

	// RangeLow and RangeHigh name the two thumbs of a range pair —
	// "Minimum %s" and "Maximum %s", the label standing in for the
	// thing bounded. RangeValue is the pair's one output sentence,
	// "%s to %s", low first.
	RangeLow   string
	RangeHigh  string
	RangeValue string

	// ─── Rating ──────────────────────────────────────────────────────

	// RatingChoice names one rating radio: "%d out of %d", the chosen
	// value and the ceiling.
	RatingChoice string

	// ─── TagInput ────────────────────────────────────────────────────

	// TagInputAdd names the control that commits the draft, a format
	// taking the field's label as %s. TagInputAdded and
	// TagInputRemoved are what the status region says after a chip
	// operation, with {name} where the chip's text goes — the runtime
	// writes it when the operation has happened.
	TagInputAdd     string
	TagInputAdded   string
	TagInputRemoved string

	// ─── Repeater ────────────────────────────────────────────────────

	// RepeaterAdd names the add control. RepeaterRemove names one
	// remove control, a format taking the item's 1-based position as
	// %d, because twelve controls all called Remove tell a screen
	// reader user nothing.
	RepeaterAdd    string
	RepeaterRemove string

	// ─── NotificationBell ────────────────────────────────────────────

	// NotificationCount is the bell's accessible name, "%d unread
	// notifications" — the count the anchor carries, said in words.
	NotificationCount string

	// ─── StepWizard ──────────────────────────────────────────────────

	// StepBack, StepNext and StepSubmit are the wizard's three
	// controls; Next is the one weighted action until the last step
	// makes it Submit. StepOf is the rail's name, "Step %d of %d",
	// and StepName is one dot's, "Step %d: %s" with the step's
	// heading as %s.
	StepBack   string
	StepNext   string
	StepSubmit string
	StepOf     string
	StepName   string

	// ─── TableOfContents ─────────────────────────────────────────────

	// TableOfContentsLabel names the contents navigation, which is a
	// landmark a screen reader jumps to by name. A short prepositional
	// phrase naming what the list is of.
	TableOfContentsLabel string

	// ─── Combobox ─────────────────────────────────────────────────────

	// ComboboxLoading is what the status region says while results
	// are being fetched. One word or a short phrase.
	ComboboxLoading string
	// ComboboxNoResults is what the status region says when a query
	// matched nothing.
	ComboboxNoResults string
	// ComboboxResultCount announces how many results a query found,
	// "{n}" where the count goes — the module writes the count in when
	// the results arrive, so the sentence travels as an attribute and
	// the placeholder is a name, not a fmt verb.
	ComboboxResultCount string
	// ComboboxResultsLabel names the listbox: "<Label> results" is
	// assembled at render, so the word here is the noun.
	ComboboxResultsLabel string

	// ─── Carousel ─────────────────────────────────────────────────────

	// CarouselSlide names one slide and the total, "Slide {n} of
	// {total}" — {n} and {total} are filled at render (the module
	// re-says it into the status after each step).
	CarouselSlide string
	// CarouselGoTo names a dot control, "Go to slide {n}".
	CarouselGoTo string

	// ─── JSONTree ──────────────────────────────────────────────────────

	// JSONObject names an object node in a JSON tree. One word.
	JSONObject string
	// JSONArray names an array node in a JSON tree. One word.
	JSONArray string
	// JSONNull is the null literal. One word.
	JSONNull string
	// JSONTrue / JSONFalse are the boolean literals. One word each.
	JSONTrue  string
	JSONFalse string
	// JSONEmptyObject / JSONEmptyArray are what an empty collection
	// renders: the literal braces/brackets. One token each.
	JSONEmptyObject string
	JSONEmptyArray  string
	// JSONTruncated is the mark a truncated string ends with.
	JSONTruncated string

	// ─── ValidationSummary ──────────────────────────────────────────

	// ThereIsAProblem heads the list a failed submit focuses. A short
	// sentence; it names the fact, the list names each field.
	ThereIsAProblem string

	// ─── Breadcrumbs ────────────────────────────────────────────────

	// BreadcrumbsLabel names the trail's navigation landmark, which a
	// screen reader jumps to by name. One word.
	BreadcrumbsLabel string

	// ─── SortableList ───────────────────────────────────────────────

	// SortableItemRole is the roledescription every row carries: it
	// tells a screen reader what kind of thing the row is before any
	// key is pressed. Two words.
	SortableItemRole string
	// SortableDragLabel names one row by what it offers: a format
	// taking the row's visible label as %s, applied at render.
	SortableDragLabel string
	// SortableGrabbed is said when a row is picked up, with {label}
	// where the row's name goes. Substituted by the runtime when the
	// grab happens, so the sentence travels as an attribute.
	SortableGrabbed string
	// SortablePosition says where the grabbed row now is, with
	// {position} and {list} (the list's own name) written in by the
	// runtime after each move.
	SortablePosition string
	// SortableMoved says the row changed lists, with {list} and
	// {position}: the kanban crossing, announced after it lands.
	SortableMoved string
	// SortableSaved confirms a commit the server accepted.
	SortableSaved string
	// SortableReverted says a failed commit put the rows back.
	SortableReverted string
	// SortableCancelled says Esc put the grabbed row back where it
	// started, uncommitted.
	SortableCancelled string
	// SortableConflictReverted says a 409 was reconciled by putting
	// the rows back.
	SortableConflictReverted string
	// SortableConflictRefreshed says a 409 was reconciled by
	// replacing the list with the server's own rows.
	SortableConflictRefreshed string

	// ─── MultiSelect ────────────────────────────────────────────────

	// MultiSelectPlaceholder is what the chips strip says when
	// nothing is picked. A short phrase; the chips replace it the
	// moment one is.
	MultiSelectPlaceholder string
	// MultiSelectRemoveLabel names a chip's remove control, with
	// {label} where the picked option's name goes — the runtime
	// writes it in when it builds the chip.
	MultiSelectRemoveLabel string
}

// defaultStrings is the English the components rendered before Strings
// existed. The goldens pin these bytes.
var defaultStrings = Strings{
	DismissTitled:  "Dismiss: %s",
	RemoveLabelled: "Remove %s",

	ActionFailed: "Could not save. Try again.",

	PickColor: "Pick %s",

	ShowPassword: "Show password", // not-a-secret: the reveal button's accessible name
	HidePassword: "Hide password", // not-a-secret: the reveal button's accessible name
	RevealShow:   "Show",
	RevealHide:   "Hide",

	Previous: "Previous",
	Next:     "Next",

	ToneInfo:      "Information",
	ToneSuccess:   "Success",
	ToneWarning:   "Warning",
	ToneDanger:    "Error",
	FileSelected:  "{name} selected.",
	FilesSelected: "{n} files selected: {names}.",

	LightboxViewerLabel: "Image viewer",
	LightboxPrevious:    "Previous image",
	LightboxNext:        "Next image",
	LightboxDownload:    "Download image",
	// The i18nui catalog's own English for ui.table.sortBy; the ui
	// bridge's gate holds the two to the same bytes.
	TableSortBy: "Sort by {column}",
	// The i18nui catalog's own English for ui.table.sortedBy and the
	// two direction words; the ui bridge's gate holds the two to the
	// same bytes.
	TableSortedBy:  "Sorted by {column}, {direction}",
	SortAscending:  "ascending",
	SortDescending: "descending",

	CounterLabel:     "Counter",
	CounterDecrement: "Decrement",
	CounterIncrement: "Increment",

	BackToTop: "Back to top",

	NumberDecrement: "Decrement %s",
	NumberIncrement: "Increment %s",

	RangeLow:   "Minimum %s",
	RangeHigh:  "Maximum %s",
	RangeValue: "%s to %s",

	RatingChoice: "%d out of %d",

	TagInputAdd:     "Add %s",
	TagInputAdded:   "{name} added",
	TagInputRemoved: "{name} removed",

	RepeaterAdd:    "Add item",
	RepeaterRemove: "Remove item %d",

	NotificationCount: "%d unread notifications",

	StepBack:   "Back",
	StepNext:   "Continue",
	StepSubmit: "Submit",
	StepOf:     "Step %d of %d",
	StepName:   "Step %d: %s",

	TableOfContentsLabel: "On this page",

	CarouselSlide: "Slide {n} of {total}",
	CarouselGoTo:  "Go to slide {n}",

	ComboboxLoading:      "Loading…",
	ComboboxNoResults:    "No matches",
	ComboboxResultCount:  "{n} results",
	ComboboxResultsLabel: "results",

	ThereIsAProblem: "There is a problem",

	BreadcrumbsLabel: "Breadcrumb",

	SortableItemRole:          "sortable item",
	SortableDragLabel:         "Drag %s",
	SortableGrabbed:           "Grabbed {label}. Arrow keys to move, Space to drop.",
	SortablePosition:          "Position {position} in {list}.",
	SortableMoved:             "Moved to {list}, position {position}.",
	SortableSaved:             "Order saved.",
	SortableReverted:          "Save failed. Reverted.",
	SortableCancelled:         "Cancelled.",
	SortableConflictReverted:  "Conflict. Reverted.",
	SortableConflictRefreshed: "Conflict. List refreshed from server.",

	MultiSelectPlaceholder: "Choose…",
	MultiSelectRemoveLabel: "Remove {label}",
	JSONObject:             "Object",
	JSONArray:              "Array",
	JSONNull:               "null",
	JSONTrue:               "true",
	JSONFalse:              "false",
	JSONEmptyObject:        "{}",
	JSONEmptyArray:         "[]",
	JSONTruncated:          "…",
}

// DefaultStrings returns a fresh copy of the English defaults, every
// field set. Fresh so a caller cannot mutate the package's copy
// through it.
func DefaultStrings() *Strings {
	w := defaultStrings
	return &w
}

// withDefaults returns w with every empty field filled from the
// English defaults; w itself is left alone.
func (w *Strings) withDefaults() *Strings {
	out := DefaultStrings()
	src := reflect.ValueOf(w).Elem()
	dst := reflect.ValueOf(out).Elem()
	for i := 0; i < src.NumField(); i++ {
		v := src.Field(i).String()
		if v == "" {
			continue
		}
		f := dst.Field(i)
		if !f.CanSet() {
			panic("headless: Strings." + src.Type().Field(i).Name + " is not settable — every field of Strings must be an exported string")
		}
		f.SetString(v)
	}
	return out
}

// ProbeStrings returns a Strings whose every field is its own name in
// angle brackets — formats as the name plus their placeholders, so
// `<RemoveLabelled env=prod>` renders where "Remove env=prod" would. A render against it shows exactly which words came
// through the table; a real English word in one is a word that
// bypassed it.
func ProbeStrings() *Strings {
	w := DefaultStrings()
	v := reflect.ValueOf(w).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		def := v.Field(i).String()
		probe := "<" + t.Field(i).Name
		if ph := placeholdersIn(def); len(ph) > 0 {
			probe += " " + strings.Join(ph, " ")
		}
		f := v.Field(i)
		if !f.CanSet() {
			panic("headless: Strings." + t.Field(i).Name + " is not settable — every field of Strings must be an exported string")
		}
		f.SetString(probe + ">")
	}
	return w
}

// placeholdersIn lists a default's placeholders in order: % verbs and
// {name} tokens, skipping the escaped %%. ProbeWords keeps them so a
// probe render still applies its arguments, and the validation test
// compares them so a translation cannot drop one.
func placeholdersIn(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '%':
			if i+1 < len(s) {
				if s[i+1] == '%' {
					i++
					continue
				}
				out = append(out, s[i:i+2])
				i++
			}
		case '{':
			if end := strings.IndexByte(s[i:], '}'); end > 0 {
				out = append(out, s[i:i+end+1])
				i += end
			}
		}
	}
	return out
}

// stringsProbe, when set, is what every component says instead of the
// English defaults. Only the harness sets it, to render the whole
// corpus through Strings without editing every fixture; it is
// nil in production and Resolve behaves as if it did not exist.
var stringsProbe *Strings

// Resolve is how a component reaches its strings: the caller's when a
// layer above resolved them from the request, the harness's probe
// when a test is looking, and the English defaults otherwise. A
// caller's Strings may be partial: every empty field falls back to
// its English default, so a Strings that sets one string does not
// silently unname the reveal button. Safe on a nil receiver, which is
// what an unset prop is.
func (s *Strings) Resolve() *Strings {
	if s != nil {
		return s.withDefaults()
	}
	if stringsProbe != nil {
		return stringsProbe
	}
	return DefaultStrings()
}
