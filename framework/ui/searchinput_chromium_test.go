//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// Hard rule 9: which box the focus ring paints is layout, invisible to
// a stylesheet string check. The input's own :focus-visible rule drew a
// second, inset teal border INSIDE the outer frame (text fields match
// :focus-visible on any focus, click included); the frame is the thing
// that should carry the ring. This renders a SearchInput, clicks into
// the field with a real pointer event, and reads the computed styles.
func TestSearchFocusRingOnTheInput(t *testing.T) {
	page := SearchInput(SearchInputConfig{
		Name: "q", ID: "find", Placeholder: "Filter",
	})
	css := searchInputStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>*,*::before,*::after{box-sizing:border-box}body{margin:0;padding:24px}%s
%s</style>%s`,
			theme.Default().CSSCustomProperties(), css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	var m map[string]any
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL),
		chromedp.WaitVisible("#find", chromedp.ByQuery),
		// A real click into the field: a text input matches
		// :focus-visible on ANY focus, not only keyboard, so the ring
		// shows for mouse users too (the ring IS the indicator).
		chromedp.Click("#find", chromedp.ByQuery),
		chromedp.Evaluate(`(() => {
			const input = document.getElementById('find');
			const frame = input.closest('.fui-search');
			const i = getComputedStyle(input);
			const f = getComputedStyle(frame);
			return {
				focused: document.activeElement === input,
				inputOutline: i.outlineStyle + ' ' + i.outlineWidth,
				inputBorder: i.borderTopStyle + ' ' + i.borderTopWidth,
				frameOutline: f.outlineStyle + ' ' + f.outlineWidth + ' ' + f.outlineColor,
				frameShadow: f.boxShadow,
			};
		})()`, &m),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// The ring lives ON the input, at the frame's inner edge: an
	// element-local indicator is what a keyboard user (and the
	// keyboard-walk gate) judges — the old frame-only :focus-within
	// ring showed nothing on the focused control itself.
	o := m["inputOutline"].(string)
	if !strings.Contains(o, "solid") {
		t.Errorf("focused input paints no outline (%s) — the ring must be element-local", o)
	}
	if b := m["inputBorder"].(string); !strings.HasPrefix(b, "none") {
		t.Errorf("focused input paints its own border (%s) — the inner field must draw nothing", b)
	}
	if f := m["frameOutline"].(string); !strings.HasPrefix(f, "none") {
		t.Errorf("focused frame paints a second outline (%s) — one ring, not two", f)
	}
}
