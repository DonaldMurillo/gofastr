package localentity_test

// Browser tests for the form and list behaviours against a real host,
// one per review finding the unit tests cannot see: an edit that
// clears a field, a checkbox with its own value and a true default,
// lengths in code points, a pattern that fails closed, an edit of a
// record another tab deleted, a form-only tab whose saves reach a
// list tab, and a failed list read that keeps the rows on screen.

import (
	"context"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/localdb"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/localentity"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

func max(n float64) *float64 { return &n }

var (
	e2eDB = localdb.New("localentity-e2e")
	notes = localentity.Define(e2eDB, "notes", []schema.Field{
		{Name: "title", Type: schema.String, Required: true, Max: max(3)},
		{Name: "memo", Type: schema.Text},
		{Name: "starred", Type: schema.Bool},
		{Name: "visible", Type: schema.Bool, Default: true},
		{Name: "code", Type: schema.String, Pattern: `^[A-Z]{2}$`},
		{Name: "due", Type: schema.Date},
		{Name: "at", Type: schema.Timestamp},
	})
)

const noteForm = "note-form"

func form(ctx context.Context) render.HTML {
	return notes.Form(noteForm, ui.Form(ui.FormConfig{Action: "/", Method: "POST", HideSubmit: true, Ctx: ctx},
		ui.TextField(ui.TextFieldConfig{Name: "title", Label: "Title", ID: "f-title"}),
		ui.TextField(ui.TextFieldConfig{Name: "memo", Label: "Memo", ID: "f-memo"}),
		ui.Checkbox(ui.ToggleConfig{Name: "starred", Label: "Starred", ID: "f-starred", Value: "yes"}),
		ui.Checkbox(ui.ToggleConfig{Name: "visible", Label: "Visible", ID: "f-visible", Value: "1", Checked: true}),
		ui.TextField(ui.TextFieldConfig{Name: "code", Label: "Code", ID: "f-code"}),
		ui.TextField(ui.TextFieldConfig{Name: "due", Label: "Due", ID: "f-due"}),
		ui.TextField(ui.TextFieldConfig{Name: "at", Label: "At", ID: "f-at"}),
		ui.Button(ui.ButtonConfig{Label: "Save", Type: "submit"}),
		ui.Button(ui.ButtonConfig{Label: "Clear", Type: "reset"}),
	))
}

type fullScreen struct{ component.ContextOnly }

func (fullScreen) RenderCtx(ctx context.Context) render.HTML {
	l := notes.List(localentity.ListConfig{})
	return ui.Stack(ui.StackConfig{}, form(ctx), notes.Count(), ui.Stack(ui.StackConfig{}, l.Render(
		l.Row(func(r localentity.Row) render.HTML {
			return ui.Card(ui.CardConfig{HeadingContent: r.Text("title")},
				r.Text("memo"),
				r.Edit(noteForm, ui.Button(ui.ButtonConfig{Label: "Edit", Type: "button"})),
				r.Delete(ui.Button(ui.ButtonConfig{Label: "Delete", Type: "button"})))
		}),
		l.Empty(ui.EmptyState(ui.EmptyStateConfig{Title: "Nothing"})),
	)))
}

// partialScreen's form has no control for the Required title.
type partialScreen struct{ component.ContextOnly }

func (partialScreen) RenderCtx(ctx context.Context) render.HTML {
	return notes.Form("partial-form", ui.Form(ui.FormConfig{Action: "/", Method: "POST", HideSubmit: true, Ctx: ctx},
		ui.TextField(ui.TextFieldConfig{Name: "memo", Label: "Memo", ID: "p-memo"}),
		ui.Button(ui.ButtonConfig{Label: "Save", Type: "submit"}),
	))
}

type formOnlyScreen struct{ component.ContextOnly }

func (formOnlyScreen) RenderCtx(ctx context.Context) render.HTML { return form(ctx) }

func startHost(t *testing.T) string {
	t.Helper()
	site := uiapp.NewApp("localentity-e2e")
	site.RegisterScreen(uiapp.NewScreen("/", &fullScreen{}), nil)
	site.RegisterScreen(uiapp.NewScreen("/form", &formOnlyScreen{}), nil)
	site.RegisterScreen(uiapp.NewScreen("/partial", &partialScreen{}), nil)
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "localentity-e2e"}))
	app.Mount(uihost.New(site))
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)
	return srv.URL
}

func await(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams { return p.WithAwaitPromise(true) }

// records reads the store straight from IndexedDB, oldest first.
func records(t *testing.T, ctx context.Context) []map[string]any {
	t.Helper()
	var out []map[string]any
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`__gofastr.localdb.open('localentity-e2e').then((db) => db.list('notes', { index: 'by_created_at' }))`, &out, await)); err != nil {
		t.Fatalf("read records: %v", err)
	}
	return out
}

func poll(js string) chromedp.Action {
	return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(10*time.Second))
}

func submit() chromedp.Action {
	return chromedp.Click(`#`+noteForm+` button[type=submit]`, chromedp.ByQuery)
}

func countIs(n int) chromedp.Action {
	return poll(`(document.querySelector('[data-fui-local-count]') || {}).textContent === '` + strconv.Itoa(n) + `'`)
}

func TestLocalEntityFormsInBrowser(t *testing.T) {
	base := startHost(t)
	tab := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	if err := chromedp.Run(tab, chromedp.Navigate(base+"/"), countIs(0)); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Create: a checkbox whose value is "yes" saves true; a true
	// default whose box stays checked saves true; three emoji fit
	// Max 3 (code points, as core/schema counts).
	if err := chromedp.Run(tab,
		chromedp.SetValue(`#f-title`, "🐉🐉🐉", chromedp.ByQuery),
		chromedp.SetValue(`#f-memo`, "first", chromedp.ByQuery),
		chromedp.Click(`#f-starred`, chromedp.ByQuery),
		submit(), countIs(1),
	); err != nil {
		t.Fatalf("create: %v", err)
	}
	r := records(t, tab)[0]
	if r["title"] != "🐉🐉🐉" || r["starred"] != true || r["visible"] != true || r["memo"] != "first" {
		t.Fatalf("created = %v", r)
	}

	// Edit: empty the optional memo and uncheck both boxes. Each must
	// change: the memo goes away, both bools save false (a true
	// Default never overrides an unchecked box).
	if err := chromedp.Run(tab,
		chromedp.Click(`[data-fui-local-item] [data-fui-local-edit] button`, chromedp.ByQuery),
		poll(`document.querySelector('#f-memo').value === 'first'`),
		chromedp.Evaluate(`document.querySelector('#f-memo').value = ''`, nil),
		chromedp.Click(`#f-starred`, chromedp.ByQuery),
		chromedp.Click(`#f-visible`, chromedp.ByQuery),
		submit(),
		poll(`document.querySelector('#f-title').value === ''`),
	); err != nil {
		t.Fatalf("edit: %v", err)
	}
	all := records(t, tab)
	if len(all) != 1 {
		t.Fatalf("an edit created a record: %v", all)
	}
	if _, has := all[0]["memo"]; has || all[0]["starred"] != false || all[0]["visible"] != false {
		t.Fatalf("after clearing memo and unchecking both boxes: %v", all[0])
	}

	// The embed frame's navigation guard passes forms the runtime
	// marks as script-handled; the module marks its forms so.
	var enctype string
	if err := chromedp.Run(tab, chromedp.Evaluate(`document.querySelector('#`+noteForm+` form').getAttribute('enctype')`, &enctype)); err != nil || enctype != "application/json" {
		t.Fatalf("local form enctype = %q (%v), want application/json", enctype, err)
	}

	// Disabled controls are not "emptied": an edit with memo and the
	// checked visible box disabled keeps both stored values.
	if err := chromedp.Run(tab,
		chromedp.Click(`[data-fui-local-item] [data-fui-local-edit] button`, chromedp.ByQuery),
		poll(`document.querySelector('#f-title').value === '🐉🐉🐉'`),
		chromedp.SetValue(`#f-memo`, "kept", chromedp.ByQuery),
		chromedp.Click(`#f-visible`, chromedp.ByQuery),
		submit(),
		poll(`document.querySelector('#f-title').value === ''`),
		chromedp.Click(`[data-fui-local-item] [data-fui-local-edit] button`, chromedp.ByQuery),
		poll(`document.querySelector('#f-memo').value === 'kept'`),
		chromedp.Evaluate(`document.querySelector('#f-memo').disabled = true; document.querySelector('#f-visible').disabled = true;`, nil),
		chromedp.SetValue(`#f-title`, "abc", chromedp.ByQuery),
		submit(),
		poll(`document.querySelector('#f-title').value === ''`),
		chromedp.Evaluate(`document.querySelector('#f-memo').disabled = false; document.querySelector('#f-visible').disabled = false;`, nil),
	); err != nil {
		t.Fatalf("disabled controls: %v", err)
	}
	if r := records(t, tab)[0]; r["title"] != "abc" || r["memo"] != "kept" || r["visible"] != true {
		t.Fatalf("an edit with disabled controls changed them: %v", r)
	}

	// Dates must be real calendar dates; a datetime-local value is
	// stored as RFC 3339 UTC.
	if err := chromedp.Run(tab,
		chromedp.Evaluate(`document.querySelector('#`+noteForm+` form').noValidate = true`, nil),
		chromedp.SetValue(`#f-title`, "d1", chromedp.ByQuery),
		chromedp.SetValue(`#f-due`, "2024-02-31", chromedp.ByQuery),
		submit(),
		poll(`!!document.querySelector('#f-due[aria-invalid="true"]')`),
		chromedp.SetValue(`#f-due`, "2024-02-29", chromedp.ByQuery),
		chromedp.SetValue(`#f-at`, "2024-05-01T14:30", chromedp.ByQuery),
		submit(), countIs(2),
	); err != nil {
		t.Fatalf("dates: %v", err)
	}
	var stamp map[string]any
	if err := chromedp.Run(tab, chromedp.Evaluate(`__gofastr.localdb.open('localentity-e2e').then((db) => db.list('notes')).then((all) => all.find((r) => r.title === 'd1'))`, &stamp, await)); err != nil {
		t.Fatal(err)
	}
	at, _ := stamp["at"].(string)
	if stamp["due"] != "2024-02-29" || len(at) != len("2024-05-01T14:30:00.000Z") || at[len(at)-1] != 'Z' {
		t.Fatalf("stored due/at = %v / %v, want the date as given and the time as RFC 3339 UTC", stamp["due"], stamp["at"])
	}

	// Two submits before the first save resets the form save once.
	if err := chromedp.Run(tab,
		chromedp.SetValue(`#f-title`, "dup", chromedp.ByQuery),
		chromedp.Evaluate(`(() => { const f = document.querySelector('#`+noteForm+` form'); f.requestSubmit(); f.requestSubmit(); })()`, nil),
		countIs(3),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatalf("double submit: %v", err)
	}
	dups := 0
	for _, r := range records(t, tab) {
		if r["title"] == "dup" {
			dups++
		}
	}
	if dups != 1 {
		t.Fatalf("a double submit saved %d records", dups)
	}
	if err := chromedp.Run(tab, chromedp.Click(`#`+noteForm+` button[type=reset]`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	// Leave one record for the steps below, as they expect.
	if err := chromedp.Run(tab, chromedp.Evaluate(`__gofastr.localdb.open('localentity-e2e').then(async (db) => {
		for (const r of await db.list('notes')) if (r.title !== 'abc') await db.delete('notes', r.id);
	})`, nil, await), countIs(1)); err != nil {
		t.Fatal(err)
	}

	// Pattern: a value the pattern refuses never saves.
	if err := chromedp.Run(tab,
		chromedp.Evaluate(`document.querySelector('#`+noteForm+` form').noValidate = true`, nil),
		chromedp.SetValue(`#f-title`, "ok", chromedp.ByQuery),
		chromedp.SetValue(`#f-code`, "abc", chromedp.ByQuery),
		submit(),
		poll(`!!document.querySelector('#f-code[aria-invalid="true"]')`),
		chromedp.Click(`#`+noteForm+` button[type=reset]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("pattern: %v", err)
	}
	if n := len(records(t, tab)); n != 1 {
		t.Fatalf("a value the pattern refuses was saved: %d records", n)
	}

	// Edit a record, then delete it before saving: the save keeps the
	// visitor's words as a new record instead of failing forever.
	if err := chromedp.Run(tab,
		chromedp.Click(`[data-fui-local-item] [data-fui-local-edit] button`, chromedp.ByQuery),
		poll(`document.querySelector('[data-fui-local-form]').hasAttribute('data-fui-local-editing')`),
		chromedp.Click(`[data-fui-local-item] [data-fui-local-delete] button`, chromedp.ByQuery),
		countIs(0),
		chromedp.SetValue(`#f-title`, "new", chromedp.ByQuery),
		submit(), countIs(1),
	); err != nil {
		t.Fatalf("edit of a deleted record: %v", err)
	}
	var editing bool
	if err := chromedp.Run(tab, chromedp.Evaluate(`document.querySelector('[data-fui-local-form]').hasAttribute('data-fui-local-editing')`, &editing)); err != nil || editing {
		t.Fatalf("the form is still editing a record that no longer exists (%v)", err)
	}
	if got := records(t, tab); len(got) != 1 || got[0]["title"] != "new" {
		t.Fatalf("records = %v", got)
	}

	// A tab that only shows the form (it watches nothing) still
	// reaches this tab's list.
	formTab, cancel := chromedp.NewContext(tab)
	defer cancel()
	if err := chromedp.Run(formTab,
		chromedp.Navigate(base+"/form"),
		chromedp.WaitVisible(`#f-title`, chromedp.ByQuery),
		chromedp.SetValue(`#f-title`, "far", chromedp.ByQuery),
		submit(),
		poll(`document.querySelector('#f-title').value === ''`),
	); err != nil {
		t.Fatalf("form-only tab: %v", err)
	}
	front := func(ctx context.Context) chromedp.Action {
		return chromedp.ActionFunc(func(c context.Context) error {
			return target.ActivateTarget(chromedp.FromContext(ctx).Target.TargetID).Do(c)
		})
	}
	if err := chromedp.Run(tab, front(tab), countIs(2)); err != nil {
		t.Fatalf("the list tab never heard the form-only tab's save: %v", err)
	}

	// A failed read keeps the rows on screen and says so.
	if err := chromedp.Run(tab,
		poll(`document.querySelectorAll('[data-fui-local-item][data-fui-local-key]').length === 2`),
		chromedp.Evaluate(`__gofastr.localdb.open = () => Promise.reject(Object.assign(new Error('x'), { code: 'closed' }));
			new BroadcastChannel('gofastr.localdb.localentity-e2e').postMessage({ v: 1, changes: [{ store: 'notes', op: 'put', key: 'x' }] });`, nil),
		poll(`document.querySelector('[data-fui-local-list]').getAttribute('data-fui-local-state') === 'closed'`),
	); err != nil {
		t.Fatalf("failed read: %v", err)
	}
	var rows int
	var empty bool
	if err := chromedp.Run(tab,
		chromedp.Evaluate(`document.querySelectorAll('[data-fui-local-item][data-fui-local-key]').length`, &rows),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-local-item="empty"]')`, &empty),
	); err != nil {
		t.Fatal(err)
	}
	if rows != 2 || empty {
		t.Fatalf("a failed read changed the list: %d rows, empty state %v", rows, empty)
	}

	// A form with no control for a Required field cannot create.
	partial, cancelP := chromedp.NewContext(tab)
	defer cancelP()
	if err := chromedp.Run(partial,
		chromedp.Navigate(base+"/partial"),
		chromedp.WaitVisible(`#p-memo`, chromedp.ByQuery),
		chromedp.SetValue(`#p-memo`, "orphan", chromedp.ByQuery),
		chromedp.Click(`#partial-form button[type=submit]`, chromedp.ByQuery),
		poll(`document.body.textContent.includes('This field is required. (title)')`),
	); err != nil {
		t.Fatalf("required field without a control: %v", err)
	}
	for _, r := range records(t, partial) {
		if r["memo"] == "orphan" {
			t.Fatalf("a record without its Required title was created: %v", r)
		}
	}
}
