package ui_test

// Browser coverage for the headless behaviour modules as framework/ui
// renders them: the real component, the real runtime and modules
// (served by themeToggleTestPage's harness), a click or a signal, and
// the DOM or ARIA outcome a user meets.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// moduleTestCtx opens the page body renders on and waits for #ready.
// pre runs before the navigation (an emulated media feature, say).
func moduleTestCtx(t *testing.T, body string, pre ...chromedp.Action) context.Context {
	t.Helper()
	return moduleTestCtxMux(t, body, nil, pre...)
}

// moduleTestCtxMux is moduleTestCtx with extra handlers on the
// server (the endpoint an island posts to).
func moduleTestCtxMux(t *testing.T, body string, extra func(mux *http.ServeMux), pre ...chromedp.Action) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	var adds []func(mux *http.ServeMux)
	if extra != nil {
		adds = append(adds, extra)
	}
	srv := themeToggleTestPage(t, body, adds...)
	ctx := chromedptest.Context(t)
	actions := append(append([]chromedp.Action{}, pre...),
		chromedp.Navigate(srv.URL),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	)
	if err := chromedp.Run(ctx, actions...); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	return ctx
}

// prefersScheme emulates the OS colour scheme, so a test does not
// depend on the machine's own appearance setting.
func prefersScheme(scheme string) chromedp.Action {
	return emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{
		{Name: "prefers-color-scheme", Value: scheme},
	})
}

// pollJS evaluates a boolean expression until it is true or about
// four seconds pass.
func pollJS(ctx context.Context, js string) bool {
	for range 40 {
		var v bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &v)); err == nil && v {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// evalString reads a string expression, empty on error.
func evalString(ctx context.Context, js string) string {
	var s string
	_ = chromedp.Run(ctx, chromedp.Evaluate(js, &s))
	return s
}

func moduleLoaded(name string) string {
	return `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['` + name + `'])`
}

// A CopyButton with ToastOnCopy shows a toast when clicked: the
// config rides the button, and the feedback module must find it there.
func TestCopyButtonToastOnCopyShowsAToast(t *testing.T) {
	body := `<pre id="snippet">go get gofastr</pre>` +
		string(ui.CopyButton(ui.CopyButtonConfig{Target: "snippet", ToastOnCopy: true, ToastTitle: "Copied it"})) +
		string(preset.ToastSlotHTML(context.Background(), "toasts"))
	ctx := moduleTestCtx(t, body)
	if !pollJS(ctx, moduleLoaded("headless-feedback")) {
		t.Fatal("the copy marker never loaded headless-feedback")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-hui-copy] button').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `(function(){var t=document.querySelector('[data-cui-toast-stack] [data-hui-toast-id] [data-hui-toast-title]');`+
		`return !!t && t.textContent==='Copied it';})()`) {
		t.Fatalf("no toast after a ToastOnCopy click; the stack holds:\n%s",
			evalString(ctx, `(document.querySelector('[data-cui-toast-stack]')||{}).innerHTML||''`))
	}
}

// colorSchemeScript is the bootstrap a uihost page ships in its head:
// the window.__gofastr_colorScheme API the theme toggle drives.
func colorSchemeScript(t *testing.T) string {
	t.Helper()
	js, err := runtime.ColorSchemeJS()
	if err != nil {
		t.Fatal(err)
	}
	return "<script>" + js + "</script>"
}

const schemeState = `document.documentElement.getAttribute('data-color-scheme')+'|'+` +
	`(document.querySelector('meta[name="color-scheme"]')||{content:'<none>'}).content`

// Choosing dark moves the page AND the native controls to dark (the
// color-scheme meta), and choosing auto resolves to the OS scheme:
// data-color-scheme is never the literal "auto", which no
// [data-color-scheme] rule matches.
func TestThemeChoiceSetsSchemeAndMeta(t *testing.T) {
	ctx := moduleTestCtx(t, colorSchemeScript(t)+string(ui.ThemeToggle(ui.ThemeToggleConfig{ID: "tt", Variant: ui.ThemeTogglePill})),
		prefersScheme("light"))
	if !pollJS(ctx, moduleLoaded("headless-navigation")) {
		t.Fatal("the theme toggle never loaded headless-navigation")
	}
	if got := evalString(ctx, schemeState); got != "light|light" {
		t.Fatalf("on a light OS the page booted as %q, want \"light|light\"", got)
	}
	click := func(opt string) {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-hui-theme-option="`+opt+`"]').click()`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	click("dark")
	if got := evalString(ctx, schemeState); got != "dark|dark" {
		t.Errorf("after choosing dark: data-color-scheme|meta = %q, want \"dark|dark\"", got)
	}
	click("auto")
	if got := evalString(ctx, schemeState); got != "light|light" {
		t.Errorf("after choosing auto on a light OS: data-color-scheme|meta = %q, want \"light|light\"", got)
	}
}

// With no bootstrap on the page the module writes the attribute
// itself, and still resolves auto to a real scheme.
func TestThemeAutoWithoutBootstrapResolves(t *testing.T) {
	ctx := moduleTestCtx(t, string(ui.ThemeToggle(ui.ThemeToggleConfig{ID: "tt", Variant: ui.ThemeTogglePill})),
		prefersScheme("light"))
	if !pollJS(ctx, moduleLoaded("headless-navigation")) {
		t.Fatal("the theme toggle never loaded headless-navigation")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-hui-theme-option="dark"]').click();`+
		`document.querySelector('[data-hui-theme-option="auto"]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(ctx, `document.documentElement.getAttribute('data-color-scheme')`); got != "light" {
		t.Errorf("auto without the bootstrap on a light OS: data-color-scheme = %q, want \"light\"", got)
	}
}

// On a dark OS with no stored choice the page is dark, so the cycle
// button's first click must show light: stepping auto to dark would
// leave the page as it was.
func TestThemeCycleFirstClickChangesScheme(t *testing.T) {
	ctx := moduleTestCtx(t, colorSchemeScript(t)+string(ui.ThemeToggle(ui.ThemeToggleConfig{ID: "tt", Variant: ui.ThemeToggleIcon})),
		prefersScheme("dark"))
	if !pollJS(ctx, moduleLoaded("headless-navigation")) {
		t.Fatal("the theme toggle never loaded headless-navigation")
	}
	if got := evalString(ctx, schemeState); got != "dark|dark" {
		t.Fatalf("on a dark OS the page booted as %q, want \"dark|dark\"", got)
	}
	seen := []string{}
	for range 3 {
		if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('tt').click()`, nil)); err != nil {
			t.Fatal(err)
		}
		seen = append(seen, evalString(ctx, schemeState))
	}
	// Every click changes what the page shows.
	prev := "dark|dark"
	for i, s := range seen {
		if s == prev {
			t.Fatalf("click %d left the page at %q (states after each click: %v)", i+1, s, seen)
		}
		prev = s
	}
}

// An island combobox says how many results arrived once the RPC's
// rows land, and says there are none when the swap is empty: the
// "Loading…" the input event wrote does not stay.
func TestIslandComboboxAnnouncesResults(t *testing.T) {
	body := string(ui.Combobox(ui.ComboboxConfig{ID: "q", Name: "q", Label: "Search",
		Island: &headless.Island{Endpoint: "/__test/search", Signal: "search"}, NoScriptAction: "/search", DebounceMs: 50}))
	ctx := moduleTestCtxMux(t, body, func(mux *http.ServeMux) {
		mux.HandleFunc("/__test/search", func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			if strings.Contains(string(b), "zz") {
				return
			}
			fmt.Fprint(w, `<li role="option" id="q-o1">Alpha</li><li role="option" id="q-o2">Alps</li>`)
		})
	})
	if !pollJS(ctx, moduleLoaded("headless-combobox")) {
		t.Fatal("the combobox marker never loaded headless-combobox")
	}
	const status = `document.querySelector('[data-hui-combobox-status]').textContent`
	want := evalString(ctx, `document.getElementById('q-listbox').getAttribute('data-hui-combobox-count').replace('{n}','2')`)
	if err := chromedp.Run(ctx, chromedp.SendKeys(`#q`, "al", chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.querySelectorAll('#q-listbox [role="option"]').length === 2`) {
		t.Fatal("the RPC rows never landed in the listbox")
	}
	if !pollJS(ctx, status+` === '`+want+`'`) {
		t.Fatalf("after two rows landed the status reads %q, want %q", evalString(ctx, status), want)
	}
	none := evalString(ctx, `document.querySelector('[data-hui-combobox-status]').getAttribute('data-hui-combobox-no-results')`)
	if err := chromedp.Run(ctx, chromedp.SendKeys(`#q`, "zz", chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, status+` === '`+none+`'`) {
		t.Fatalf("after an empty swap the status reads %q, want %q", evalString(ctx, status), none)
	}
}

// The bell's spoken count follows its badge: when the unread signal
// changes the number the badge shows, the anchor's accessible name
// says the same number.
func TestBellLabelFollowsUnreadSignal(t *testing.T) {
	trigger, _ := ui.NotificationBell(ui.NotificationBellConfig{Name: "bell", Href: "/notifications",
		Label: "Notifications", UnreadCount: 2, SignalUnread: "unread", ID: "bell"})
	ctx := moduleTestCtx(t, string(trigger))
	if !pollJS(ctx, moduleLoaded("headless-feedback")) {
		t.Fatal("the bell marker never loaded headless-feedback")
	}
	if got := evalString(ctx, `document.getElementById('bell').getAttribute('aria-label')`); got != "2 unread notifications" {
		t.Fatalf("SSR aria-label = %q, want \"2 unread notifications\"", got)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.setSignal('unread', '7')`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.querySelector('#bell [data-hui-notification-count]').textContent === '7'`) {
		t.Fatal("the kernel never wrote 7 into the badge")
	}
	if !pollJS(ctx, `document.getElementById('bell').getAttribute('aria-label') === '7 unread notifications'`) {
		t.Fatalf("after setSignal('unread','7') the bell says %q, want \"7 unread notifications\"",
			evalString(ctx, `document.getElementById('bell').getAttribute('aria-label')`))
	}
	// A badge that is not a whole count ("12x") leaves the spoken
	// number and the count hook alone: parseInt's leading-digits read
	// would announce 12 for a badge that shows something else.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.setSignal('unread', '12x')`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.querySelector('#bell [data-hui-notification-count]').textContent === '12x'`) {
		t.Fatal("the kernel never wrote 12x into the badge")
	}
	if err := chromedp.Run(ctx, chromedp.Sleep(150*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(ctx, `document.getElementById('bell').getAttribute('aria-label')`); got != "7 unread notifications" {
		t.Errorf("a badge of 12x changed the spoken count to %q, want the last whole count kept", got)
	}
	if got := evalString(ctx, `document.querySelector('#bell [data-hui-notification-count]').getAttribute('data-hui-notification-count')`); got != "7" {
		t.Errorf("a badge of 12x set the count hook to %q, want 7 kept", got)
	}
}

// innerTabsState reads the nested strip's aria-selected and tabindex
// per tab, "s0" for selected with tabindex 0 and "-1" otherwise.
const innerTabsState = `[0,1,2].map(function(i){var t=document.getElementById('inner-tab-'+i);` +
	`return (t.getAttribute('aria-selected')==='true'?'s':'u')+t.getAttribute('tabindex');}).join(',')`

// A strip nested in a panel is its own strip: clicking, focusing and
// arrowing the outer tabs leaves the inner tabs' selection and roving
// tabindex alone, and the outer arrows rotate over the outer tabs only.
func TestNestedTabsKeepTheirOwnState(t *testing.T) {
	inner := ui.Tabs(ui.TabsConfig{SignalName: "inner", ID: "inner", Tabs: []ui.TabItem{
		{Label: "One", Content: "1"}, {Label: "Two", Content: "2"}, {Label: "Three", Content: "3"}}})
	outer := ui.Tabs(ui.TabsConfig{SignalName: "outer", ID: "outer", Tabs: []ui.TabItem{
		{Label: "A", Content: inner}, {Label: "B", Content: "b"}, {Label: "C", Content: "c"}}})
	ctx := moduleTestCtx(t, string(outer))
	if !pollJS(ctx, moduleLoaded("headless-tabs")) {
		t.Fatal("the tabs marker never loaded headless-tabs")
	}
	const want = "s0,u-1,u-1"
	if got := evalString(ctx, innerTabsState); got != want {
		t.Fatalf("inner strip at boot = %q, want %q", got, want)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('outer-tab-1').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(ctx, innerTabsState); got != want {
		t.Errorf("after clicking outer tab B the inner strip reads %q, want %q", got, want)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){var t=document.getElementById('outer-tab-0'); t.focus();`+
		`t.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowLeft',bubbles:true,cancelable:true}));})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(ctx, `document.activeElement.id`); got != "outer-tab-2" {
		t.Errorf("ArrowLeft from the first outer tab focused %q, want the last outer tab \"outer-tab-2\"", got)
	}
	if got := evalString(ctx, innerTabsState); got != want {
		t.Errorf("after arrowing the outer strip the inner strip reads %q, want %q", got, want)
	}
}

// A selection that arrives through the signal (no click) moves the
// roving tabindex and aria-selected with it, with StateAttrs off: the
// tab of the shown panel is the one Tab reaches and the one selected.
func TestTabsSignalMovesRovingTabindex(t *testing.T) {
	strip := ui.Tabs(ui.TabsConfig{SignalName: "tb", ID: "tb", Tabs: []ui.TabItem{
		{Label: "A", Content: "a"}, {Label: "B", Content: "b"}, {Label: "C", Content: "c"}}})
	ctx := moduleTestCtx(t, string(strip))
	if !pollJS(ctx, moduleLoaded("headless-tabs")) {
		t.Fatal("the tabs marker never loaded headless-tabs")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.setSignal('tb', '2')`, nil)); err != nil {
		t.Fatal(err)
	}
	const state = `[0,1,2].map(function(i){var t=document.getElementById('tb-tab-'+i);` +
		`return (t.getAttribute('aria-selected')==='true'?'s':'u')+t.getAttribute('tabindex');}).join(',')`
	if !pollJS(ctx, `document.querySelector('[data-hui-tabs]').getAttribute('data-active') === '2'`) {
		t.Fatal("setSignal('tb','2') never reached data-active")
	}
	const want = "u-1,u-1,s0"
	if !pollJS(ctx, state+` === '`+want+`'`) {
		t.Fatalf("after setSignal('tb','2') the strip reads %q, want %q", evalString(ctx, state), want)
	}
}

// toastProbeStatus is a status variant an app registers: a runtime
// toast carrying it wears its class and glyph, the way a server
// Notification does.
var toastProbeStatus = ui.RegisterStatusVariant("toastprobe", ui.StatusVariantCSS{Color: "{colors.primary}", Icon: "γ"})

// A runtime toast keeps its own variant: neutral and a registered
// variant wear their class and glyph and say no tone word, and the
// kernel's 'error' is drawn and announced as danger.
func TestRuntimeToastKeepsItsVariant(t *testing.T) {
	ctx := moduleTestCtx(t, string(preset.ToastSlotHTML(context.Background(), "toasts")))
	if !pollJS(ctx, moduleLoaded("headless-feedback")) {
		t.Fatal("the stack marker never loaded headless-feedback")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){var T=window.__gofastr.toast;`+
		`T({variant:'neutral', title:'N'}); T({variant:'error', title:'E'}); T({variant:'`+string(toastProbeStatus)+`', title:'G'});})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.querySelectorAll('[data-cui-toast-stack] [data-hui-toast-id]').length === 3`) {
		t.Fatal("three runtime toasts did not render")
	}
	row := func(title string) string {
		return `(function(){var r=[].slice.call(document.querySelectorAll('[data-cui-toast-stack] [data-hui-toast]'))` +
			`.filter(function(x){return x.querySelector('[data-hui-toast-title]').textContent==='` + title + `'})[0];` +
			`var tw=r.querySelector('[data-hui-toast-tone]'), ic=r.querySelector('[data-hui-toast-icon]');` +
			`return r.className+'|'+(tw?tw.textContent:'<none>')+'|'+(ic?ic.textContent:'<none>')+'|'+r.getAttribute('role');})()`
	}
	for _, c := range []struct{ title, want string }{
		{"N", "fui-notification fui-notification--neutral|<none>|•|status"},
		{"E", "fui-notification fui-notification--danger|Error: |✕|alert"},
		{"G", "fui-notification fui-notification--toastprobe|<none>|γ|status"},
	} {
		if got := evalString(ctx, row(c.title)); got != c.want {
			t.Errorf("toast %s: class|tone word|glyph|role = %q, want %q", c.title, got, c.want)
		}
	}
}

// A MultiSelect that arrives as the root node of an insertion (an
// island swap, an appended fragment) is wired by the kernel's
// arrival pass: its pre-selected options render as chips.
func TestInsertedMultiSelectRendersChips(t *testing.T) {
	first := ui.MultiSelect(ui.MultiSelectConfig{Name: "a", Label: "A", ID: "ms-a",
		Options: []ui.MultiSelectOption{{Value: "x", Label: "X"}}})
	later := ui.MultiSelect(ui.MultiSelectConfig{Name: "b", Label: "B", ID: "ms-b",
		Options: []ui.MultiSelectOption{{Value: "red", Label: "Red", Selected: true},
			{Value: "blue", Label: "Blue", Selected: true}, {Value: "green", Label: "Green"}}})
	ctx := moduleTestCtx(t, string(first)+`<template id="later">`+string(later)+`</template>`)
	if !pollJS(ctx, moduleLoaded("headless-multiselect")) {
		t.Fatal("the multiselect marker never loaded headless-multiselect")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body.appendChild(`+
		`document.getElementById('later').content.firstElementChild.cloneNode(true))`, nil)); err != nil {
		t.Fatal(err)
	}
	const chips = `document.querySelectorAll('#ms-b [data-hui-multiselect-chip-text]')`
	if !pollJS(ctx, chips+`.length === 2`) {
		t.Fatalf("the inserted MultiSelect shows %s chips, want 2 (Red, Blue)",
			evalString(ctx, `String(`+chips+`.length)`))
	}
	if got := evalString(ctx, `Array.from(`+chips+`).map(function(c){return c.textContent}).join(',')`); got != "Red,Blue" {
		t.Errorf("chips = %q, want \"Red,Blue\"", got)
	}
}
