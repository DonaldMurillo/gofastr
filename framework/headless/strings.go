package headless

import (
	"context"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/uiaction"
)

// Strings are the words the components say, one field per string; nil or an
// empty field takes the English default. A {{name}} placeholder is filled at
// render when the value is known there and by the runtime otherwise, and a
// translation keeps every placeholder.
type Strings struct {
	// DismissTitled names a dismiss control whose target follows it; {{title}} after the colon.
	DismissTitled string
	// RemoveLabelled names a chip's remove control; {{label}} is the value's name.
	RemoveLabelled string

	// ActionFailed is a failed mutation's announcement when the caller gave none: a sentence.
	ActionFailed string

	// PickColor names the colour swatch; {{name}} is the field's name.
	PickColor string

	// ShowPassword names the reveal toggle; aria-pressed says the state, so the name never changes.
	ShowPassword string
	// RevealShow is the reveal button's visible word, contained in ShowPassword.
	RevealShow string

	// Previous labels the pager's first end. One word, no arrow.
	Previous string
	// Next labels the pager's last end. One word, no arrow.
	Next string

	// ToneInfo is the tone said before an info title. One word.
	ToneInfo string
	// ToneSuccess is the tone said before a success title. One word.
	ToneSuccess string
	// ToneWarning is the tone said before a warning title. One word.
	ToneWarning string
	// ToneDanger is the tone said before a danger title, the reader's word ("Error"). One word.
	ToneDanger string

	// FileSelected says one file was chosen; the runtime fills {{name}}.
	FileSelected string
	// FilesSelected says several files were chosen; the runtime fills {{n}} and {{names}}.
	FilesSelected string

	// LightboxViewerLabel is the open viewer's accessible name. A short noun phrase.
	LightboxViewerLabel string
	// LightboxPrevious names the viewer's previous button ("Previous image").
	LightboxPrevious string
	// LightboxNext names the viewer's next button ("Next image").
	LightboxNext string
	// LightboxDownload names the anchor that saves the viewed image.
	LightboxDownload string

	// TableSortBy names a sort control on a column with no header text; {{column}} is its Key, filled at render.
	TableSortBy string
	// TableSelectAll names the select-all checkbox of a column with no Header.
	TableSelectAll string
	// SelectionCount says how many rows a selection holds; the behaviour fills {{n}}.
	SelectionCount string
	// SelectionClear names the button that clears a selection.
	SelectionClear string
	// SelectionCopy names the button that copies a selection as CSV.
	SelectionCopy string
	// SelectionCopied is the toast after a copy; {{n}} is the row count.
	SelectionCopied string
	// SelectionCopyFailed is the toast when the rows could not be copied.
	SelectionCopyFailed string
	// TableSortedBy announces a new sort; {{column}} and {{direction}} are filled at render.
	TableSortedBy string
	// SortAscending is the ascending direction word inside TableSortedBy. Lowercase.
	SortAscending string
	// SortDescending is the descending direction word inside TableSortedBy. Lowercase.
	SortDescending string

	// CounterLabel names the counter group. One noun.
	CounterLabel string
	// CounterDecrement names the counter's decrement button. One word.
	CounterDecrement string
	// CounterIncrement names the counter's increment button. One word.
	CounterIncrement string

	// BackToTop names the back-to-top anchor. A short imperative.
	BackToTop string

	// NumberDecrement names a stepper's decrement button; {{label}} is the field's name.
	NumberDecrement string
	// NumberIncrement names a stepper's increment button; {{label}} is the field's name.
	NumberIncrement string

	// RangeLow names a range pair's low thumb; {{label}} is the thing bounded.
	RangeLow string
	// RangeHigh names a range pair's high thumb; {{label}} is the thing bounded.
	RangeHigh string
	// RangeValue is a range pair's output sentence, "{{low}} to {{high}}".
	RangeValue string

	// RatingChoice names one rating radio, "{{n}} out of {{max}}".
	RatingChoice string

	// TagInputAdd names the control that commits a draft tag; {{label}} is the field's label.
	TagInputAdd string
	// TagInputAdded is said after a chip is added; the runtime fills {{name}}.
	TagInputAdded string
	// TagInputRemoved is said after a chip is removed; the runtime fills {{name}}.
	TagInputRemoved string

	// RepeaterAdd names the repeater's add control.
	RepeaterAdd string
	// RepeaterRemove names one remove control; {{n}} is the item's 1-based position.
	RepeaterRemove string

	// NotificationCount is the bell's accessible name, "{{n}} unread notifications".
	NotificationCount string

	// StepBack is the wizard's back control.
	StepBack string
	// StepNext is the wizard's next control.
	StepNext string
	// StepSubmit is the wizard's last-step control.
	StepSubmit string
	// StepOf names the wizard rail, "Step {{step}} of {{total}}".
	StepOf string
	// StepName names one rail dot, "Step {{step}}: {{heading}}".
	StepName string

	// TableOfContentsLabel names the contents landmark. A short phrase naming what it lists.
	TableOfContentsLabel string

	// ComboboxLoading is the status while results load. A short phrase.
	ComboboxLoading string
	// ComboboxNoResults is the status when a query matched nothing.
	ComboboxNoResults string
	// ComboboxResultCount announces a result count; the module fills {{n}}.
	ComboboxResultCount string
	// ComboboxResultsLabel is the noun in the listbox name "<Label> results".
	ComboboxResultsLabel string

	// CarouselSlide names a slide, "Slide {{n}} of {{total}}", filled at render.
	CarouselSlide string
	// CarouselGoTo names a dot control, "Go to slide {{n}}".
	CarouselGoTo string
	// CarouselRole is the carousel region's roledescription. One word.
	CarouselRole string
	// CarouselSlideRole is each slide's roledescription. One word.
	CarouselSlideRole string
	// CarouselPause names the rotation control of a rotating carousel; aria-pressed says it is paused.
	CarouselPause string

	// PaneSecondary names a secondary pane with no label of its own. One word.
	PaneSecondary string
	// PaneTertiary names a tertiary pane with no label of its own. One word.
	PaneTertiary string

	// SidebarOpenDrawer names the phone drawer's button when DrawerLabel is empty.
	SidebarOpenDrawer string

	// JSONObject names an object node. One word.
	JSONObject string
	// JSONArray names an array node. One word.
	JSONArray string
	// JSONNull is the null literal.
	JSONNull string
	// JSONTrue is the true literal.
	JSONTrue string
	// JSONFalse is the false literal.
	JSONFalse string
	// JSONEmptyObject is what an empty object renders. One token.
	JSONEmptyObject string
	// JSONEmptyArray is what an empty array renders. One token.
	JSONEmptyArray string
	// JSONTruncated is the mark a truncated string ends with.
	JSONTruncated string

	// ThereIsAProblem heads the list a failed submit focuses. A short sentence.
	ThereIsAProblem string

	// BreadcrumbsLabel names the trail's navigation landmark. One word.
	BreadcrumbsLabel string

	// SortableItemRole is every row's roledescription. Two words.
	SortableItemRole string
	// SortableDragLabel names a row by what it offers; {{label}} is filled at render.
	SortableDragLabel string
	// SortableGrabbed is said when a row is picked up; the runtime fills {{label}}.
	SortableGrabbed string
	// SortablePosition says where the grabbed row is; the runtime fills {{position}} and {{list}}.
	SortablePosition string
	// SortableMoved says the row changed lists; the runtime fills {{list}} and {{position}}.
	SortableMoved string
	// SortableSaved confirms a commit the server accepted.
	SortableSaved string
	// SortableReverted says a failed commit put the rows back.
	SortableReverted string
	// SortableCancelled says Esc put the grabbed row back, uncommitted.
	SortableCancelled string
	// SortableConflictReverted says a 409 was resolved by putting the rows back.
	SortableConflictReverted string
	// SortableConflictRefreshed says a 409 was resolved by taking the server's rows.
	SortableConflictRefreshed string

	// MultiSelectPlaceholder is what the chips strip says when nothing is picked.
	MultiSelectPlaceholder string

	// DatePickerPrevMonth names the previous-month anchor. A short phrase.
	DatePickerPrevMonth string
	// DatePickerNextMonth names the next-month anchor. A short phrase.
	DatePickerNextMonth string
	// DatePickerPrevYear names the previous-year anchor. A short phrase.
	DatePickerPrevYear string
	// DatePickerNextYear names the next-year anchor. A short phrase.
	DatePickerNextYear string
	// DatePickerStart labels a range's start input when the caller names none.
	DatePickerStart string
	// DatePickerEnd labels a range's end input when the caller names none.
	DatePickerEnd string
	// DatePickerChosen is said when a day is picked; the runtime fills {{date}}.
	DatePickerChosen string
	// DatePickerChooseEnd is said when a range start is picked; the runtime fills {{date}}.
	DatePickerChooseEnd string
	// DatePickerRange is said when a range end is picked; the runtime fills {{start}} and {{end}}.
	DatePickerRange string
	// DatePickerMonthFailed is said when the month could not be loaded.
	DatePickerMonthFailed string

	ctx context.Context
}

// ForRequest returns a copy of w whose numbers follow ctx's locale; nil w
// yields the English words.
func (w *Strings) ForRequest(ctx context.Context) *Strings {
	out := w.Resolve()
	if out == stringsProbe {
		c := *out
		out = &c
	}
	out.ctx = ctx
	return out
}

// Number writes n in the reader's locale, or English without a request.
func (w *Strings) Number(n int) string {
	var ctx context.Context
	if w != nil {
		ctx = w.ctx
	}
	return i18n.Number(ctx, n)
}

var defaultStrings = Strings{
	DismissTitled:  "Dismiss: {{title}}",
	RemoveLabelled: "Remove {{label}}",

	ActionFailed: uiaction.DefaultFailedText,

	PickColor: "Pick {{name}}",

	ShowPassword: "Show password", // not-a-secret: the reveal button's accessible name
	RevealShow:   "Show",

	Previous: "Previous",
	Next:     "Next",

	ToneInfo:      "Information",
	ToneSuccess:   "Success",
	ToneWarning:   "Warning",
	ToneDanger:    "Error",
	FileSelected:  "{{name}} selected.",
	FilesSelected: "{{n}} files selected: {{names}}.",

	LightboxViewerLabel: "Image viewer",
	LightboxPrevious:    "Previous image",
	LightboxNext:        "Next image",
	LightboxDownload:    "Download image",
	TableSortBy:         "Sort by {{column}}",
	TableSelectAll:      "Select all rows",
	SelectionCount:      "{{n}} selected",
	SelectionClear:      "Clear selection",
	SelectionCopy:       "Copy CSV",
	SelectionCopied:     "Copied {{n}} rows as CSV",
	SelectionCopyFailed: "The rows could not be copied.",
	TableSortedBy:       "Sorted by {{column}}, {{direction}}",
	SortAscending:       "ascending",
	SortDescending:      "descending",

	CounterLabel:     "Counter",
	CounterDecrement: "Decrement",
	CounterIncrement: "Increment",

	BackToTop: "Back to top",

	NumberDecrement: "Decrement {{label}}",
	NumberIncrement: "Increment {{label}}",

	RangeLow:   "Minimum {{label}}",
	RangeHigh:  "Maximum {{label}}",
	RangeValue: "{{low}} to {{high}}",

	RatingChoice: "{{n}} out of {{max}}",

	TagInputAdd:     "Add {{label}}",
	TagInputAdded:   "{{name}} added",
	TagInputRemoved: "{{name}} removed",

	RepeaterAdd:    "Add item",
	RepeaterRemove: "Remove item {{n}}",

	NotificationCount: "{{n}} unread notifications",

	StepBack:   "Back",
	StepNext:   "Continue",
	StepSubmit: "Submit",
	StepOf:     "Step {{step}} of {{total}}",
	StepName:   "Step {{step}}: {{heading}}",

	TableOfContentsLabel: "On this page",

	CarouselSlide: "Slide {{n}} of {{total}}",
	CarouselGoTo:  "Go to slide {{n}}",

	CarouselRole:      "carousel",
	CarouselSlideRole: "slide",
	CarouselPause:     "Pause slides",

	PaneSecondary: "Secondary",
	PaneTertiary:  "Tertiary",

	SidebarOpenDrawer: "Open navigation",

	ComboboxLoading:      "Loading…",
	ComboboxNoResults:    "No matches",
	ComboboxResultCount:  "{{n}} results",
	ComboboxResultsLabel: "results",

	ThereIsAProblem: "There is a problem",

	BreadcrumbsLabel: "Breadcrumb",

	SortableItemRole:          "sortable item",
	SortableDragLabel:         "Drag {{label}}",
	SortableGrabbed:           "Grabbed {{label}}. Arrow keys to move, Space to drop.",
	SortablePosition:          "Position {{position}} in {{list}}.",
	SortableMoved:             "Moved to {{list}}, position {{position}}.",
	SortableSaved:             "Order saved.",
	SortableReverted:          "Save failed. Reverted.",
	SortableCancelled:         "Cancelled.",
	SortableConflictReverted:  "Conflict. Reverted.",
	SortableConflictRefreshed: "Conflict. List refreshed from server.",

	MultiSelectPlaceholder: "Choose…",
	JSONObject:             "Object",
	JSONArray:              "Array",
	JSONNull:               "null",
	JSONTrue:               "true",
	JSONFalse:              "false",
	JSONEmptyObject:        "{}",
	JSONEmptyArray:         "[]",
	JSONTruncated:          "…",

	DatePickerPrevMonth: "Previous month",
	DatePickerNextMonth: "Next month",
	DatePickerPrevYear:  "Previous year",
	DatePickerNextYear:  "Next year",
	DatePickerStart:     "Start date",
	DatePickerEnd:       "End date",
	DatePickerChosen:    "{{date}} selected.",
	DatePickerChooseEnd: "{{date}} selected. Choose the end date.",
	DatePickerRange:     "{{start}} to {{end}} selected.",

	DatePickerMonthFailed: "The month could not be loaded.",
}

// DefaultStrings returns a fresh copy of the English defaults.
func DefaultStrings() *Strings {
	w := defaultStrings
	return &w
}

func (w *Strings) withDefaults() *Strings {
	out := DefaultStrings()
	out.ctx = w.ctx
	src := reflect.ValueOf(w).Elem()
	dst := reflect.ValueOf(out).Elem()
	for i := 0; i < src.NumField(); i++ {
		if !src.Type().Field(i).IsExported() {
			continue
		}
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

// ProbeStrings returns a Strings whose every field is "<FieldName>" plus the
// default's placeholders, so a render shows which words came through it.
func ProbeStrings() *Strings {
	w := DefaultStrings()
	v := reflect.ValueOf(w).Elem()
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		if !t.Field(i).IsExported() {
			continue
		}
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

func placeholdersIn(s string) []string {
	var out []string
	for {
		open := strings.Index(s, "{{")
		if open < 0 {
			return out
		}
		end := strings.Index(s[open:], "}}")
		if end < 0 {
			return out
		}
		out = append(out, s[open:open+end+2])
		s = s[open+end+2:]
	}
}

func (w *Strings) fill(tpl string, kv ...any) string {
	params := make(map[string]any, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		if name, ok := kv[i].(string); ok {
			params[name] = kv[i+1]
		}
	}
	var ctx context.Context
	if w != nil {
		ctx = w.ctx
	}
	return i18n.FillContext(ctx, tpl, params)
}

var stringsProbe *Strings

// Resolve returns s with every empty field defaulted, the harness probe when
// s is nil and a probe is set, and the English defaults otherwise.
func (s *Strings) Resolve() *Strings {
	if s != nil {
		return s.withDefaults()
	}
	if stringsProbe != nil {
		return stringsProbe
	}
	return DefaultStrings()
}
