package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// The scrollHeight fallback: the engine is told it cannot size a field
// to its content, so the module measures.
func TestHiddenTextareaGrowsOnReveal(t *testing.T) {
	for _, container := range []string{"section", "details"} {
		t.Run(container, func(t *testing.T) {
			attributes := ` hidden`
			if container == "details" {
				attributes = ""
			}
			markup := `<` + container + ` id="container"` + attributes + `><summary>Show</summary><textarea id="draft" data-cui-autogrow>` + strings.Repeat("prefilled text\n", 12) + `</textarea></` + container + `>`
			server := widgetHelpersServer(t, markup)
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx,
				chromedp.ActionFunc(func(ctx context.Context) error {
					_, err := page.AddScriptToEvaluateOnNewDocument(`
const nativeSupports = CSS.supports.bind(CSS);
CSS.supports = function (a, b) { return String(a).includes('field-sizing') ? false : nativeSupports(...arguments); };
window.__observedTextareas = new Set();
const NativeResizeObserver = window.ResizeObserver;
window.ResizeObserver = class extends NativeResizeObserver {
  observe(target) { if (target.matches('textarea')) window.__observedTextareas.add(target); super.observe(target); }
  unobserve(target) { window.__observedTextareas.delete(target); super.unobserve(target); }
};`).Do(ctx)
					return err
				}),
				chromedp.Navigate(server),
				chromedp.Poll(`!!window.__gofastr?.loadedModules?.textarea`, nil),
				chromedp.Sleep(100*time.Millisecond),
			); err != nil {
				t.Fatal(err)
			}
			var height string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('draft').style.height`, &height)); err != nil {
				t.Fatal(err)
			}
			if height == "0px" {
				t.Error("hidden autogrow textarea was collapsed to zero height")
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`(() => { const container = document.getElementById('container'); container.hidden = false; container.open = true; })()`, nil),
				chromedp.Poll(`(() => { const textarea = document.getElementById('draft'); return textarea.clientHeight >= textarea.scrollHeight && textarea.clientHeight > 100; })()`, nil, chromedp.WithPollingTimeout(time.Second)),
			); err != nil {
				t.Fatalf("revealed textarea clipped its prefilled value: %v", err)
			}
			if directory := os.Getenv("GOFASTR_GROUP14_VISUAL_DIR"); directory != "" {
				var screenshot []byte
				if err := chromedp.Run(ctx, chromedp.FullScreenshot(&screenshot, 90)); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "textarea-"+container+".png"), screenshot, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`(() => { const textarea = document.getElementById('draft'); window.__previousHeight = textarea.clientHeight; textarea.cols = 8; })()`, nil),
				chromedp.Poll(`(() => { const textarea = document.getElementById('draft'); return textarea.clientHeight > window.__previousHeight && textarea.clientHeight >= textarea.scrollHeight; })()`, nil, chromedp.WithPollingTimeout(time.Second)),
				chromedp.Evaluate(`window.__detachedTextarea = document.getElementById('draft'); window.__detachedTextarea.remove()`, nil),
				chromedp.Poll(`window.__observedTextareas.size === 0`, nil, chromedp.WithPollingTimeout(time.Second)),
				chromedp.Evaluate(`document.getElementById('container').appendChild(window.__detachedTextarea)`, nil),
				chromedp.Poll(`window.__observedTextareas.size === 1`, nil, chromedp.WithPollingTimeout(time.Second)),
			); err != nil {
				t.Fatalf("textarea resize or observer lifecycle failed: %v", err)
			}
		})
	}
}

// Where the engine sizes a field to its content, the module hands the
// textarea to field-sizing: content and measures nothing (no inline
// height, no observer), and a textarea revealed from a hidden panel
// still fits its prefilled value (#704).
func TestAutogrowUsesFieldSizing(t *testing.T) {
	markup := `<section id="container" hidden><textarea id="draft" data-cui-autogrow>` + strings.Repeat("prefilled text\n", 12) + `</textarea></section>`
	server := widgetHelpersServer(t, markup)
	ctx := chromedptest.Context(t)
	var supported bool
	if err := chromedp.Run(ctx,
		chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(`
window.__observedTextareas = 0;
const NativeResizeObserver = window.ResizeObserver;
window.ResizeObserver = class extends NativeResizeObserver {
  observe(target) { if (target.matches('textarea')) window.__observedTextareas++; super.observe(target); }
};`).Do(ctx)
			return err
		}),
		chromedp.Navigate(server),
		chromedp.Evaluate(`CSS.supports('field-sizing', 'content')`, &supported),
	); err != nil {
		t.Fatal(err)
	}
	if !supported {
		t.Skip("this browser has no field-sizing: the fallback test covers it")
	}
	var got struct {
		Sizing   string `json:"sizing"`
		Height   string `json:"height"`
		Observed int    `json:"observed"`
		Fits     bool   `json:"fits"`
		Grew     bool   `json:"grew"`
	}
	if err := chromedp.Run(ctx,
		chromedp.Poll(`!!window.__gofastr?.loadedModules?.textarea`, nil),
		chromedp.Evaluate(`document.getElementById('container').hidden = false`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`(() => {
			const ta = document.getElementById('draft');
			const before = ta.clientHeight;
			const fits = ta.clientHeight >= ta.scrollHeight && ta.clientHeight > 100;
			ta.value += 'one more line\nand another\n';
			return { sizing: ta.style.fieldSizing, height: ta.style.height, observed: window.__observedTextareas, fits, grew: ta.clientHeight > before };
		})()`, &got),
	); err != nil {
		t.Fatal(err)
	}
	if got.Sizing != "content" || got.Height != "" || got.Observed != 0 {
		t.Errorf("the module measured instead of handing the textarea to field-sizing: %+v", got)
	}
	if !got.Fits || !got.Grew {
		t.Errorf("the revealed textarea does not fit or follow its value: %+v", got)
	}
}
