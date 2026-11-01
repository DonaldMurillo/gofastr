package headless

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

const datepickerLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-datepicker'])`

// datePickerServer serves a page whose host form holds the picker,
// the island endpoint that answers DatePickerMonth for the asked
// month, and the form's target, which echoes what it received.
func datePickerServer(t *testing.T, props func(month string) DatePickerProps) context.Context {
	t.Helper()
	page := `<form id="host" method="post" action="/submit">` +
		string(DatePicker(props(""), nil)) +
		`<button id="save" type="submit">Save</button></form>`
	b := startBehaviorServer(t, page, func(mux *http.ServeMux) {
		mux.HandleFunc("/island/cal", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, DatePickerMonth(props(r.URL.Query().Get("month")), nil))
		})
		mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><p id="got">%s</p>`, html.EscapeString(r.PostForm.Encode()))
		})
	})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, datepickerLoaded) {
		t.Fatal("the picker's marker never loaded headless-datepicker")
	}
	return ctx
}

func dpRun(t *testing.T, ctx context.Context, actions ...chromedp.Action) {
	t.Helper()
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatal(err)
	}
}

func dpKey(key string) chromedp.Action {
	return chromedp.KeyEvent(key)
}

// dpShiftKey sends key with Shift held, the year jump's chord.
func dpShiftKey(key string) chromedp.Action {
	return chromedp.KeyEvent(key, chromedp.KeyModifiers(input.ModifierShift))
}

// The issue's bar: selecting a day round-trips. A click writes the day
// into the native input; the keys move through the grid and across a
// month, the island swapping the new month in with focus on the day
// the key named; Enter picks; and the host form posts the picked date
// to the server, exactly as a typed one would.
func TestE2E_DatePickerSelectingADayRoundTrips(t *testing.T) {
	ctx := datePickerServer(t, func(month string) DatePickerProps {
		return DatePickerProps{ID: "due", Label: "Due date", Name: "due_on",
			Month: orDefault(month, "2026-11"), Today: "2026-11-02",
			Days:   map[string]DateDay{"2026-11-20": {Disabled: true, Note: "Closed"}},
			Island: Island{Endpoint: "/island/cal", Signal: "due-cal"}}
	})

	dpRun(t, ctx, chromedp.Click(`[data-hui-date="2026-11-12"]`, chromedp.ByQuery))
	if !pollTrue(ctx, `document.getElementById('due').value === '2026-11-12' &&
		document.querySelector('[data-hui-date="2026-11-12"]').closest('td').getAttribute('aria-selected') === 'true' &&
		document.querySelector('[data-hui-date="2026-11-02"]').closest('td').getAttribute('aria-selected') === 'false'`) {
		t.Fatal("a click did not write the day into the native input and mark its cell")
	}
	if !pollTrue(ctx, `document.getElementById('due-status').textContent === 'Thursday, November 12, 2026 selected.'`) {
		t.Fatal("the pick was not announced")
	}

	// A refused day stays reachable but picks nothing.
	dpRun(t, ctx, chromedp.Click(`[data-hui-date="2026-11-20"]`, chromedp.ByQuery))
	if !pollTrue(ctx, `document.getElementById('due').value === '2026-11-12'`) {
		t.Fatal("a disabled day was picked")
	}

	// Arrows move focus a day and a week, and the tab stop follows.
	dpRun(t, ctx, chromedp.Focus(`[data-hui-date="2026-11-12"]`, chromedp.ByQuery), dpKey(kb.ArrowRight), dpKey(kb.ArrowDown))
	if !pollTrue(ctx, `document.activeElement.getAttribute('data-hui-date') === '2026-11-20' &&
		document.activeElement.getAttribute('tabindex') === '0' &&
		document.querySelectorAll('#due-calendar [tabindex="0"]').length === 1`) {
		t.Fatal("ArrowRight then ArrowDown did not move focus to November 20 with one tab stop")
	}

	// PageDown crosses the month: the island swaps December in and
	// focus lands on the same day of it.
	dpRun(t, ctx, dpKey(kb.PageDown))
	if !pollTrue(ctx, `document.getElementById('due-caption').textContent === 'December 2026' &&
		document.activeElement.getAttribute('data-hui-date') === '2026-12-20'`) {
		t.Fatal("PageDown did not swap December in with focus on December 20")
	}
	if !pollTrue(ctx, `document.getElementById('due-status').textContent === 'December 2026'`) {
		t.Fatal("the new month was not announced")
	}
	// The swapped month knows nothing of the pick; the module marks it
	// from the input, so going back to November shows the 12th again.
	dpRun(t, ctx, dpKey(kb.ArrowLeft), dpKey(kb.Enter))
	if !pollTrue(ctx, `document.getElementById('due').value === '2026-12-19' &&
		document.querySelector('[data-hui-date="2026-12-19"]').closest('td').getAttribute('aria-selected') === 'true'`) {
		t.Fatal("Enter on December 19 did not pick it")
	}

	// Shift+PageUp is a year back, through the year anchor.
	dpRun(t, ctx, dpShiftKey(kb.PageUp))
	if !pollTrue(ctx, `document.getElementById('due-caption').textContent === 'December 2025' &&
		document.activeElement.getAttribute('data-hui-date') === '2025-12-19'`) {
		t.Fatal("Shift+PageUp did not move a year back")
	}

	// A mouse on the nav anchor keeps focus on it across the swap.
	dpRun(t, ctx, chromedp.Click(`[data-hui-datepicker-nav="next-year"]`, chromedp.ByQuery))
	if !pollTrue(ctx, `document.getElementById('due-caption').textContent === 'December 2026' &&
		document.activeElement.getAttribute('data-hui-datepicker-nav') === 'next-year' &&
		document.querySelector('[data-hui-date="2026-12-19"]').closest('td').getAttribute('aria-selected') === 'true'`) {
		t.Fatal("the next-year anchor did not keep focus, or the swapped month lost the pick's mark")
	}

	// Typing a date in the native input moves the mark with it.
	dpRun(t, ctx, chromedp.Evaluate(`(() => { const i = document.getElementById('due'); i.value = '2026-12-03'; i.dispatchEvent(new Event('input', {bubbles: true})); })()`, nil))
	if !pollTrue(ctx, `document.querySelector('[data-hui-date="2026-12-03"]').closest('td').getAttribute('aria-selected') === 'true' &&
		document.querySelector('[data-hui-date="2026-12-19"]').closest('td').getAttribute('aria-selected') === 'false'`) {
		t.Fatal("a typed date did not move the grid's mark")
	}

	// The round trip: the host form posts the native input's value.
	// The announcement lands in the unstyled status region and moves
	// what is under it, so the click on Save waits for it.
	dpRun(t, ctx, chromedp.Click(`[data-hui-date="2026-12-24"]`, chromedp.ByQuery))
	if !pollTrue(ctx, `document.getElementById('due-status').textContent === 'Thursday, December 24, 2026 selected.'`) {
		t.Fatal("the last pick was not announced")
	}
	dpRun(t, ctx, chromedp.Click(`#save`, chromedp.ByID))
	if !pollTrue(ctx, `!!document.getElementById('got') && document.getElementById('got').textContent === 'due_on=2026-12-24'`) {
		var got string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`document.body ? document.body.innerText : ''`, &got))
		t.Fatalf("the form did not post the picked date; the page says %q", got)
	}
}

// A range: the first pick is the start and asks for the end, the
// second is the end and the cells between are marked; a pick before
// the start restarts the range; both inputs post.
func TestE2E_DatePickerPicksARange(t *testing.T) {
	ctx := datePickerServer(t, func(month string) DatePickerProps {
		return DatePickerProps{ID: "stay", Label: "Stay",
			Range:  &DateRange{StartName: "check_in", EndName: "check_out"},
			Month:  orDefault(month, "2026-11"),
			Island: Island{Endpoint: "/island/cal", Signal: "stay-cal"}}
	})
	dpRun(t, ctx, chromedp.Click(`[data-hui-date="2026-11-10"]`, chromedp.ByQuery))
	if !pollTrue(ctx, `document.getElementById('stay-start').value === '2026-11-10' && document.getElementById('stay-end').value === '' &&
		document.getElementById('stay-status').textContent === 'Tuesday, November 10, 2026 selected. Choose the end date.'`) {
		t.Fatal("the first pick did not set the start and ask for the end")
	}
	dpRun(t, ctx, chromedp.Click(`[data-hui-date="2026-11-13"]`, chromedp.ByQuery))
	if !pollTrue(ctx, `document.getElementById('stay-end').value === '2026-11-13' &&
		document.querySelector('[data-hui-date="2026-11-11"]').closest('td').getAttribute('data-hui-datepicker-range') === 'inside' &&
		document.querySelector('[data-hui-date="2026-11-13"]').closest('td').getAttribute('data-hui-datepicker-range') === 'end' &&
		document.querySelectorAll('#stay-calendar td[aria-selected="true"]').length === 4 &&
		document.getElementById('stay-status').textContent === 'Tuesday, November 10, 2026 to Friday, November 13, 2026 selected.'`) {
		t.Fatal("the second pick did not set the end and mark the range")
	}
	dpRun(t, ctx, chromedp.Click(`[data-hui-date="2026-11-05"]`, chromedp.ByQuery))
	if !pollTrue(ctx, `document.getElementById('stay-start').value === '2026-11-05' && document.getElementById('stay-end').value === '' &&
		document.querySelectorAll('#stay-calendar td[aria-selected="true"]').length === 1`) {
		t.Fatal("a pick after a whole range did not restart it")
	}
	// The end in the next month: the range crosses the swap.
	dpRun(t, ctx, chromedp.Click(`[data-hui-datepicker-nav="next-month"]`, chromedp.ByQuery))
	if !pollTrue(ctx, `document.getElementById('stay-caption').textContent === 'December 2026'`) {
		t.Fatal("the next-month anchor did not swap December in")
	}
	dpRun(t, ctx, chromedp.Click(`[data-hui-date="2026-12-02"]`, chromedp.ByQuery))
	if !pollTrue(ctx, `document.getElementById('stay-end').value === '2026-12-02' &&
		document.querySelector('[data-hui-date="2026-12-01"]').closest('td').getAttribute('data-hui-datepicker-range') === 'inside' &&
		document.getElementById('stay-status').textContent === 'Thursday, November 5, 2026 to Wednesday, December 2, 2026 selected.'`) {
		t.Fatal("an end in the next month did not mark the days before it")
	}
	dpRun(t, ctx, chromedp.Click(`#save`, chromedp.ByID))
	if !pollTrue(ctx, `!!document.getElementById('got') && document.getElementById('got').textContent === 'check_in=2026-11-05&check_out=2026-12-02'`) {
		t.Fatal("the form did not post the range")
	}
}

// On a right-to-left page ArrowLeft is the next day and ArrowRight the
// one before, the way the grid is read; Home and End are the week's
// first and last drawn days, whatever the direction.
func TestE2E_DatePickerKeysFollowTheDirection(t *testing.T) {
	box := DatePicker(DatePickerProps{ID: "rtl", Label: "Date", Name: "d", Month: "2026-11",
		Island: Island{Endpoint: "/island/none", Signal: "rtl-cal"}}, nil)
	b := startBehaviorServer(t, `<div dir="rtl">`+string(box)+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, datepickerLoaded) {
		t.Fatal("the picker's marker never loaded headless-datepicker")
	}
	at := func(want string) bool {
		return pollTrue(ctx, `document.activeElement.getAttribute('data-hui-date') === '`+want+`'`)
	}
	dpRun(t, ctx, chromedp.Focus(`[data-hui-date="2026-11-11"]`, chromedp.ByQuery), dpKey(kb.ArrowLeft))
	if !at("2026-11-12") {
		t.Fatal("ArrowLeft on a right-to-left page did not move to the next day")
	}
	dpRun(t, ctx, dpKey(kb.ArrowRight), dpKey(kb.ArrowRight))
	if !at("2026-11-10") {
		t.Fatal("ArrowRight on a right-to-left page did not move to the day before")
	}
	dpRun(t, ctx, dpKey(kb.Home))
	if !at("2026-11-08") {
		t.Fatal("Home did not move to the week's first day")
	}
	dpRun(t, ctx, dpKey(kb.End))
	if !at("2026-11-14") {
		t.Fatal("End did not move to the week's last day")
	}
}
