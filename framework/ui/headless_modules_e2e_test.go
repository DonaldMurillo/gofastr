package ui_test

// Browser coverage for the headless behaviour modules as framework/ui
// renders them: the real component, the real runtime and modules
// (served by themeToggleTestPage's harness), a click or a signal, and
// the DOM or ARIA outcome a user meets.

import (
	"context"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/widget/preset"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// moduleTestCtx opens the page body renders on and waits for #ready.
// pre runs before the navigation (an emulated media feature, say).
func moduleTestCtx(t *testing.T, body string, pre ...chromedp.Action) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	srv := themeToggleTestPage(t, body)
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
