//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// Hard rule 9: dialog bounding is layout — a DOM dump cannot see that a
// tall command list pushed the palette's input past the top of a phone
// screen. These tests boot the REAL widget runtime (router-mounted
// palette + runtime.js, exactly what a host serves), open it at mobile
// and pathologically-short desktop viewports, and measure geometry.

// paletteTestServer mounts an 80-command palette on a real router
// plus the widget runtime, serving everything a page needs to boot:
// the runtime script, the widget chrome/style endpoints, and the
// per-component CSS the runtime lazily fetches. 80 rows: at headless
// Chrome's ~18px/row this stacks ~1.4k px of list — comfortably taller
// than the 844px viewport, so the bound is actually exercised (the
// anti-vacuity check enforces it).
func paletteTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	commands := make([]PaletteCommand, 80)
	for i := range commands {
		commands[i] = PaletteCommand{
			Label: fmt.Sprintf("Command %03d — jump to the thing", i+1),
			Href:  fmt.Sprintf("/cmd/%03d", i+1),
			Meta:  fmt.Sprintf("/cmd/%03d", i+1),
		}
	}
	_, pal := CommandPalette(CommandPaletteConfig{Commands: commands})

	r := router.New()
	widget.MountBuilder(r, pal)
	widget.MountRuntime(r)

	th := theme.Default()
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasPrefix(req.URL.Path, "/__gofastr/comp/"):
			// Component CSS endpoint (registry.RegisterStyle sheets).
			name := strings.TrimSuffix(path.Base(req.URL.Path), ".css")
			e, ok := registry.Lookup(name)
			if !ok {
				http.NotFound(w, req)
				return
			}
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
			fmt.Fprint(w, e.CSSFor(th))
		case req.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><meta charset=utf-8>
<meta name=viewport content="width=device-width, initial-scale=1">
<style>`+th.CSSCustomProperties()+`
body{margin:0}
.ui-visually-hidden{position:absolute !important;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap;border:0}
</style>
<script type="application/json" id="gofastr-catalog">
{"ui-cmd-palette":{"stylePath":"/__gofastr/comp/ui-cmd-palette.css","version":"test","loadMode":"auto"},
 "combobox":{"stylePath":"/__gofastr/comp/combobox.css","version":"test","loadMode":"auto"}}
</script>
<button id="open" type="button" data-fui-open="command-palette">Open palette</button>
`+widget.RuntimeTag())
		default:
			r.ServeHTTP(w, req)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// paletteChromeCtx boots headless Chrome for the given viewport. The
// emulation override (not WindowSize, which headless does not honor)
// pins the layout viewport so the 540px media query and dvh units
// resolve against real pixels.
func paletteChromeCtx(t *testing.T, w, h int64) context.Context {
	t.Helper()
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], // A cold Chrome start on a shared runner can take longer than
			// chromedp's 20s default wait for the DevTools websocket URL, which
			// surfaces as "websocket url timeout reached" and looks like a test
			// failure rather than a slow launch. The cmd/gofastr chromedp tests
			// already raise it to 90s; these did not.
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox)...)
	t.Cleanup(cancelAlloc)
	ctx, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(cancelTimeout)
	return ctx
}

// openPalette navigates, pins the viewport, opens the palette via the
// trigger, and waits for the widget + combobox runtime to settle: the
// combobox module loaded and the static listbox open.
func openPalette(t *testing.T, ctx context.Context, url string, vw, vh int64) {
	t.Helper()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.EmulateViewport(vw, vh),
		chromedp.WaitVisible(`#open`, chromedp.ByID),
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.WaitVisible(`[data-fui-comp="ui-cmd-palette"]`),
	); err != nil {
		t.Fatalf("open palette: %v", err)
	}
	// The combobox module is demand-loaded when the runtime scans the
	// freshly inserted widget; wait for it before driving the input.
	var comboLoaded bool
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.combobox)`, &comboLoaded,
			chromedp.WithPollingTimeout(15*time.Second), chromedp.WithPollingInterval(100*1e6)),
	); err != nil {
		var loaded string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify((window.__gofastr&&window.__gofastr.loadedModules)||{})`, &loaded))
		t.Fatalf("combobox module never loaded (loaded=%s): %v", loaded, err)
	}
	// The listbox opens on focusin / ArrowDown / input — NOT on a
	// plain click, and the input was already focused by mountWidget
	// before the module bound its listeners (no focusin will fire).
	// Drive the real keyboard open path instead.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('command-palette-input').focus()`, nil),
		chromedp.KeyEvent(kb.ArrowDown),
		chromedp.WaitVisible(`#command-palette-input-listbox`, chromedp.ByID),
		chromedp.Sleep(250*time.Millisecond),
	); err != nil {
		t.Fatalf("open listbox: %v", err)
	}
}

// TestCommandPaletteBoundedDialogChromium renders an 80-command palette
// whose untruncated list is several viewports tall and asserts the
// dialog stays inside the viewport with its input and footer visible
// and the list scrolling inside the remaining space. The 1280x240
// subtest engages the desktop max-block-size guard: with the listbox
// capped at 50vh=120px, form+list+footer exceed the 208px available, so
// only the dialog-level cap can keep the palette on screen.
func TestCommandPaletteBoundedDialogChromium(t *testing.T) {
	srv := paletteTestServer(t)
	for _, vp := range []struct {
		name         string
		w, h         int64
		wantShot     bool
		heightBudget float64 // max palette height: vh, or vh - 2*spacing-lg when centered
	}{
		{name: "mobile_390x844", w: 390, h: 844, wantShot: true, heightBudget: 844},           // full-bleed: exactly 100dvh
		{name: "desktop_1280x800", w: 1280, h: 800, wantShot: true, heightBudget: 800 - 2*16}, // centered: vh - 2*spacing-lg
		{name: "desktop_short_1280x240", w: 1280, h: 240, heightBudget: 240 - 2*16},           // centered: vh - 2*spacing-lg
	} {
		t.Run(vp.name, func(t *testing.T) {
			ctx := paletteChromeCtx(t, vp.w, vp.h)
			openPalette(t, ctx, srv.URL, vp.w, vp.h)

			var m struct {
				InnerHeight    int64   `json:"innerHeight"`
				PaletteTop     float64 `json:"paletteTop"`
				PaletteBottom  float64 `json:"paletteBottom"`
				PaletteHeight  float64 `json:"paletteHeight"`
				InputTop       float64 `json:"inputTop"`
				InputBottom    float64 `json:"inputBottom"`
				FooterBottom   float64 `json:"footerBottom"`
				ListboxScrollH int64   `json:"listboxScrollH"`
				ListboxClientH int64   `json:"listboxClientH"`
				Options        int64   `json:"options"`
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				const pal = document.querySelector('[data-fui-comp="ui-cmd-palette"]');
				const r = pal.getBoundingClientRect();
				const input = document.getElementById('command-palette-input').getBoundingClientRect();
				const footer = pal.querySelector('.ui-cmd-palette__footer').getBoundingClientRect();
				const list = document.getElementById('command-palette-input-listbox');
				return {innerHeight: window.innerHeight,
					paletteTop: r.top, paletteBottom: r.bottom, paletteHeight: r.height,
					inputTop: input.top, inputBottom: input.bottom,
					footerBottom: footer.bottom,
					listboxScrollH: list.scrollHeight, listboxClientH: list.clientHeight,
					options: list.querySelectorAll('[role="option"]').length};
			})()`, &m)); err != nil {
				t.Fatal(err)
			}

			// Anti-vacuity: the fixture must actually stress the guard.
			if m.Options < 80 {
				t.Fatalf("expected >= 80 options, saw %d", m.Options)
			}
			if m.ListboxScrollH <= m.InnerHeight {
				t.Fatalf("fixture too weak: listbox scrollHeight %d <= viewport %d; the list is not taller than the screen", m.ListboxScrollH, m.InnerHeight)
			}

			if m.PaletteTop < -0.5 || m.PaletteBottom > float64(m.InnerHeight)+0.5 {
				t.Errorf("palette escapes viewport: top %.1f bottom %.1f (vh %d)", m.PaletteTop, m.PaletteBottom, m.InnerHeight)
			}
			if m.PaletteHeight > vp.heightBudget+1 {
				t.Errorf("palette height %.1f exceeds budget %.0f", m.PaletteHeight, vp.heightBudget)
			}
			if m.InputTop < -0.5 || m.InputBottom > float64(m.InnerHeight)+0.5 {
				t.Errorf("input not fully visible: top %.1f bottom %.1f (vh %d) — the input the user must type into is clipped", m.InputTop, m.InputBottom, m.InnerHeight)
			}
			if m.FooterBottom > float64(m.InnerHeight)+0.5 {
				t.Errorf("footer pushed off-screen: bottom %.1f (vh %d)", m.FooterBottom, m.InnerHeight)
			}
			if m.ListboxClientH <= 0 {
				t.Errorf("listbox collapsed to %dpx — the flex column starved the scrolling region", m.ListboxClientH)
			}

			// Scroll-reachability: bounds alone don't make the tail of an
			// 80-command list usable — the listbox must be the scrolling
			// region. Jump to the bottom and require the LAST painted
			// option to land inside the listbox's visible box. If
			// overflow-y is lost (content pushing the dialog instead),
			// scrollTop stays 0 and the last row sits far below the fold.
			var sr struct {
				ScrollTop  float64 `json:"scrollTop"`
				LastTop    float64 `json:"lastTop"`
				LastBottom float64 `json:"lastBottom"`
				ListTop    float64 `json:"listTop"`
				ListBottom float64 `json:"listBottom"`
				LastLabel  string  `json:"lastLabel"`
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				const list = document.getElementById('command-palette-input-listbox');
				list.scrollTop = list.scrollHeight;
				const lb = list.getBoundingClientRect();
				const opts = [...list.querySelectorAll('[role="option"]')].filter(o => o.getClientRects().length > 0);
				const last = opts[opts.length - 1];
				const r = last.getBoundingClientRect();
				return {scrollTop: list.scrollTop, lastTop: r.top, lastBottom: r.bottom,
					listTop: lb.top, listBottom: lb.bottom, lastLabel: last.textContent.trim()};
			})()`, &sr)); err != nil {
				t.Fatal(err)
			}
			if sr.ScrollTop <= 0 {
				t.Errorf("listbox did not scroll (scrollTop=%.1f after jump-to-bottom) — the 80-command tail is unreachable", sr.ScrollTop)
			}
			if sr.LastBottom > sr.ListBottom+0.5 || sr.LastTop < sr.ListTop-0.5 {
				t.Errorf("last command %q not scroll-reachable: option box [%.1f,%.1f] outside listbox [%.1f,%.1f]",
					sr.LastLabel, sr.LastTop, sr.LastBottom, sr.ListTop, sr.ListBottom)
			}

			if vp.wantShot {
				var shot []byte
				if err := chromedp.Run(ctx, chromedp.FullScreenshot(&shot, 100)); err != nil {
					t.Fatal(err)
				}
				if len(shot) == 0 {
					t.Fatal("empty screenshot")
				}
				out := fmt.Sprintf("/tmp/gofastr-palette-%s.png", vp.name)
				if err := os.WriteFile(out, shot, 0o644); err != nil {
					t.Fatalf("save screenshot: %v", err)
				}
				t.Logf("screenshot: %s", out)
			}
		})
	}
}

// TestCommandPaletteCloseBehaviorChromium drives the visible close
// control end-to-end through the real widget runtime: it renders with
// an accessible name, dismisses the palette, returns focus to the
// trigger, keeps the focus trap cycling with the new control in the
// DOM, and the palette reopens afterwards.
func TestCommandPaletteCloseBehaviorChromium(t *testing.T) {
	srv := paletteTestServer(t)
	ctx := paletteChromeCtx(t, 390, 844)
	openPalette(t, ctx, srv.URL, 390, 844)

	var aria struct {
		Label        string `json:"label"`
		Type         string `json:"type"`
		Visible      bool   `json:"visible"`
		InputFocused bool   `json:"inputFocused"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const btn = document.querySelector('.ui-cmd-palette__close');
		return {label: btn.getAttribute('aria-label') || '',
			type: btn.getAttribute('type') || '',
			visible: btn.offsetParent !== null,
			inputFocused: document.activeElement === document.getElementById('command-palette-input')};
	})()`, &aria)); err != nil {
		t.Fatal(err)
	}
	if aria.Label == "" {
		t.Error("close button has no accessible name")
	}
	if aria.Type != "button" {
		t.Errorf("close button type = %q, want button", aria.Type)
	}
	if !aria.Visible {
		t.Error("close button is not rendered visibly")
	}
	if !aria.InputFocused {
		t.Error("initial focus did not land on the input")
	}

	// Focus trap with the new control in the DOM: the palette's
	// tabbables are [input, close]. Tab from the input must land on
	// the close button (it is IN the focus order); Tab from close must
	// wrap back to the input (the trap still cycles and close is the
	// last node). A button outside the order, or a trap broken by the
	// new control, fails one of the two.
	var afterTab1, afterTab2 string
	if err := chromedp.Run(ctx,
		chromedp.KeyEvent(kb.Tab),
		chromedp.Sleep(50*time.Millisecond),
		chromedp.Evaluate(`document.activeElement.className`, &afterTab1),
		chromedp.KeyEvent(kb.Tab),
		chromedp.Sleep(50*time.Millisecond),
		chromedp.Evaluate(`document.activeElement.id`, &afterTab2),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(afterTab1, "ui-cmd-palette__close") {
		t.Errorf("Tab from input landed on %q, want the close button — the control is not in the focus order", afterTab1)
	}
	if afterTab2 != "command-palette-input" {
		t.Errorf("Tab from close landed on %q, want wrap to the input — the focus trap no longer cycles", afterTab2)
	}

	// Dismiss via the close control, then check focus restore + reopen.
	var closed, focusBack bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`.ui-cmd-palette__close`),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const w = document.querySelector('[data-fui-widget="command-palette"]');
			return !w || w.hidden || getComputedStyle(w).display === 'none';
		})()`, &closed),
		chromedp.Evaluate(`document.activeElement === document.getElementById('open')`, &focusBack),
	); err != nil {
		t.Fatal(err)
	}
	if !closed {
		t.Error("clicking the close button did not dismiss the palette")
	}
	if !focusBack {
		t.Error("focus was not restored to the opening trigger after close")
	}

	// Reopen: dismissal must not have destroyed the widget.
	var reopened bool
	if err := chromedp.Run(ctx,
		chromedp.Click(`#open`, chromedp.ByID),
		chromedp.WaitVisible(`[data-fui-comp="ui-cmd-palette"]`),
		chromedp.Sleep(250*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const pal = document.querySelector('[data-fui-comp="ui-cmd-palette"]');
			return !!pal && pal.offsetParent !== null;
		})()`, &reopened),
	); err != nil {
		t.Fatal(err)
	}
	if !reopened {
		t.Error("palette did not reopen after close-button dismissal")
	}

	// Escape still closes (preserved affordance).
	var escClosed bool
	if err := chromedp.Run(ctx,
		chromedp.KeyEvent(kb.Escape),
		chromedp.Sleep(300*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const w = document.querySelector('[data-fui-widget="command-palette"]');
			return !w || w.hidden || getComputedStyle(w).display === 'none';
		})()`, &escClosed),
	); err != nil {
		t.Fatal(err)
	}
	if !escClosed {
		t.Error("Escape no longer dismisses the palette")
	}
}

// TestCommandPaletteStaticFilteringPaintsOnlyMatchesChromium guards the
// palette's primary interaction: typing filters the static command list.
// The assertion counts PAINTED rows (client rects), never `hidden`
// attributes. combobox.js sets opt.hidden correctly, but the combobox
// stylesheet's `[role="option"] { display: block }` is author-origin and
// beats the UA `[hidden] { display: none }` regardless of specificity —
// an attribute-level assertion passes while every row stays painted
// (#337). The same sheet re-sets display:flex on options under
// @media (pointer: coarse), so the test re-measures under emulated
// coarse pointer: the [hidden] guard must hold in both environments.
func TestCommandPaletteStaticFilteringPaintsOnlyMatchesChromium(t *testing.T) {
	srv := paletteTestServer(t)
	ctx := paletteChromeCtx(t, 390, 844)
	openPalette(t, ctx, srv.URL, 390, 844)

	// Harness guard: the combobox stylesheet named by the catalog must
	// actually load. It 404'd for this harness's whole life while the
	// catalog named /__gofastr/combobox.css (only /__gofastr/comp/<n>.css
	// is served), and every palette test ran with zero combobox CSS —
	// invisible to every existing assertion.
	var sheetRules int64
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const link = document.querySelector('link[data-fui-style="combobox"]');
		return (link && link.sheet) ? link.sheet.cssRules.length : -1;
	})()`, &sheetRules)); err != nil {
		t.Fatal(err)
	}
	if sheetRules <= 0 {
		t.Fatalf("combobox stylesheet not applied (cssRules=%d) — the harness catalog points at an unserved URL; combobox-style assertions are meaningless without it", sheetRules)
	}

	// "command 05" matches Command 050–059: 10 of 80 options.
	if err := chromedp.Run(ctx,
		chromedp.SendKeys(`#command-palette-input`, "command 05", chromedp.ByID),
		chromedp.Sleep(300*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}

	measure := `(() => {
		const list = document.getElementById('command-palette-input-listbox');
		const opts = [...list.querySelectorAll('[role="option"]')];
		let hiddenAttr = 0, painted = 0, firstPainted = '', lastPainted = '';
		for (const o of opts) {
			if (o.hidden) hiddenAttr++;
			if (o.getClientRects().length > 0) {
				painted++;
				if (!firstPainted) firstPainted = o.textContent.trim();
				lastPainted = o.textContent.trim();
			}
		}
		return {total: opts.length, hiddenAttr, painted, firstPainted, lastPainted};
	})()`
	var m struct {
		Total        int64  `json:"total"`
		HiddenAttr   int64  `json:"hiddenAttr"`
		Painted      int64  `json:"painted"`
		FirstPainted string `json:"firstPainted"`
		LastPainted  string `json:"lastPainted"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(measure, &m)); err != nil {
		t.Fatal(err)
	}
	t.Logf("filter \"command 05\": total=%d hiddenAttr=%d painted=%d first=%q last=%q",
		m.Total, m.HiddenAttr, m.Painted, m.FirstPainted, m.LastPainted)

	shot := func(name string) {
		var png []byte
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&png, 100)); err != nil {
			t.Fatal(err)
		}
		out := "/tmp/gofastr-palette-filter-" + name + ".png"
		if err := os.WriteFile(out, png, 0o644); err != nil {
			t.Fatalf("save screenshot: %v", err)
		}
		t.Logf("screenshot: %s", out)
	}
	shot("fine") // saved before assertions so a red run still records the render

	if m.Total != 80 {
		t.Fatalf("fixture changed: %d options, want 80", m.Total)
	}
	if m.HiddenAttr != 70 {
		t.Errorf("expected 70 options flagged hidden by the runtime, got %d", m.HiddenAttr)
	}
	if m.Painted != 10 {
		t.Errorf("typing \"command 05\" must leave 10 painted rows, got %d — the [hidden] attribute is set but an author display rule is beating the UA default (#337)", m.Painted)
	}
	if m.Painted == 10 {
		if !strings.HasPrefix(m.FirstPainted, "Command 050") || !strings.HasPrefix(m.LastPainted, "Command 059") {
			t.Errorf("painted set is wrong: first=%q last=%q, want Command 050…Command 059", m.FirstPainted, m.LastPainted)
		}
	}

	// Coarse pointer re-check: the combobox sheet sets display:flex on
	// options inside @media (pointer: coarse). If the [hidden] guard
	// doesn't out-specify THAT rule too, phones filter nothing while
	// desktop works.
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			return emulation.SetEmulatedMedia().
				WithFeatures([]*emulation.MediaFeature{{Name: "pointer", Value: "coarse"}}).
				Do(ctx)
		}),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(measure, &m)); err != nil {
		t.Fatal(err)
	}
	shot("coarse")
	if m.Painted != 10 {
		t.Errorf("coarse pointer (touch) must also leave 10 painted rows, got %d — the @media (pointer: coarse) display:flex rule beats the [hidden] guard", m.Painted)
	}
}
