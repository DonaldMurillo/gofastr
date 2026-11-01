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

// Min and Max refuse the days outside them and disable an anchor
// whose whole month is outside; Days refuses its own.
func TestDatePickerBounds(t *testing.T) {
	p := dpProps()
	p.Month, p.Min, p.Max = "2026-11", "2026-11-03", "2026-11-28"
	p.Days = map[string]DateDay{"2026-11-10": {Disabled: true}}
	out := string(DatePickerMonth(p, nil))
	for _, d := range []string{"2026-11-02", "2026-11-10", "2026-11-29"} {
		if !strings.Contains(out, `aria-disabled="true" aria-label="`+i18n.Date(context.Background(), mustDate(t, d), i18n.DateFull)+`" data-hui-date="`+d+`"`) {
			t.Errorf("%s is not refused", d)
		}
	}
	for _, d := range []string{"2026-11-03", "2026-11-28"} {
		if strings.Contains(out, `aria-disabled="true" aria-label="`+i18n.Date(context.Background(), mustDate(t, d), i18n.DateFull)+`" data-hui-date="`+d+`"`) {
			t.Errorf("%s, a bound itself, is refused", d)
		}
	}
	for _, nav := range []string{"prev-month", "next-month", "prev-year", "next-year"} {
		if !strings.Contains(out, `aria-disabled="true" aria-label="`) || !strings.Contains(out, `data-hui-datepicker-nav="`+nav+`"`) {
			t.Fatalf("missing %s", nav)
		}
		i := strings.Index(out, `data-hui-datepicker-nav="`+nav+`"`)
		start := strings.LastIndex(out[:i], "<a ")
		if !strings.Contains(out[start:i], `aria-disabled="true"`) || strings.Contains(out[start:i], "href=") {
			t.Errorf("%s leads to a month wholly outside the bounds and must be a disabled anchor: %s", nav, out[start:i])
		}
	}
	// The tab stop skips the refused first days.
	if !strings.Contains(out, `data-hui-date="2026-11-03" tabindex="0"`) {
		t.Error("the tab stop is not on the first day that can be picked")
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
