package headless

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// DatePicker parts.
const (
	PartDatePickerInputs   Part = "datepicker-inputs"
	PartDatePickerInput    Part = "datepicker-input"
	PartDatePickerCalendar Part = "datepicker-calendar"
	PartDatePickerMonth    Part = "datepicker-month"
	PartDatePickerHeader   Part = "datepicker-header"
	PartDatePickerNav      Part = "datepicker-nav"
	PartDatePickerCaption  Part = "datepicker-caption"
	PartDatePickerGrid     Part = "datepicker-grid"
	PartDatePickerWeekday  Part = "datepicker-weekday"
	PartDatePickerCell     Part = "datepicker-cell"
	PartDatePickerDay      Part = "datepicker-day"
	PartDatePickerNote     Part = "datepicker-note"
	PartDatePickerStatus   Part = "datepicker-status"
)

const (
	isoDate  = "2006-01-02"
	isoMonth = "2006-01"
)

// DateRange turns a DatePicker into a range picker with a start and
// an end input.
type DateRange struct {
	// StartName and EndName are the two inputs' form names. Required.
	StartName, EndName string
	// Start and End are the current values, YYYY-MM-DD.
	Start, End string
	// StartLabel and EndLabel name the inputs; empty takes the Strings defaults.
	StartLabel, EndLabel string
}

// DateDay is what the server knows about one day.
type DateDay struct {
	// Disabled refuses the day; it stays reachable, announced as unavailable.
	Disabled bool
	// Note is a short line on the day ("3 left"), read after its date.
	Note string
}

// DatePickerProps configures a DatePicker and the DatePickerMonth its
// endpoint answers.
type DatePickerProps struct {
	// ID is the input's id; a range takes <ID>-start and <ID>-end. Required.
	ID string
	// Label names the calendar and a single date's input. Required.
	Label string

	// Name is the single input's form name. Required unless Range.
	Name string
	// Value is the single input's value, YYYY-MM-DD; a non-date is kept and selects nothing.
	Value string
	// Range makes this a range picker; Name and Value are then refused.
	Range *DateRange

	// Month is the month shown, YYYY-MM; a non-month falls back to the value's, Today's or the current one.
	Month string
	// Today is the reader's today, YYYY-MM-DD; a non-date panics.
	Today string
	// Min and Max bound the dates, YYYY-MM-DD; a non-date panics.
	Min, Max string
	// Days is what the server knows about each day, keyed YYYY-MM-DD.
	Days map[string]DateDay
	// WeekStart is the first column's weekday; nil takes the locale's.
	WeekStart *time.Weekday

	// Island answers DatePickerMonth for a month change. Required.
	Island Island
	// Path is the page's constant path for the nav anchors' hrefs, never r.URL.Path.
	Path string
	// Query is the page's query, kept across a month change.
	Query url.Values
	// MonthParam names the month query parameter. Default "month".
	MonthParam string

	Required bool
	Disabled bool
	// Control hands a single date's input to a host Field; refused with Range.
	Control *FieldControl

	ExtraAttrs html.Attrs
	Parts      Parts
	Strings    *Strings
}

// DatePicker renders the native input (or the range's two), the
// calendar for the shown month, and its status region.
func DatePicker(p DatePickerProps, s Classes) render.HTML {
	p.check()
	w := p.Strings.Resolve()
	b := p.Parts.Box(s)

	rootAttrs := Safe(p.ExtraAttrs, "role")
	Mark(rootAttrs, "data-hui-datepicker")
	if p.Range != nil {
		rootAttrs["data-hui-datepicker-range-mode"] = ""
		rootAttrs["data-hui-datepicker-choose-end"] = w.DatePickerChooseEnd
		rootAttrs["data-hui-datepicker-ranged"] = w.DatePickerRange
	} else {
		rootAttrs["data-hui-datepicker-chosen"] = w.DatePickerChosen
	}

	inputs := make([]render.HTML, 0, 4)
	if r := p.Range; r != nil {
		inputs = append(inputs,
			datePickerInput(b, p, p.ID+"-start", r.StartName, r.Start, orDefault(r.StartLabel, w.DatePickerStart), "start", nil),
			datePickerInput(b, p, p.ID+"-end", r.EndName, r.End, orDefault(r.EndLabel, w.DatePickerEnd), "end", nil))
	} else {
		inputs = append(inputs, datePickerInput(b, p, p.ID, p.Name, p.Value, p.Label, "single", p.Control))
	}

	calAttrs := Attrs(map[string]string{
		"id":                   p.ID + "-calendar",
		"role":                 "group",
		"aria-label":           scrubControlBytes(p.Label),
		"data-cui-signal":      p.Island.Signal,
		"data-cui-signal-mode": "html",
	})
	Mark(calAttrs, "data-hui-datepicker-calendar")

	statusAttrs := Attrs(map[string]string{"id": p.ID + "-status", "role": "status"})
	Mark(statusAttrs, "data-hui-datepicker-status")

	return b.El("div", PartRoot, rootAttrs,
		b.El("div", PartDatePickerInputs, Internal(nil), inputs...),
		b.El("div", PartDatePickerCalendar, Internal(calAttrs), datePickerMonth(b, p, w)),
		b.El("span", PartDatePickerStatus, Internal(statusAttrs), render.HTML("")))
}

// DatePickerMonth renders the calendar for p's month, the Island
// endpoint's answer; Month is the MonthParam query value.
func DatePickerMonth(p DatePickerProps, s Classes) render.HTML {
	p.check()
	return datePickerMonth(p.Parts.Box(s), p, p.Strings.Resolve())
}

func datePickerInput(b Box, p DatePickerProps, id, name, value, label, which string, c *FieldControl) render.HTML {
	attrs := Attrs(map[string]string{
		"type":  "date",
		"id":    id,
		"name":  name,
		"value": scrubControlBytes(value),
		"min":   p.Min,
		"max":   p.Max,
	})
	attrs["data-hui-datepicker-input"] = which
	Flag(attrs, "required", p.Required)
	Flag(attrs, "disabled", p.Disabled)
	if c != nil {
		if c.DescribedBy != "" {
			attrs["aria-describedby"] = c.DescribedBy
		}
		if c.Invalid {
			attrs["aria-invalid"] = "true"
		}
		if c.Required {
			attrs["required"] = ""
		}
		return b.El("input", PartDatePickerInput, attrs)
	}
	return group(
		b.El("label", PartLabel, Attrs(map[string]string{"for": id}), render.Text(scrubControlBytes(label))),
		b.El("input", PartDatePickerInput, attrs))
}

func (p DatePickerProps) check() {
	if p.ID == "" {
		panic("headless: DatePicker requires ID — the inputs, the calendar and its caption are paired by it")
	}
	checkFragmentID("DatePicker", "ID", p.ID)
	checkLabel("DatePicker", "Label", p.Label)
	if r := p.Range; r != nil {
		if r.StartName == "" || r.EndName == "" {
			panic("headless: DatePicker Range requires StartName and EndName — the two inputs submit under them")
		}
		if p.Name != "" || p.Value != "" {
			panic("headless: DatePicker Range refuses Name and Value — the range's StartName, EndName, Start and End say both")
		}
		if p.Control != nil {
			panic("headless: DatePicker Range refuses Control — a Field names one control, and a range is two; group them in a Fieldset")
		}
	} else if p.Name == "" {
		panic("headless: DatePicker requires Name — the native input submits the date under it")
	}
	if p.Control != nil && p.Control.ID != p.ID {
		panic("headless: DatePicker Control.ID " + p.Control.ID + " is not the input's ID " + p.ID + " — the field's label would name nothing")
	}
	requireIsland("DatePicker", p.Island)
	if p.Path != "" {
		checkSameOrigin("DatePicker", "Path", p.Path)
		if strings.ContainsAny(p.Path, "?#") {
			panic("headless: DatePicker Path " + strconv.Quote(p.Path) + " carries its own query or fragment — the carry belongs in Query, where it survives the month change")
		}
	}
	for _, f := range []struct{ name, v string }{{"Today", p.Today}, {"Min", p.Min}, {"Max", p.Max}} {
		if f.v == "" {
			continue
		}
		if _, err := time.Parse(isoDate, f.v); err != nil {
			panic("headless: DatePicker " + f.name + " " + strconv.Quote(f.v) + " is not a date in the HTML format, YYYY-MM-DD")
		}
	}
	if p.Min != "" && p.Max != "" && p.Max < p.Min {
		panic("headless: DatePicker Max " + p.Max + " is before Min " + p.Min + " — no day could be picked")
	}
	if p.WeekStart != nil && (*p.WeekStart < time.Sunday || *p.WeekStart > time.Saturday) {
		panic("headless: DatePicker WeekStart " + strconv.Itoa(int(*p.WeekStart)) + " is not a weekday")
	}
}

func (p DatePickerProps) shownMonth() time.Time {
	lo, hi := p.monthBounds()
	if m, err := time.Parse(isoMonth, p.Month); err == nil && m.Year() >= 1 {
		return clampMonth(m, lo, hi)
	}
	for _, v := range []string{p.Value, p.rangeStart(), p.Today} {
		if d, err := time.Parse(isoDate, v); err == nil && d.Year() >= 1 {
			return clampMonth(monthOf(d), lo, hi)
		}
	}
	return clampMonth(monthOf(time.Now().UTC()), lo, hi)
}

func (p DatePickerProps) monthBounds() (lo, hi time.Time) {
	lo = time.Date(1, time.January, 1, 0, 0, 0, 0, time.UTC)
	hi = time.Date(9999, time.December, 1, 0, 0, 0, 0, time.UTC)
	if d, err := time.Parse(isoDate, p.Min); err == nil && d.Year() >= 1 {
		lo = monthOf(d)
	}
	if d, err := time.Parse(isoDate, p.Max); err == nil {
		hi = monthOf(d)
	}
	return lo, hi
}

func monthOf(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
}

func clampMonth(m, lo, hi time.Time) time.Time {
	if m.Before(lo) {
		return lo
	}
	if m.After(hi) {
		return hi
	}
	return m
}

func (p DatePickerProps) rangeStart() string {
	if p.Range == nil {
		return ""
	}
	return p.Range.Start
}

func (p DatePickerProps) selected(d string) (bool, string) {
	if r := p.Range; r != nil {
		start, end := validDate(r.Start), validDate(r.End)
		switch {
		case start == "":
			return false, ""
		case end == "" || end < start:
			if d == start {
				return true, "start"
			}
			return false, ""
		case d == start:
			return true, "start"
		case d == end:
			return true, "end"
		case d > start && d < end:
			return true, "inside"
		}
		return false, ""
	}
	v := validDate(p.Value)
	return v != "" && d == v, ""
}

func validDate(s string) string {
	if _, err := time.Parse(isoDate, s); err != nil {
		return ""
	}
	return s
}

func datePickerMonth(b Box, p DatePickerProps, w *Strings) render.HTML {
	month := p.shownMonth()
	ctx := w.ctx
	first := month
	last := month.AddDate(0, 1, -1)
	captionID := p.ID + "-caption"

	lo, hi := p.monthBounds()
	nav := func(which, label, glyph string, target time.Time) render.HTML {
		tFirst := clampMonth(target, lo, hi)
		disabled := p.Disabled || tFirst.Equal(month)
		attrs := html.Attrs{"data-hui-datepicker-nav": which}
		if disabled {
			attrs["aria-disabled"] = "true"
		} else {
			href := p.monthHref(tFirst)
			attrs["href"] = href
			attrs["data-cui-rpc"] = mergeQuery(p.Island.Endpoint, href)
			attrs["data-cui-rpc-method"] = "GET"
			attrs["data-cui-rpc-signal"] = p.Island.Signal
			attrs["data-cui-rpc-error-toast"] = w.DatePickerMonthFailed
		}
		return b.El("a", PartDatePickerNav, attrs,
			render.Tag("span", map[string]string{"aria-hidden": "true"}, render.Text(glyph)),
			b.El("span", PartVisuallyHidden, nil, render.Text(label)))
	}

	captionAttrs := Attrs(map[string]string{"id": captionID})
	Mark(captionAttrs, "data-hui-datepicker-caption")
	header := b.El("div", PartDatePickerHeader, Internal(nil),
		nav("prev-year", w.DatePickerPrevYear, "«", month.AddDate(-1, 0, 0)),
		nav("prev-month", w.DatePickerPrevMonth, "‹", month.AddDate(0, -1, 0)),
		b.El("span", PartDatePickerCaption, captionAttrs,
			render.Text(i18n.Date(ctx, month, i18n.DateYearMonthLong))),
		nav("next-month", w.DatePickerNextMonth, "›", month.AddDate(0, 1, 0)),
		nav("next-year", w.DatePickerNextYear, "»", month.AddDate(1, 0, 0)))

	weekStart := i18n.FirstWeekday(ctx)
	if p.WeekStart != nil {
		weekStart = *p.WeekStart
	}
	heads := make([]render.HTML, 0, 7)
	for i := range 7 {
		wd := time.Weekday((int(weekStart) + i) % 7)
		heads = append(heads, b.El("th", PartDatePickerWeekday, Attrs(map[string]string{
			"scope": "col",
			"abbr":  i18n.WeekdayName(ctx, wd),
		}), render.Text(i18n.WeekdayAbbr(ctx, wd))))
	}

	tabDay := ""
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		if sel, place := p.selected(d.Format(isoDate)); sel && (place == "" || place == "start") {
			tabDay = d.Format(isoDate)
			break
		}
	}
	if tabDay == "" && strings.HasPrefix(p.Today, month.Format(isoMonth)) {
		tabDay = p.Today
	}
	if tabDay == "" {
		for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
			if !p.refused(d.Format(isoDate)) {
				tabDay = d.Format(isoDate)
				break
			}
		}
	}
	if tabDay == "" {
		tabDay = first.Format(isoDate)
	}

	lead := (int(first.Weekday()) - int(weekStart) + 7) % 7
	cells := make([]render.HTML, 0, 42)
	for range lead {
		cells = append(cells, b.El("td", PartDatePickerCell, nil))
	}
	for d := first; !d.After(last); d = d.AddDate(0, 0, 1) {
		cells = append(cells, datePickerDay(b, p, w, d, tabDay))
	}
	for len(cells)%7 != 0 {
		cells = append(cells, b.El("td", PartDatePickerCell, nil))
	}
	rows := make([]render.HTML, 0, 6)
	for i := 0; i < len(cells); i += 7 {
		rows = append(rows, render.Tag("tr", nil, cells[i:i+7]...))
	}

	gridAttrs := Attrs(map[string]string{
		"role":            "grid",
		"aria-labelledby": captionID,
	})
	gridAttrs["data-hui-datepicker-grid"] = month.Format(isoMonth)
	if p.Range != nil {
		gridAttrs["aria-multiselectable"] = "true"
	}
	grid := b.El("table", PartDatePickerGrid, Internal(gridAttrs),
		render.Tag("thead", nil, render.Tag("tr", nil, heads...)),
		render.Tag("tbody", nil, rows...))
	return b.El("div", PartDatePickerMonth, nil, header, grid)
}

func (p DatePickerProps) refused(d string) bool {
	if p.Disabled || (p.Min != "" && d < p.Min) || (p.Max != "" && d > p.Max) {
		return true
	}
	return p.Days[d].Disabled
}

func datePickerDay(b Box, p DatePickerProps, w *Strings, d time.Time, tabDay string) render.HTML {
	iso := d.Format(isoDate)
	sel, place := p.selected(iso)
	cellAttrs := Attrs(map[string]string{"aria-selected": strconv.FormatBool(sel)})
	if place != "" {
		cellAttrs["data-hui-datepicker-range"] = place
	}
	dayAttrs := Attrs(map[string]string{
		"type":       "button",
		"aria-label": i18n.Date(w.ctx, d, i18n.DateFull),
	})
	dayAttrs["data-hui-date"] = iso
	dayAttrs["aria-pressed"] = strconv.FormatBool(sel)
	dayAttrs["tabindex"] = "-1"
	if iso == tabDay {
		dayAttrs["tabindex"] = "0"
	}
	if iso == p.Today {
		dayAttrs["aria-current"] = "date"
	}
	if p.refused(iso) {
		dayAttrs["aria-disabled"] = "true"
	}
	kids := []render.HTML{render.Tag("span", nil, render.Text(w.Number(d.Day())))}
	if note := scrubControlBytes(p.Days[iso].Note); note != "" {
		noteID := p.ID + "-note-" + iso
		dayAttrs["aria-describedby"] = noteID
		kids = append(kids, b.El("span", PartDatePickerNote, Attrs(map[string]string{"id": noteID}), render.Text(note)))
	}
	return b.El("td", PartDatePickerCell, cellAttrs, b.El("button", PartDatePickerDay, dayAttrs, kids...))
}

func (p DatePickerProps) monthHref(month time.Time) string {
	q := scrubbedQuery(p.Query)
	q.Set(orDefault(p.MonthParam, "month"), month.Format(isoMonth))
	return queryHref("DatePicker", p.Path, q)
}

func init() {
	nov := &Island{Endpoint: "/island/booking/calendar", Signal: "booking-calendar"}
	monday := time.Monday
	Register(Spec{
		Name: "DatePicker",
		Anatomy: []Part{PartRoot, PartLabel, PartDatePickerInputs, PartDatePickerInput,
			PartDatePickerCalendar, PartDatePickerMonth, PartDatePickerHeader, PartDatePickerNav,
			PartVisuallyHidden, PartDatePickerCaption, PartDatePickerGrid, PartDatePickerWeekday,
			PartDatePickerCell, PartDatePickerDay, PartDatePickerNote, PartDatePickerStatus},
		Hooks: []string{"data-hui-datepicker", "data-hui-datepicker-input",
			"data-hui-datepicker-calendar", "data-hui-datepicker-nav",
			"data-hui-datepicker-caption", "data-hui-datepicker-grid",
			"data-hui-date", "data-hui-datepicker-range",
			"data-hui-datepicker-status", "data-hui-datepicker-range-mode",
			"data-hui-datepicker-chosen", "data-hui-datepicker-choose-end",
			"data-hui-datepicker-ranged"},
		WithParts: func(s Classes, parts Parts) render.HTML {
			return DatePicker(DatePickerProps{ID: "due", Label: "Due date", Name: "due_on",
				Value: "2026-11-05", Month: "2026-11", Island: *nov, Parts: parts}, s)
		},
		Cases: func(k Kit) []Case {
			s := k.Classes
			return []Case{{
				Name: "a single date",
				Why:  "the native input is the value and submits without script; the grid names each day by its full date, one day is in the tab order, the selected cell says so, and the nav anchors keep a real href beside the island contract",
				HTML: DatePicker(DatePickerProps{ID: "due", Label: "Due date", Name: "due_on",
					Value: "2026-11-05", Month: "2026-11", Today: "2026-11-02",
					Island: *nov, Path: "/tasks/new"}, s),
			}, {
				Name: "a range with refused days and notes",
				Why:  "two native inputs carry the start and the end, the cells between say they are selected, a booked day is aria-disabled yet reachable, a note is read after its date, and a month wholly before Min disables its anchor",
				HTML: DatePicker(DatePickerProps{ID: "stay", Label: "Stay",
					Range: &DateRange{StartName: "check_in", EndName: "check_out", Start: "2026-11-10", End: "2026-11-13"},
					Month: "2026-11", Min: "2026-11-01", WeekStart: &monday,
					Days: map[string]DateDay{
						"2026-11-20": {Disabled: true, Note: "Full"},
						"2026-11-21": {Note: "2 left"},
					},
					Island: *nov, Query: url.Values{"room": {"suite"}}}, s),
			}}
		},
	})
	Register(Spec{
		Name: "DatePickerMonth",
		Anatomy: []Part{PartDatePickerMonth, PartDatePickerHeader, PartDatePickerNav, PartVisuallyHidden,
			PartDatePickerCaption, PartDatePickerGrid, PartDatePickerWeekday, PartDatePickerCell, PartDatePickerDay},
		Cases: func(k Kit) []Case {
			return []Case{{
				Name: "a month an endpoint answers",
				Why:  "the swapped-in calendar carries the same ids and hooks as the page's, so the module's focus and selection survive the swap, and a month wholly after Max disables its anchors",
				HTML: render.Tag("div", map[string]string{"id": "due-calendar", "role": "group", "aria-label": "Due date"},
					DatePickerMonth(DatePickerProps{ID: "due", Label: "Due date", Name: "due_on",
						Month: "2026-12", Max: "2026-12-31", Island: *nov}, k.Classes)),
			}}
		},
	})
}
