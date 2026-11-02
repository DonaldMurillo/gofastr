package headless

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func dpProps() DatePickerProps {
	return DatePickerProps{ID: "due", Label: "Due date", Name: "due_on",
		Island: Island{Endpoint: "/island/due", Signal: "due"}}
}

func mustPanicWith(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("no panic, want one saying %q", want)
		}
		if msg, _ := r.(string); !strings.Contains(msg, want) {
			t.Fatalf("panic %q, want one saying %q", r, want)
		}
	}()
	f()
}

// A picker that could not work refuses to render, with the reason.
func TestDatePickerRefusals(t *testing.T) {
	for _, tc := range []struct {
		want string
		edit func(*DatePickerProps)
	}{
		{"requires ID", func(p *DatePickerProps) { p.ID = "" }},
		{"requires Name", func(p *DatePickerProps) { p.Name = "" }},
		{"requires Island", func(p *DatePickerProps) { p.Island = Island{} }},
		{"Range requires StartName", func(p *DatePickerProps) {
			p.Name = ""
			p.Range = &DateRange{EndName: "b"}
		}},
		{"Range refuses Name", func(p *DatePickerProps) { p.Range = &DateRange{StartName: "a", EndName: "b"} }},
		{"Range refuses Control", func(p *DatePickerProps) {
			p.Name = ""
			p.Range = &DateRange{StartName: "a", EndName: "b"}
			p.Control = &FieldControl{ID: "due"}
		}},
		{"is not the input's ID", func(p *DatePickerProps) { p.Control = &FieldControl{ID: "other"} }},
		{"Min \"2026-13-01\" is not a date", func(p *DatePickerProps) { p.Min = "2026-13-01" }},
		{"Today \"11/02/2026\" is not a date", func(p *DatePickerProps) { p.Today = "11/02/2026" }},
		{"is before Min", func(p *DatePickerProps) { p.Min, p.Max = "2026-11-10", "2026-11-01" }},
		{"carries its own query", func(p *DatePickerProps) { p.Path = "/book?room=1" }},
		{"must be same-origin", func(p *DatePickerProps) { p.Path = "//evil.example/book" }},
		{"is not a weekday", func(p *DatePickerProps) { d := time.Weekday(7); p.WeekStart = &d }},
	} {
		t.Run(tc.want, func(t *testing.T) {
			p := dpProps()
			tc.edit(&p)
			mustPanicWith(t, tc.want, func() { DatePicker(p, nil) })
		})
	}
}

// Month and Value come from the request, so a stale link or a
// resubmitted form that is not a date falls back instead of panicking:
// the month of the value, then Today's.
func TestDatePickerUserInputFallsBack(t *testing.T) {
	p := dpProps()
	p.Month = "2026-1x"
	p.Value = "not-a-date"
	p.Today = "2026-03-04"
	out := string(DatePicker(p, nil))
	if !strings.Contains(out, `data-hui-datepicker-grid="2026-03"`) {
		t.Fatalf("a bad month and value did not fall back to Today's month:\n%s", out)
	}
	if !strings.Contains(out, `value="not-a-date"`) || strings.Contains(out, `aria-selected="true"`) {
		t.Fatalf("a value that is not a date must stay in the input and select nothing:\n%s", out)
	}
	p.Value = "2026-05-09"
	if out := string(DatePicker(p, nil)); !strings.Contains(out, `data-hui-datepicker-grid="2026-05"`) {
		t.Fatal("a valid value did not decide the month when Month is bad")
	}
}

// tagWith returns the start tag in out carrying attr, or "".
func tagWith(out, attr string) string {
	i := strings.Index(out, attr)
	if i < 0 {
		return ""
	}
	start := strings.LastIndex(out[:i], "<")
	end := strings.Index(out[i:], ">")
	return out[start : i+end+1]
}

// Min and Max refuse the days outside them; Days refuses its own; and a
// nav anchor that would not move is disabled, with no href.
func TestDatePickerBounds(t *testing.T) {
	p := dpProps()
	p.Month, p.Min, p.Max = "2026-11", "2026-11-03", "2026-11-28"
	p.Days = map[string]DateDay{"2026-11-10": {Disabled: true}}
	out := string(DatePickerMonth(p, nil))
	for d, refused := range map[string]bool{
		"2026-11-02": true, "2026-11-10": true, "2026-11-29": true,
		"2026-11-03": false, "2026-11-28": false, "2026-11-15": false,
	} {
		tag := tagWith(out, `data-hui-date="`+d+`"`)
		if tag == "" {
			t.Fatalf("no day %s", d)
		}
		if got := strings.Contains(tag, `aria-disabled="true"`); got != refused {
			t.Errorf("%s refused = %v, want %v: %s", d, got, refused, tag)
		}
	}
	for _, nav := range []string{"prev-month", "next-month", "prev-year", "next-year"} {
		tag := tagWith(out, `data-hui-datepicker-nav="`+nav+`"`)
		if !strings.Contains(tag, `aria-disabled="true"`) || strings.Contains(tag, "href=") || strings.Contains(tag, "data-cui-rpc") {
			t.Errorf("%s would not move and must be a disabled anchor: %s", nav, tag)
		}
	}
	// The tab stop skips the refused first days.
	if tag := tagWith(out, `data-hui-date="2026-11-03"`); !strings.Contains(tag, `tabindex="0"`) {
		t.Errorf("the tab stop is not on the first day that can be picked: %s", tag)
	}
}

// The shown month is held between Min's and Max's months, so a picker
// never opens on a month it cannot leave; a year anchor lands on the
// nearest reachable month rather than going dark with months behind it.
func TestDatePickerHoldsTheMonthInBounds(t *testing.T) {
	p := dpProps()
	p.Today, p.Min, p.Max = "2026-01-15", "2026-03-01", "2027-02-10"
	out := string(DatePickerMonth(p, nil))
	if !strings.Contains(out, `data-hui-datepicker-grid="2026-03"`) {
		t.Fatalf("Today before Min did not open on Min's month:\n%s", out)
	}
	if tag := tagWith(out, `data-hui-datepicker-nav="next-year"`); !strings.Contains(tag, `href="?month=2027-02"`) {
		t.Errorf("next year from March 2026 must land on February 2027, Max's month: %s", tag)
	}
	if tag := tagWith(out, `data-hui-datepicker-nav="prev-month"`); !strings.Contains(tag, `aria-disabled="true"`) {
		t.Errorf("at Min's month the previous month must be disabled: %s", tag)
	}
	for month, want := range map[string]string{
		"2025-06": "2026-03", // a stale link before Min
		"2030-01": "2027-02", // after Max
		"0000-01": "2026-03", // year zero is not a month
	} {
		p.Month = month
		if out := string(DatePickerMonth(p, nil)); !strings.Contains(out, `data-hui-datepicker-grid="`+want+`"`) {
			t.Errorf("Month %s did not show %s", month, want)
		}
	}
}

// A range selects many cells, which a grid allows only when it says so;
// a single date selects one, and says nothing.
func TestDatePickerRangeGridIsMultiselectable(t *testing.T) {
	p := dpProps()
	p.Month = "2026-11"
	if strings.Contains(string(DatePickerMonth(p, nil)), "aria-multiselectable") {
		t.Error("a single date's grid claims many selections")
	}
	p.Name = ""
	p.Range = &DateRange{StartName: "a", EndName: "b", Start: "2026-11-02", End: "2026-11-04"}
	out := string(DatePickerMonth(p, nil))
	if !strings.Contains(tagWith(out, `role="grid"`), `aria-multiselectable="true"`) {
		t.Error("a range's grid does not say it holds many selections")
	}
	if tag := tagWith(out, `data-hui-date="2026-11-03"`); !strings.Contains(tag, `aria-pressed="true"`) {
		t.Errorf("a day inside the range does not say so on the button focus lands on: %s", tag)
	}
}

// The week starts where the reader's does, and the digits and the
// names are the reader's.
func TestDatePickerFollowsTheLocale(t *testing.T) {
	p := dpProps()
	p.Month = "2026-11"
	p.Strings = DefaultStrings().ForRequest(i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"}))
	out := string(DatePickerMonth(p, nil))
	if !strings.Contains(out, `<th abbr="lundi" scope="col">lun.</th>`) {
		t.Errorf("French weeks start on Monday, named in French:\n%s", out)
	}
	if !strings.Contains(out, `>novembre 2026<`) {
		t.Error("the caption is not French")
	}
	p.Strings = DefaultStrings().ForRequest(i18n.WithContext(context.Background(), i18n.Locale{Tag: "ar-EG"}))
	if out := string(DatePickerMonth(p, nil)); !strings.Contains(out, `<span>١٢</span>`) {
		t.Error("Egyptian Arabic days are not written in its digits")
	}
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse(isoDate, s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
