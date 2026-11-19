package localentity_test

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A Date field takes every year core/schema's Date takes. day() built
// the date with Date.UTC, which reads years 0-99 as 1900-1999, so
// 0050-01-01 came back as 1950-01-01, failed its own round-trip, and
// the form refused a date the server accepts (found by core-ui/check's
// dateutcyear lint).
func TestDateFieldKeepsYearsBelow100(t *testing.T) {
	base := startHost(t)
	tab := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(tab,
		chromedp.Navigate(base+"/"),
		countIs(0),
		chromedp.Evaluate(`document.querySelector('#`+noteForm+` form').noValidate = true`, nil),
		chromedp.SetValue(`#f-title`, "y50", chromedp.ByQuery),
		chromedp.SetValue(`#f-due`, "0050-01-01", chromedp.ByQuery),
		submit(),
		poll(`document.querySelector('[data-fui-local-count]').textContent === '1' || !!document.querySelector('#f-due[aria-invalid="true"]')`),
	); err != nil {
		t.Fatalf("save: %v", err)
	}
	var invalid bool
	if err := chromedp.Run(tab, chromedp.Evaluate(`!!document.querySelector('#f-due[aria-invalid="true"]')`, &invalid)); err != nil {
		t.Fatal(err)
	}
	if invalid {
		t.Fatal("the form refused 0050-01-01, a date core/schema accepts")
	}
	if got := records(t, tab); len(got) != 1 || got[0]["due"] != "0050-01-01" {
		t.Fatalf("stored %v, want one record due 0050-01-01", got)
	}
}

// fill wrote the year without its leading zeros, so a datetime-local
// control holding a year-0050 stamp cleared itself and an untouched
// save dropped the field. The year is written the control's four-digit
// width.
func TestUntouchedDateTimeKeepsEarlyYears(t *testing.T) {
	base := startHost(t)
	tab := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	const stored = "0050-01-01T14:30:45.123Z"
	edit := chromedp.Evaluate(`Array.from(document.querySelectorAll('[data-fui-local-item]'))
		.find((n) => n.textContent.includes('y5')).querySelector('[data-fui-local-edit] button').click()`, nil)
	if err := chromedp.Run(tab,
		chromedp.Navigate(base+"/"),
		chromedp.Evaluate(`__gofastr.localdb.open('localentity-e2e').then((db) => db.put('notes', {
			id: 'y5', title: 'y5', when: '`+stored+`',
			created_at: '2024-01-01T00:00:00.000Z', updated_at: '2024-01-01T00:00:00.000Z' }))`, nil, await),
		poll(`Array.from(document.querySelectorAll('[data-fui-local-item]')).some((n) => n.textContent.includes('y5'))`),
		edit,
		poll(`document.querySelector('#f-when').value.startsWith('0050-')`),
		save(),
	); err != nil {
		t.Fatalf("edit untouched: %v", err)
	}
	var rec map[string]any
	if err := chromedp.Run(tab, chromedp.Evaluate(`__gofastr.localdb.open('localentity-e2e').then((db) => db.get('notes', 'y5'))`, &rec, await)); err != nil {
		t.Fatal(err)
	}
	if got, _ := rec["when"].(string); got != stored {
		t.Fatalf("untouched when saved as %q, want %q", got, stored)
	}
}
