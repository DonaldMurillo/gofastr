package ui

import (
	"context"
	"net/url"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── DatePicker ────────────────────────────────────────────────────
//
// A calendar that stays on the page beside the native date input it
// fills (headless.DatePicker). It is for what the browser's own picker
// cannot do: a range in one control, specific days refused (booked,
// closed), or a note written on each day ("3 left"). An ordinary date
// is DateField, and stays DateField: the native picker is accessible,
// local and needs no script.
//
// The native input is the value, so the form posts the HTML date
// format with or without script. A month change is an island: the
// page serves Endpoint, which answers DatePickerMonth for the month in
// the query, built from the same config.

// DatePickerDay is what the server knows about one day.
type DatePickerDay struct {
	// Disabled refuses the day. It stays in the grid, announced as
	// unavailable.
	Disabled bool
	// Note is a short line written on the day: "3 left", "Full".
	Note string
}

// DatePickerRange makes a DatePicker pick a start and an end.
type DatePickerRange struct {
	// StartName and EndName are the two inputs' form names. Required.
	StartName, EndName string
	// Start and End are the current values, YYYY-MM-DD.
	Start, End string
	// StartLabel and EndLabel name the inputs; empty says "Start date"
	// and "End date" in the reader's language.
	StartLabel, EndLabel string
}

// DatePickerConfig configures a DatePicker and the DatePickerMonth its
// endpoint answers. Build both from one function of the request, so
// the page and the endpoint agree on the days.
type DatePickerConfig struct {
	// ID is the input's id (a range's are ID-start and ID-end). Label
	// names the field. Required, with Endpoint and Name (or Range).
	ID, Label string
	// Name and Value are a single date's form name and value,
	// YYYY-MM-DD. Refused with Range.
	Name, Value string
	Range       *DatePickerRange

	// Endpoint is the same-origin path a month change GETs, with the
	// month in its query (MonthParam): it answers DatePickerMonth.
	Endpoint string
	// Month is the month shown, YYYY-MM: the endpoint passes the
	// query's. Empty, or not a month, shows the value's month, else
	// Today's.
	Month string
	// MonthParam names the month query parameter. Default "month".
	MonthParam string
	// Path and Query are the page's own path and query: a nav anchor's
	// href without script is Path plus Query with the month set.
	// Empty Path is the current page.
	Path  string
	Query url.Values

	// Today is the reader's today, YYYY-MM-DD, in the reader's zone,
	// which the server knows and this package does not.
	Today string
	// Min and Max bound the dates, YYYY-MM-DD.
	Min, Max string
	// Days is what the server knows about each day, keyed YYYY-MM-DD.
	Days map[string]DatePickerDay
	// WeekStart is the first column. Nil takes the reader's locale.
	WeekStart *time.Weekday

	// Help and Error describe the field, as on every form field.
	Help, Error string
	Required    bool
	Disabled    bool
	Class       string
	// ExtraAttrs forwards attributes to the picker's root; keys the
	// component owns are dropped.
	ExtraAttrs html.Attrs
	Ctx        context.Context
}

// datePickerClasses dresses headless.DatePicker. The native inputs
// wear the field sheet's fui-input and fui-field__label, so a range's
// two inputs look like every other field's.
var datePickerClasses = headless.Classes{
	headless.PartRoot:               "fui-datepicker",
	headless.PartLabel:              "fui-field__label",
	headless.PartDatePickerInputs:   "fui-datepicker__inputs",
	headless.PartDatePickerInput:    "fui-input",
	headless.PartDatePickerCalendar: "fui-datepicker__calendar",
	headless.PartDatePickerHeader:   "fui-datepicker__header",
	headless.PartDatePickerNav:      "fui-datepicker__nav",
	headless.PartDatePickerCaption:  "fui-datepicker__caption",
	headless.PartDatePickerGrid:     "fui-datepicker__grid",
	headless.PartDatePickerWeekday:  "fui-datepicker__weekday",
	headless.PartDatePickerCell:     "fui-datepicker__cell",
	headless.PartDatePickerDay:      "fui-datepicker__day",
	headless.PartDatePickerNote:     "fui-datepicker__note",
	headless.PartDatePickerStatus:   "fui-visually-hidden",
}

// datePickerGroupClasses dresses a range's Fieldset: its legend reads
// as a field's label, its description and error as a field's.
var datePickerGroupClasses = headless.Classes{
	headless.PartRoot:       "fui-datepicker-group",
	headless.PartLegend:     "fui-field__label",
	headless.PartGroupDesc:  "fui-field__hint",
	headless.PartGroupError: "fui-field__error",
	headless.PartFields:     "fui-datepicker-group__fields",
}

func (cfg DatePickerConfig) props(c *headless.FieldControl) headless.DatePickerProps {
	if cfg.Endpoint == "" {
		panic("ui: DatePicker requires Endpoint — a month change is an island the page serves")
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	p := headless.DatePickerProps{
		ID: cfg.ID, Label: cfg.Label, Name: cfg.Name, Value: cfg.Value,
		Month: cfg.Month, Today: cfg.Today, Min: cfg.Min, Max: cfg.Max,
		WeekStart: cfg.WeekStart,
		// The signal is the calendar's own: one per picker, so two
		// pickers on a page never answer each other's months.
		Island: headless.Island{Endpoint: cfg.Endpoint, Signal: cfg.ID + "-calendar"},
		Path:   cfg.Path, Query: cfg.Query, MonthParam: cfg.MonthParam,
		Required: cfg.Required, Disabled: cfg.Disabled, Control: c,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs),
		Strings:    StringsFor(ctx),
	}
	if r := cfg.Range; r != nil {
		p.Range = &headless.DateRange{StartName: r.StartName, EndName: r.EndName,
			Start: r.Start, End: r.End, StartLabel: r.StartLabel, EndLabel: r.EndLabel}
	}
	if len(cfg.Days) > 0 {
		p.Days = make(map[string]headless.DateDay, len(cfg.Days))
		for k, d := range cfg.Days {
			p.Days[k] = headless.DateDay{Disabled: d.Disabled, Note: d.Note}
		}
	}
	return p
}

// DatePicker renders the field: a single date in a FormField, a range
// in a fieldset named by Label.
func DatePicker(cfg DatePickerConfig) render.HTML {
	if cfg.ID == "" || cfg.Label == "" {
		panic("ui: DatePicker requires ID and Label")
	}
	if cfg.Range != nil {
		picker := headless.Own(datePickerStyle.WrapHTML(headless.DatePicker(cfg.props(nil), datePickerClasses)))
		return formFieldStyle.WrapHTML(headless.Fieldset(headless.FieldsetProps{
			Legend:      cfg.Label,
			Description: cfg.Help,
			Error:       cfg.Error,
			ID:          cfg.ID + "-group",
			Parts:       rootClassParts(cfg.Class),
		}, datePickerGroupClasses, picker))
	}
	control := func(c headless.FieldControl) render.HTML {
		return headless.Own(datePickerStyle.WrapHTML(headless.DatePicker(cfg.props(&c), datePickerClasses)))
	}
	return formFieldStyle.WrapHTML(headless.Field(headless.FieldProps{
		Label:    cfg.Label,
		For:      cfg.ID,
		Hint:     cfg.Help,
		Error:    cfg.Error,
		Required: cfg.Required,
		Parts:    rootClassParts(cfg.Class),
	}, fieldClasses, control))
}

// DatePickerMonth renders the endpoint's answer: the calendar for
// cfg.Month, swapped into the page's picker in place. Pass the config
// the page built, with Month set from the request's MonthParam.
func DatePickerMonth(cfg DatePickerConfig) render.HTML {
	return headless.DatePickerMonth(cfg.props(nil), datePickerClasses)
}

var datePickerStyle = registry.RegisterStyle("ui-datepicker", datePickerCSS)

// datePickerCSS draws the calendar on tokens: a bordered surface, a
// header row of nav anchors around the caption, and a seven-column
// grid of day buttons whose states come from the attributes the
// headless layer and its module write (aria-selected on the cell,
// the range place, aria-current, aria-disabled), never from a class
// the script toggles.
func datePickerCSS(_ style.Theme) string {
	return `.fui-datepicker {
  display: grid;
  gap: var(--spacing-md, 8px);
  inline-size: 100%;
  max-inline-size: var(--ui-datepicker-max-width, 22rem);
}
.fui-datepicker__inputs { display: grid; gap: var(--spacing-xs, 2px) var(--spacing-md, 8px); }
/* A range's two inputs side by side, each label over its input. */
[data-hui-datepicker-range-mode] > .fui-datepicker__inputs {
  grid-template-columns: repeat(2, minmax(0, 1fr));
  grid-template-rows: auto auto;
  grid-auto-flow: column;
}
.fui-datepicker-group { margin: 0; padding: 0; border: 0; min-inline-size: 0; display: grid; gap: var(--fui-density-gap, 8px); }
.fui-datepicker-group > .fui-field__label { padding: 0; }
.fui-datepicker__calendar {
  padding: var(--spacing-md, 8px);
  border: var(--stroke-thin, 1px) solid var(--color-border, #E4E4E7);
  border-radius: var(--radii-lg, 12px);
  background: var(--color-surface, #FFFFFF);
  box-shadow: var(--shadow-xs);
}
.fui-datepicker__header {
  display: flex;
  align-items: center;
  gap: var(--spacing-xs, 2px);
  margin-block-end: var(--spacing-sm, 4px);
}
.fui-datepicker__caption {
  flex: 1;
  text-align: center;
  font-size: var(--text-sm, 0.875rem);
  font-weight: var(--font-weight-semibold);
  color: var(--color-text, #09090B);
}
.fui-datepicker__nav {
  display: inline-grid;
  place-items: center;
  inline-size: var(--ui-datepicker-nav-size, 32px);
  block-size: var(--ui-datepicker-nav-size, 32px);
  border-radius: var(--radii-md, 8px);
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-lg, 1.125rem);
  line-height: 1;
  text-decoration: none;
  transition: background-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
.fui-datepicker__nav:hover { background: var(--color-surface-soft, #F4F4F5); color: var(--color-text, #09090B); }
.fui-datepicker__nav[aria-disabled="true"] { opacity: var(--opacity-disabled, 0.4); pointer-events: none; }
/* The glyphs point the way the page reads. */
.fui-datepicker__nav:dir(rtl) > span { display: inline-block; transform: scaleX(-1); }
.fui-datepicker__nav:focus-visible,
.fui-datepicker__day:focus-visible {
  outline: var(--stroke-focus, 2px) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset, 2px);
}
.fui-datepicker__grid { inline-size: 100%; border-collapse: collapse; table-layout: fixed; }
.fui-datepicker__weekday {
  padding-block: var(--spacing-xs, 2px);
  font-size: var(--text-xs, 0.75rem);
  font-weight: var(--font-weight-medium);
  color: var(--color-text-subtle, #71717A);
  text-align: center;
}
.fui-datepicker__cell { padding: 1px 0; text-align: center; }
.fui-datepicker__day {
  display: grid;
  place-content: center;
  inline-size: 100%;
  min-block-size: var(--ui-datepicker-day-size, 40px);
  padding: 0;
  border: 0;
  border-radius: var(--radii-md, 8px);
  background: transparent;
  color: var(--color-text, #09090B);
  font: inherit;
  font-size: var(--text-sm, 0.875rem);
  font-variant-numeric: tabular-nums;
  line-height: var(--leading-snug, 1.25);
  cursor: pointer;
  transition: background-color var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
.fui-datepicker__day:hover { background: var(--color-surface-soft, #F4F4F5); }
.fui-datepicker__day[aria-current="date"] {
  font-weight: var(--font-weight-semibold);
  box-shadow: inset 0 0 0 var(--stroke-thin, 1px) var(--color-border, #E4E4E7);
}
.fui-datepicker__day[aria-disabled="true"] {
  color: var(--color-text-subtle, #71717A);
  background: transparent;
  cursor: not-allowed;
}
/* The number is struck, not the note: "Full" says why, and reads. */
.fui-datepicker__day[aria-disabled="true"] > :first-child { text-decoration: line-through; }
.fui-datepicker__note {
  font-size: var(--text-xs, 0.75rem);
  color: var(--color-text-muted, #52525B);
}
[aria-selected="true"] > .fui-datepicker__day {
  background: var(--color-primary, #18181B);
  color: var(--color-primary-fg, #FFFFFF);
}
[aria-selected="true"] > .fui-datepicker__day .fui-datepicker__note { color: inherit; }
/* A range reads as one band: the days between are a tint of the ends,
   squared off so the band runs unbroken across the week. */
[data-hui-datepicker-range="inside"] > .fui-datepicker__day {
  background: color-mix(in oklab, var(--color-primary, #18181B) 14%, transparent);
  color: var(--color-text, #09090B);
  border-radius: 0;
}
[data-hui-datepicker-range="start"]:has(~ [data-hui-datepicker-range]) > .fui-datepicker__day {
  border-start-end-radius: 0;
  border-end-end-radius: 0;
}
[data-hui-datepicker-range="end"] > .fui-datepicker__day {
  border-start-start-radius: 0;
  border-end-start-radius: 0;
}`
}
