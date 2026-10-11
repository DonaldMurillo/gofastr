package ui_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// A picker with more or longer labels than its row holds stays inside
// the row on a phone: the track scrolls instead of widening the page,
// and End brings the last option into view.
func TestThemePickerFitsANarrowRow(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var choices []ui.ThemeChoice
	var overrides strings.Builder
	for i, label := range []string{"Brutalist high contrast", "Solarized evening", "Paper", "Midnight ocean", "Forest canopy"} {
		alt := style.DefaultTheme()
		alt.Colors.Primary = style.Color{Name: "primary", Value: fmt.Sprintf("#0%d6a3%d", i, i)}
		ref := style.RegisterThemeOverride(alt)
		overrides.WriteString(style.ThemeOverrideCSS(ref.Hash(), alt))
		choices = append(choices, ui.ThemeChoice{Label: label, Theme: ref})
	}
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := `<meta name="viewport" content="width=device-width, initial-scale=1">` +
		"<style>" + th.CSSCustomProperties() + "\n" + overrides.String() + "\n" + css.String() + "</style>" + colorSchemeScript(t)
	srv := themeTestPageWithHead(t, head, string(ui.ThemePicker(ui.ThemePickerConfig{ID: "tp", Themes: choices})))
	ctx := moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(360, 640), prefersScheme("light"))
	if !pollJS(ctx, moduleLoaded("headless-navigation")) {
		t.Fatal("the picker never loaded headless-navigation")
	}
	shot := func(name string) {
		t.Helper()
		var png []byte
		// Past the kit's transitions, so the picture is the settled state.
		if err := chromedp.Run(ctx, chromedp.Sleep(400*time.Millisecond), chromedp.FullScreenshot(&png, 100)); err != nil {
			t.Fatalf("screenshot: %v", err)
		}
		if err := os.WriteFile(filepath.Join(t.ArtifactDir(), name), png, 0o600); err != nil {
			t.Fatalf("write screenshot: %v", err)
		}
	}

	geom := `(() => {
		const tp = document.getElementById('tp').getBoundingClientRect();
		const el = document.getElementById('tp');
		return [document.documentElement.scrollWidth <= window.innerWidth,
			tp.right <= window.innerWidth,
			el.scrollWidth > el.clientWidth].join(',');
	})()`
	if got := evalString(ctx, geom); got != "true,true,true" {
		t.Fatalf("page fits, picker fits, track scrolls = %s, want true,true,true", got)
	}
	shot("picker-narrow-light.png")
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr_colorScheme.set('dark')`, nil)); err != nil {
		t.Fatal(err)
	}
	shot("picker-narrow-dark.png")

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#tp [aria-checked="true"]').focus()`, nil),
		chromedp.KeyEvent(kb.End),
	); err != nil {
		t.Fatal(err)
	}
	inView := `(() => {
		const tp = document.getElementById('tp').getBoundingClientRect();
		const opts = document.querySelectorAll('#tp [role="radio"]');
		const last = opts[opts.length - 1];
		const r = last.getBoundingClientRect();
		return document.activeElement === last && r.left >= tp.left - 1 && r.right <= tp.right + 1;
	})()`
	if !pollJS(ctx, inView) {
		t.Fatalf("End left the last option out of the track's view")
	}
	// The last theme is light-only, so the page draws it light.
	shot("picker-narrow-end.png")
}
