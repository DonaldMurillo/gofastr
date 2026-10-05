package ui_test

// Browser coverage for ui.ThemePicker: the page carries the canonical
// :root theme and one registered override, the picker switches the
// override class on <html>, the colour-scheme bootstrap re-applies the
// stored choice before the body parses, and the override's dark
// palette follows the document's scheme. Same harness as the theme
// toggle tests, with the stylesheet and the bootstrap in <head> where
// uihost ships them.

import (
	"strconv"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

// pickerState reads class|primary|checked: the theme class on <html>,
// the --color-primary the document resolves, and the pick value of the
// checked option ("<none>" when no option is checked).
const pickerState = `(() => {
	const cls = [...document.documentElement.classList].filter(c => c.startsWith('cui-theme-')).join(' ');
	const primary = getComputedStyle(document.documentElement).getPropertyValue('--color-primary').trim().toLowerCase();
	const on = document.querySelector('[data-hui-theme-pick][aria-checked="true"]');
	return cls + '|' + primary + '|' + (on ? on.getAttribute('data-hui-theme-pick') : '<none>');
})()`

func TestThemePickerSwitchesThePage(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	base := style.DefaultTheme()
	base.Colors.Primary = style.Color{Name: "primary", Value: "#111111"}
	base.DarkColors = map[string]string{"primary": "#eeeeee"}
	alt := style.DefaultTheme()
	alt.Colors.Primary = style.Color{Name: "primary", Value: "#00aa00"}
	alt.DarkColors = map[string]string{"primary": "#aaffaa"}
	ref := style.RegisterThemeOverride(alt)
	cls := ref.Class()
	// A light-only override: in dark mode it must stay light, as a
	// wrapped subtree does, not fall to the canonical dark palette.
	lite := style.DefaultTheme()
	lite.Colors.Primary = style.Color{Name: "primary", Value: "#0000aa"}
	liteRef := style.RegisterThemeOverride(lite)
	liteCls := liteRef.Class()

	head := "<style>" + base.CSSCustomProperties() + "\n" + style.ThemeOverrideCSS(ref.Hash(), alt) + "\n" +
		style.ThemeOverrideCSS(liteRef.Hash(), lite) + "</style>" +
		colorSchemeScript(t)
	// Runs while the body parses, before runtime.js and any module: what
	// it records is what the first paint drew with.
	body := `<script>window.__boot = document.documentElement.className;</script>` +
		string(ui.ThemePicker(ui.ThemePickerConfig{ID: "tp", Themes: []ui.ThemeChoice{{Label: "Alt", Theme: ref}, {Label: "Lite", Theme: liteRef}}}))
	srv := themeTestPageWithHead(t, head, body)

	ctx := moduleTestCtxURL(t, srv.URL, prefersScheme("light"))
	if !pollJS(ctx, moduleLoaded("headless-navigation")) {
		t.Fatal("the picker never loaded headless-navigation")
	}
	want := func(step, state string) {
		t.Helper()
		if !pollJS(ctx, pickerState+` === `+strconv.Quote(state)) {
			t.Fatalf("%s: class|primary|checked = %q, want %q", step, evalString(ctx, pickerState), state)
		}
	}
	run := func(js string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, nil)); err != nil {
			t.Fatal(err)
		}
	}
	reload := func() {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Reload(), chromedp.WaitVisible(`#ready`, chromedp.ByID)); err != nil {
			t.Fatal(err)
		}
	}

	want("fresh profile", "|#111111|")
	run(`document.querySelector('[data-hui-theme-pick="` + cls + `"]').click()`)
	want("after picking Alt", cls+"|#00aa00|"+cls)

	// The override's dark palette wins over the canonical dark block on
	// the same element.
	run(`window.__gofastr_colorScheme.set('dark')`)
	want("Alt in dark mode", cls+"|#aaffaa|"+cls)

	reload()
	if got := evalString(ctx, `window.__boot`); got != cls {
		t.Errorf("before any module ran, <html> carried class %q, want %q: the bootstrap must apply the stored theme before first paint", got, cls)
	}
	want("Alt after reload", cls+"|#aaffaa|"+cls)

	run(`document.querySelector('[data-hui-theme-pick="` + liteCls + `"]').click()`)
	want("light-only Lite in dark mode", liteCls+"|#0000aa|"+liteCls)

	run(`document.querySelector('[data-hui-theme-pick=""]').click()`)
	want("back to Default", "|#eeeeee|")
	if got := evalString(ctx, `String(localStorage.getItem('gofastr.theme'))`); got != "null" {
		t.Errorf("Default left %q in storage, want the key removed", got)
	}

	// A stored value that is not one theme class never reaches classList.
	run(`localStorage.setItem('gofastr.theme', 'evil')`)
	reload()
	if got := evalString(ctx, `document.documentElement.className`); got != "" {
		t.Errorf("a malformed stored theme set <html class=%q>, want none", got)
	}
}
