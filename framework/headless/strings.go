package headless

import (
	"context"
	"reflect"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Strings are the words the components say, one field per string; nil or an
// empty field takes the English default. A {{name}} placeholder is filled at
// render when the value is known there and by the runtime otherwise, and a
// translation keeps every placeholder.
type Strings struct {
	DismissTitled  string
	RemoveLabelled string

	ActionFailed string

	PickColor string

	ShowPassword string
	RevealShow   string

	Previous string
	Next     string

	ToneInfo    string
	ToneSuccess string
	ToneWarning string
	ToneDanger  string

	FileSelected  string
	FilesSelected string

	LightboxViewerLabel string
	LightboxPrevious    string
	LightboxNext        string
	LightboxDownload    string

	TableSortBy         string
	TableSelectAll      string
	SelectionCount      string
	SelectionClear      string
	SelectionCopy       string
	SelectionCopied     string
	SelectionCopyFailed string
	TableSortedBy       string
	SortAscending       string
	SortDescending      string

	CounterLabel     string
	CounterDecrement string
	CounterIncrement string

	BackToTop string

	NumberDecrement string
	NumberIncrement string

	RangeLow   string
	RangeHigh  string
	RangeValue string

	RatingChoice string

	TagInputAdd     string
	TagInputAdded   string
	TagInputRemoved string

	RepeaterAdd    string
	RepeaterRemove string

	NotificationCount string

	StepBack   string
	StepNext   string
	StepSubmit string
	StepOf     string
	StepName   string

	TableOfContentsLabel string

	ComboboxLoading      string
	ComboboxNoResults    string
	ComboboxResultCount  string
	ComboboxResultsLabel string

	CarouselSlide     string
	CarouselGoTo      string
	CarouselRole      string
	CarouselSlideRole string

	PaneSecondary string
	PaneTertiary  string

	SidebarOpenDrawer string

	JSONObject      string
	JSONArray       string
	JSONNull        string
	JSONTrue        string
	JSONFalse       string
	JSONEmptyObject string
	JSONEmptyArray  string
	JSONTruncated   string

	ThereIsAProblem string

	BreadcrumbsLabel string

	SortableItemRole          string
	SortableDragLabel         string
	SortableGrabbed           string
	SortablePosition          string
	SortableMoved             string
	SortableSaved             string
	SortableReverted          string
	SortableCancelled         string
	SortableConflictReverted  string
	SortableConflictRefreshed string

	MultiSelectPlaceholder string

	DatePickerPrevMonth   string
	DatePickerNextMonth   string
	DatePickerPrevYear    string
	DatePickerNextYear    string
	DatePickerStart       string
	DatePickerEnd         string
	DatePickerChosen      string
	DatePickerChooseEnd   string
	DatePickerRange       string
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

	ActionFailed: "Could not save. Try again.",

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
