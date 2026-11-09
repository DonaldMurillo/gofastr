package ui_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// stickyFocusPage serves thirty labelled fields under the given frame
// at phone width.
func stickyFocusPage(t *testing.T, frame func(fields render.HTML) render.HTML) context.Context {
	t.Helper()
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	head := "<style>" + th.CSSCustomProperties() + "\n" + css.String() + "</style><style>body{margin:0}</style>"
	var fields strings.Builder
	for i := range 30 {
		fmt.Fprintf(&fields, `<p><label>Field %d <input id="f%d"></label></p>`, i, i)
	}
	srv := themeTestPageWithHead(t, head, string(frame(render.HTML(fields.String()))))
	return moduleTestCtxURL(t, srv.URL, chromedp.EmulateViewport(390, 700))
}

func stickyHeader() render.HTML {
	return render.HTML(`<header style="block-size:var(--size-header-height);background:var(--color-surface)"><a id="home" href="#">App</a></header>`)
}

// shiftTabBehindBars scrolls field 10 to at px from the window's top,
// behind the bars, focuses field 11 without scrolling and presses
// Shift+Tab. It returns field 10's top after the move.
func shiftTabBehindBars(t *testing.T, ctx context.Context, at int) float64 {
	t.Helper()
	var res map[string]float64
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(() => {
			const f = document.getElementById('f10');
			window.scrollTo(0, f.getBoundingClientRect().top + window.scrollY - `+fmt.Sprint(at)+`);
			document.getElementById('f11').focus({preventScroll: true});
			return true;
		})()`, nil),
		chromedp.KeyEvent(kb.Tab, chromedp.KeyModifiers(input.ModifierShift)),
		chromedp.Evaluate(`({top: document.activeElement.getBoundingClientRect().top, idx: Number(document.activeElement.id.slice(1))})`, &res),
	); err != nil {
		t.Fatal(err)
	}
	if res["idx"] != 10 {
		t.Fatalf("focus landed on field %v, want 10", res["idx"])
	}
	return res["top"]
}

func barBottom(t *testing.T, ctx context.Context, sel string) float64 {
	t.Helper()
	var b float64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector(`+"`"+sel+"`"+`).getBoundingClientRect().bottom`, &b)); err != nil {
		t.Fatal(err)
	}
	return b
}

// WCAG 2.4.11: a control Shift+Tab reaches must not stay behind the
// sticky page header or the sticky toolbar under it. The browser
// counted a control behind the bars as in view and did not scroll, so
// on a phone it stayed hidden.
func TestStickyBarsClearFocusedControl(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	t.Run("header and toolbar", func(t *testing.T) {
		ctx := stickyFocusPage(t, func(f render.HTML) render.HTML {
			return ui.Stack(ui.StackConfig{StickyHeader: true}, stickyHeader(),
				ui.ContentRow(ui.ContentRowConfig{Sticky: true, Toolbar: render.HTML(`<button id="tb">Filter</button>`)}, f))
		})
		top := shiftTabBehindBars(t, ctx, 70)
		if bar := barBottom(t, ctx, ".fui-content-row__toolbar"); top < bar {
			t.Errorf("focused field top %.1f is under the sticky toolbar ending at %.1f", top, bar)
		}
	})
	t.Run("toolbar only", func(t *testing.T) {
		ctx := stickyFocusPage(t, func(f render.HTML) render.HTML {
			return ui.ContentRow(ui.ContentRowConfig{Sticky: true, Toolbar: render.HTML(`<button id="tb">Filter</button>`)}, f)
		})
		top := shiftTabBehindBars(t, ctx, 20)
		if bar := barBottom(t, ctx, ".fui-content-row__toolbar"); top < bar {
			t.Errorf("focused field top %.1f is under the sticky toolbar ending at %.1f", top, bar)
		}
	})
	t.Run("header only", func(t *testing.T) {
		ctx := stickyFocusPage(t, func(f render.HTML) render.HTML {
			return ui.Stack(ui.StackConfig{StickyHeader: true}, stickyHeader(), ui.Box(ui.BoxConfig{}, f))
		})
		top := shiftTabBehindBars(t, ctx, 20)
		if bar := barBottom(t, ctx, "header"); top < bar {
			t.Errorf("focused field top %.1f is under the sticky header ending at %.1f", top, bar)
		}
	})
}

// Focusing a control in the sticky header or toolbar of a scrolled
// page leaves the page where it is, though the control carries the
// scroll margin too: the bar already holds it in view.
func TestStickyBarControlsDoNotScroll(t *testing.T) {
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	ctx := stickyFocusPage(t, func(f render.HTML) render.HTML {
		return ui.Stack(ui.StackConfig{StickyHeader: true}, stickyHeader(),
			ui.ContentRow(ui.ContentRowConfig{Sticky: true, Toolbar: render.HTML(`<button id="tb">Filter</button>`)}, f))
	})
	for _, id := range []string{"home", "tb"} {
		var before, after float64
		if err := chromedp.Run(ctx,
			chromedp.Evaluate(`window.scrollTo(0, 600); window.scrollY`, &before),
			chromedp.Evaluate(`document.getElementById('`+id+`').focus(); window.scrollY`, &after),
		); err != nil {
			t.Fatal(err)
		}
		if before != after {
			t.Errorf("focusing #%s scrolled the page from %v to %v", id, before, after)
		}
	}
}
