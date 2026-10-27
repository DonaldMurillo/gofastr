package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// The catalog must show every drawing, with readable controls in both schemes
// and without horizontal scrolling on phones. Exercise the actual site page.
func TestIconCatalogBrowser(t *testing.T) {
	base := siteE2EServer(t)
	browser := axetest.NewBrowser(t)
	for _, width := range []int{1280, 390} {
		for _, scheme := range axetest.Schemes {
			t.Run(fmt.Sprintf("%d/%s", width, scheme), func(t *testing.T) {
				ctx, cancel := axetest.NewTab(t, browser)
				defer cancel()
				var state struct {
					Tiles    int  `json:"tiles"`
					Samples  int  `json:"samples"`
					Overflow bool `json:"overflow"`
				}
				if err := chromedp.Run(ctx,
					page.BringToFront(),
					chromedp.EmulateViewport(int64(width), 844),
					chromedp.Navigate(base+"/components/icon"),
					pageReady(),
					axetest.Prepare(scheme),
					chromedp.Evaluate(`({
                        tiles: document.querySelectorAll('[data-icon-name]').length,
                        samples: document.querySelectorAll('[data-icon-name] svg').length,
                        overflow: document.documentElement.scrollWidth > window.innerWidth
                    })`, &state),
				); err != nil {
					t.Fatal(err)
				}
				if state.Tiles != len(icons.Names()) || state.Samples != len(icons.Names()) || state.Overflow {
					t.Fatalf("incomplete or overflowing icon preview: %+v", state)
				}
				violations, err := axetest.Scan(ctx, scheme, axeAllowlistFor("/components/icon"), axeScanOptsFor("/components/icon")...)
				if err != nil {
					t.Fatal(err)
				}
				for _, v := range violations {
					t.Errorf("%s: %s (%+v)", v.ID, v.Help, v.Nodes)
				}
				var sticky struct {
					Top, Expected, Bottom, Scroll, Height float64
					TwoColumns                            bool
				}
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(`window.scrollTo(0, 1400)`, nil),
					chromedp.Evaluate(`(() => {
						const el = document.getElementById('icon-catalog-header');
						const r = el.getBoundingClientRect();
						const tiles = document.querySelectorAll('[data-icon-name]');
						return {Top:r.top, Expected:parseFloat(getComputedStyle(el).top), Bottom:r.bottom, Scroll:scrollY, Height:r.height,
							TwoColumns:Math.abs(tiles[0].getBoundingClientRect().top - tiles[1].getBoundingClientRect().top) < 2};
					})()`, &sticky),
				); err != nil {
					t.Fatal(err)
				}
				if sticky.Scroll < 500 || sticky.Top < sticky.Expected-2 || sticky.Top > sticky.Expected+2 || sticky.Bottom > 844 {
					t.Fatalf("filters did not stay visible below the site header: %+v", sticky)
				}
				if sticky.Height > 80 || !sticky.TwoColumns {
					t.Fatalf("catalog wastes browsing space: %+v", sticky)
				}
				if dir := os.Getenv("GOFASTR_VISUAL_DIR"); dir != "" {
					var png []byte
					if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
						var err error
						png, err = page.CaptureScreenshot().WithCaptureBeyondViewport(false).Do(ctx)
						return err
					})); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("icons-sticky-%d-%s.png", width, scheme)), png, 0644); err != nil {
						t.Fatal(err)
					}
				}
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(`window.iconNavigationProbe = 'same-document'`, nil),
					chromedp.SetValue(`#icon-filters input[name="q"]`, "pencil", chromedp.ByQuery),
					chromedp.Click(`#icon-filters summary`, chromedp.ByQuery),
					chromedp.SetValue(`#icon-filters select[name="category"]`, "actions", chromedp.ByQuery),
					chromedp.Click(`#icon-filters .fui-filter-toolbar__apply`, chromedp.ByQuery),
				); err != nil {
					t.Fatalf("submit search and category: %v", err)
				}
				if err := chromedp.Run(ctx,
					chromedp.Poll(`document.querySelectorAll('[data-icon-name]').length === 1 && !!document.querySelector('[data-icon-name="edit"]') && new URLSearchParams(location.search).get('category') === 'actions'`, nil),
					chromedp.Evaluate(`(() => { if (window.iconNavigationProbe !== 'same-document') throw new Error('icon search reloaded the page'); })()`, nil),
					chromedp.Reload(), pageReady(),
					chromedp.Poll(`document.querySelectorAll('[data-icon-name]').length === 1 && document.querySelector('#icon-filters input[name="q"]').value === 'pencil' && document.querySelector('#icon-filters select').value === 'actions'`, nil),
					// Capture the clipboard argument so the test does not depend on
					// browser permission prompts or the host's shared clipboard.
					chromedp.Evaluate(`Object.defineProperty(navigator, 'clipboard', {configurable:true, value:{writeText:async value => {window.iconCopiedValue = value}}})`, nil),
					chromedp.Click(`[data-icon-name="edit"] button`, chromedp.ByQuery),
					chromedp.Poll(`window.iconCopiedValue === 'edit' && document.querySelector('[data-icon-name="edit"] [data-hui-copy-state="done"]') !== null`, nil),
				); err != nil {
					t.Fatalf("search/category result or refresh: %v", err)
				}
				if err := chromedp.Run(ctx,
					chromedp.Navigate(base+"/components/icon?q=folder&category=security"), pageReady(),
					chromedp.WaitVisible(`.fui-empty-state`, chromedp.ByQuery),
					chromedp.Click(`#icon-filters summary`, chromedp.ByQuery),
					pageReady(), // wait for the disclosure's viewport shift before clicking its small reset link
					chromedp.Click(`#icon-filters a`, chromedp.ByQuery),
					chromedp.Poll(fmt.Sprintf(`document.querySelectorAll('[data-icon-name]').length === %d && !location.search`, len(icons.Names())), nil),
				); err != nil {
					t.Fatalf("empty results or clear: %v", err)
				}
			})
		}
	}
}

// Progressive enhancement retains the native GET fallback when the runtime
// cannot load. The same query still renders the matching catalog server-side.
func TestIconCatalogNativeGETWithoutRuntime(t *testing.T) {
	base := siteE2EServer(t)
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	if err := chromedp.Run(ctx,
		emulation.SetScriptExecutionDisabled(true),
		chromedp.Navigate(base+"/components/icon"),
		chromedp.WaitVisible(`#icon-filters`, chromedp.ByID),
		chromedp.Click(`#icon-filters summary`, chromedp.ByQuery),
		chromedp.SetValue(`#icon-filters select[name="category"]`, "files", chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := chromedp.RunResponse(ctx, chromedp.Click(`#icon-filters .fui-filter-toolbar__apply`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	var nativeFiles bool
	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(`[data-icon-name="folder"]`, chromedp.ByQuery),
		chromedp.Evaluate(`new URLSearchParams(location.search).get('category') === 'files' && typeof window.__gofastr?.navigate === 'undefined' && [...document.querySelectorAll('[data-icon-name]')].every(el => el.dataset.iconCategory === 'files')`, &nativeFiles),
	); err != nil {
		t.Fatal(err)
	}
	if !nativeFiles {
		t.Fatal("native GET fallback did not show the files category")
	}
}

// At the end of its containing block a sticky toolbar moves above its normal
// offset. The page banner must paint over it rather than behind it.
func TestIconCatalogStickyHeaderBoundary(t *testing.T) {
	base := siteE2EServer(t)
	browser := axetest.NewBrowser(t)
	for _, width := range []int{1280, 390} {
		for _, scheme := range axetest.Schemes {
			t.Run(fmt.Sprintf("%d/%s", width, scheme), func(t *testing.T) {
				ctx, cancel := axetest.NewTab(t, browser)
				defer cancel()
				var state struct {
					Overlaps, HeaderOnTop     bool
					Top, Bottom, HeaderBottom float64
				}
				if err := chromedp.Run(ctx,
					page.BringToFront(), chromedp.EmulateViewport(int64(width), 844),
					chromedp.Navigate(base+"/components/icon?category=files"), pageReady(), axetest.Prepare(scheme),
					chromedp.Evaluate(`(() => {
						const bar = document.getElementById('icon-catalog-header');
						const header = document.querySelector('.fui-stack--sticky-header > header');
						const h = header.getBoundingClientRect();
						window.scrollTo(0, bar.parentElement.getBoundingClientRect().bottom + scrollY - h.bottom / 2);
						const r = bar.getBoundingClientRect();
						const y = (Math.max(0, r.top) + Math.min(h.bottom, r.bottom)) / 2;
						return {Overlaps:r.top < h.bottom && r.bottom > 0,
							HeaderOnTop:header.contains(document.elementFromPoint(r.left + r.width / 2, y)),
							Top:r.top, Bottom:r.bottom, HeaderBottom:h.bottom};
					})()`, &state),
				); err != nil {
					t.Fatal(err)
				}
				if !state.Overlaps || state.Bottom > state.HeaderBottom || !state.HeaderOnTop {
					t.Fatalf("toolbar escaped above the site header at its container boundary: %+v", state)
				}
				if dir := os.Getenv("GOFASTR_VISUAL_DIR"); dir != "" {
					var png []byte
					if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&png)); err != nil {
						t.Fatal(err)
					}
					if err := os.MkdirAll(dir, 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("icon-boundary-%d-%s.png", width, scheme)), png, 0644); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
