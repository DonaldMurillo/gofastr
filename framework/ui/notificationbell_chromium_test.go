//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
)

func TestBellBadgeCoversAtMostAQuarterOfTheGlyph(t *testing.T) {
	// Two badge widths: a single digit and a two-digit count (the
	// widest everyday shape; formatBellCount caps the spoken/wide forms).
	trigger3, _ := NotificationBell(NotificationBellConfig{Name: "bell", Href: "/inbox", Label: "Notifications", UnreadCount: 3})
	trigger12, _ := NotificationBell(NotificationBellConfig{Name: "bell2", Href: "/inbox", Label: "Notifications", UnreadCount: 12})
	css := notificationBellStyle.Entry().CSSFor(theme.Default())

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>*,*::before,*::after{box-sizing:border-box}body{margin:0;padding:24px;display:flex;gap:48px}
%s
%s</style>%s%s`,
			theme.Default().CSSCustomProperties(), css, string(trigger3), string(trigger12))
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

	// measure returns, per bell: the badge/svg intersection as a share
	// of the svg's area, and whether the badge escapes the button.
	const measure = `(() => {
	const out = [];
	for (const b of document.querySelectorAll('[data-cui-comp="ui-notification-bell"]')) {
		const svg = b.querySelector('svg').getBoundingClientRect();
		const badge = b.querySelector('.fui-notification-bell__badge').getBoundingClientRect();
		const btn = b.getBoundingClientRect();
		const ix = Math.max(0, Math.min(svg.right, badge.right) - Math.max(svg.left, badge.left));
		const iy = Math.max(0, Math.min(svg.bottom, badge.bottom) - Math.max(svg.top, badge.top));
		out.push({
			share: (ix * iy) / (svg.width * svg.height),
			inBtn: badge.left >= btn.left - 0.5 && badge.right <= btn.right + 0.5 &&
				badge.top >= btn.top - 0.5 && badge.bottom <= btn.bottom + 0.5,
			svgArea: svg.width * svg.height,
		});
	}
	return out;
})()`

	for _, scheme := range []string{"light", "dark"} {
		for _, vp := range [][2]int{{1280, 800}, {390, 844}} {
			var rows []map[string]any
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL),
				chromedp.EmulateViewport(int64(vp[0]), int64(vp[1])),
				chromedp.Evaluate(
					`document.documentElement.setAttribute('data-color-scheme', '`+scheme+`')`, nil),
				chromedp.Evaluate(measure, &rows),
			); err != nil {
				t.Fatalf("%s %dx%d: %v", scheme, vp[0], vp[1], err)
			}
			if len(rows) != 2 {
				t.Fatalf("%s %dx%d: measured %d bells, want 2", scheme, vp[0], vp[1], len(rows))
			}
			for i, row := range rows {
				if row["svgArea"].(float64) <= 0 {
					t.Errorf("%s %dx%d bell %d: the svg has no box — the glyph is hidden", scheme, vp[0], vp[1], i)
				}
				if s := row["share"].(float64); s > 0.25 {
					t.Errorf("%s %dx%d bell %d: the badge covers %.0f%% of the glyph (want ≤ 25%%)",
						scheme, vp[0], vp[1], i, s*100)
				}
				if !row["inBtn"].(bool) {
					t.Errorf("%s %dx%d bell %d: the badge extends past the button", scheme, vp[0], vp[1], i)
				}
			}
		}
	}
}

// An open bell under the pointer keeps a legible glyph. The runtime's
// generic .is-popover-trigger-active rule sets a primary fill and
// primary-fg text; the bell's own :hover rule used to win the
// background alone, leaving a white glyph on light grey. Both
// stylesheet orders are checked, since the widget sheet and the
// component bundle can land either way round.
func TestBellOpenUnderPointerKeepsGlyphContrast(t *testing.T) {
	trigger, _ := NotificationBell(NotificationBellConfig{Name: "bell", Href: "/inbox", Label: "Notifications", UnreadCount: 3})
	css := notificationBellStyle.Entry().CSSFor(theme.Default())
	const active = `.is-popover-trigger-active{background:var(--color-primary);color:var(--color-primary-fg);border-color:var(--color-primary)}`

	for _, order := range []string{"generic-last", "generic-first"} {
		sheet := css + "\n" + active
		if order == "generic-first" {
			sheet = active + "\n" + css
		}
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>*,*::before,*::after{box-sizing:border-box}body{margin:0;padding:24px}
%s
%s</style>%s`, theme.Default().CSSCustomProperties(), sheet, string(trigger))
		}))

		allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
			append(chromedp.DefaultExecAllocatorOptions[:],
				chromedp.WSURLReadTimeout(90*time.Second),
				chromedp.NoSandbox)...)
		ctx, cancel := chromedp.NewContext(allocCtx)
		ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)

		const measure = `(() => {
	const lum = s => { const [r, g, b] = s.match(/[\d.]+/g).slice(0, 3).map(Number).map(v => { v /= 255; return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4); }); return 0.2126 * r + 0.7152 * g + 0.0722 * b; };
	const b = document.querySelector('[data-cui-comp="ui-notification-bell"]');
	const c = getComputedStyle(b);
	let bg = c.backgroundColor;
	if (/rgba\(.*, 0\)$/.test(bg) || bg === 'transparent') bg = getComputedStyle(document.body).backgroundColor;
	if (/rgba\(0, 0, 0, 0\)/.test(bg)) bg = 'rgb(255, 255, 255)';
	const a = lum(c.color), z = lum(bg);
	return { ratio: (Math.max(a, z) + 0.05) / (Math.min(a, z) + 0.05), fg: c.color, bg: bg, hover: b.matches(':hover') };
})()`
		for _, scheme := range []string{"light", "dark"} {
			var row map[string]any
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL),
				chromedp.EmulateViewport(1280, 800),
				chromedp.Evaluate(`document.documentElement.setAttribute('data-color-scheme', '`+scheme+`')`, nil),
				chromedp.Evaluate(`document.querySelector('[data-cui-comp="ui-notification-bell"]').classList.add('is-popover-trigger-active')`, nil),
				chromedp.ActionFunc(func(ctx context.Context) error {
					return input.DispatchMouseEvent(input.MouseMoved, 46, 46).Do(ctx)
				}),
				chromedp.Evaluate(measure, &row),
			); err != nil {
				t.Fatalf("%s %s: %v", order, scheme, err)
			}
			if !row["hover"].(bool) {
				t.Fatalf("%s %s: the pointer is not over the bell; the check measured nothing", order, scheme)
			}
			if r := row["ratio"].(float64); r < 3 {
				t.Errorf("%s %s: open bell under the pointer draws %v on %v (contrast %.2f, want >= 3)",
					order, scheme, row["fg"], row["bg"], r)
			}
		}
		cancelTimeout()
		cancel()
		cancelAlloc()
		srv.Close()
	}
}
