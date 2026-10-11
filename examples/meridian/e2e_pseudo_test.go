package main

// Pseudo-locale overflow gate for the admin. The app boots with
// MERIDIAN_PSEUDO_LOCALE=1, the browser asks for en-XA, and every
// framework string arrives accented and at least a third longer: about
// what German or Spanish adds to English. The same screens are then
// browsed in Spanish, whose real words are longest in other places. Each admin screen the sidebar
// links, plus a record, is then checked at desktop and phone widths for
// the two ways longer words break a layout: the page scrolls sideways,
// or a control clips its own label with no ellipsis to say so.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// overflowProbe lists what overflows on the current page: the document
// wider than the viewport, content that runs past the screen outside a
// scroller, a control or badge that ends past the box drawn around it,
// a control another box covers, and any control, tab, badge, label or header
// cell whose text is cut off by its own box without an ellipsis, or
// runs past it.
const overflowProbe = `(() => {
	const out = [];
	const vw = document.documentElement.clientWidth;
	if (document.documentElement.scrollWidth > vw + 1) {
		out.push('page scrolls sideways: ' + document.documentElement.scrollWidth + ' > ' + vw);
	}
	// The shell contains its own overflow, so the page may not scroll
	// while its content runs past the screen. An element that ends past
	// the viewport is cut off unless a scroller (overflow-x auto or
	// scroll, and wider inside than out) holds it on purpose. Only the
	// outermost one is named.
	const scrolled = el => {
		for (let a = el.parentElement; a; a = a.parentElement) {
			const ox = getComputedStyle(a).overflowX;
			if ((ox === 'auto' || ox === 'scroll') && a.scrollWidth > a.clientWidth + 1) return true;
		}
		return false;
	};
	// Inside a closed disclosure or a visually hidden box nothing shows.
	const unseen = el => {
		if (el.closest('details:not([open]) > :not(summary)')) return true;
		for (let a = el.parentElement; a; a = a.parentElement) {
			const ar = a.getBoundingClientRect();
			if (ar.width <= 2 || ar.height <= 2) return true;
		}
		return false;
	};
	const past = el => {
		const r = el.getBoundingClientRect();
		return r.width > 2 && r.height > 2 && (r.right > vw + 1 || r.left < -1) && !scrolled(el) && !unseen(el);
	};
	for (const el of document.querySelectorAll('body *')) {
		if (!past(el) || (el.parentElement && past(el.parentElement))) continue;
		if (el.closest('[hidden], [aria-hidden="true"], dialog:not([open])')) continue;
		const cs = getComputedStyle(el);
		if (cs.visibility === 'hidden' || cs.position === 'fixed') continue;
		out.push('runs past the screen: ' + el.tagName.toLowerCase() + '.' + (el.className || '').toString().split(' ')[0] + ' (' + Math.round(el.getBoundingClientRect().left) + '..' + Math.round(el.getBoundingClientRect().right) + ' of ' + vw + ')');
	}
	const sel = 'button, a, summary, label, th, [role=tab], .fui-badge, .fui-tab-nav__link, .fui-page-header__title';
	for (const el of document.querySelectorAll(sel)) {
		const r = el.getBoundingClientRect();
		// A visually hidden element is a 1px box on purpose.
		if (r.width <= 2 || r.height <= 2) continue;
		if (el.closest('[hidden], [aria-hidden="true"], dialog:not([open])') || unseen(el)) continue;
		const cs = getComputedStyle(el);
		// The element itself ends past the box that draws around it:
		// the nearest ancestor that clips, or paints a border or a fill.
		for (let a = el.parentElement; a && a !== document.body; a = a.parentElement) {
			const as = getComputedStyle(a);
			const clipsA = as.overflowX !== 'visible';
			const painted = parseFloat(as.borderRightWidth) > 0 || (as.backgroundColor !== 'rgba(0, 0, 0, 0)' && as.backgroundColor !== 'transparent');
			if (!clipsA && !painted) continue;
			// A table's row groups are not boxes a reader sees: card
			// layouts reflow cells inside them.
			if (/^(TR|THEAD|TBODY|TFOOT)$/.test(a.tagName)) continue;
			if ((as.overflowX === 'auto' || as.overflowX === 'scroll') && a.scrollWidth > a.clientWidth + 1) break;
			const ar = a.getBoundingClientRect();
			// A visually hidden box, a scroller further out, and inline
			// text the box ends with an ellipsis are all on purpose.
			if (ar.width <= 2 || ar.height <= 2 || scrolled(a)) break;
			if (as.textOverflow === 'ellipsis' && cs.display === 'inline') break;
			if (r.right > ar.right + 1 || r.left < ar.left - 1) {
				out.push('runs out of ' + a.tagName.toLowerCase() + '.' + (a.className || '').toString().split(' ')[0] + ': ' + el.tagName.toLowerCase() + '.' + (el.className || '').toString().split(' ')[0] + ' ' + el.textContent.trim().slice(0, 40));
			}
			break;
		}
		// A control another box draws over cannot be reached: its
		// centre must hit itself.
		if (/^(BUTTON|A|SUMMARY)$/.test(el.tagName)) {
			const x = r.left + r.width / 2, y = r.top + r.height / 2;
			if (x >= 0 && y >= 0 && x < vw && y < innerHeight) {
				const hit = document.elementFromPoint(x, y);
				if (hit && !el.contains(hit) && !hit.contains(el)) {
					out.push('covered: ' + el.tagName.toLowerCase() + '.' + (el.className || '').toString().split(' ')[0] + ' ' + el.textContent.trim().slice(0, 30) + ' under ' + hit.tagName.toLowerCase() + '.' + (hit.className || '').toString().split(' ')[0]);
				}
			}
		}
		if (cs.textOverflow === 'ellipsis' || el.scrollWidth <= el.clientWidth + 1) continue;
		const clips = cs.overflowX === 'hidden' || cs.overflowX === 'clip';
		out.push((clips ? 'clipped ' : 'spills out of ') + el.tagName.toLowerCase() + '.' + (el.className || '').toString().split(' ')[0] + ': ' + el.textContent.trim().slice(0, 60));
	}
	return out;
})()`

func TestE2E_AdminPseudoLocaleOverflow(t *testing.T) {
	if testing.Short() {
		t.Skip("builds + boots the binary")
	}
	t.Setenv("MERIDIAN_PSEUDO_LOCALE", "1")
	base := e2eBootApp(t)
	ctx := chromedptest.Context(t, chromedptest.WindowSize(1280, 800), chromedptest.Timeout(5*time.Minute))
	if err := chromedp.Run(ctx, network.Enable(),
		network.SetExtraHTTPHeaders(network.Headers{"Accept-Language": pseudoLocale})); err != nil {
		t.Fatal(err)
	}
	e2eLogin(t, ctx, base)

	// The admin's own screens, from its sidebar, plus one record.
	var paths []string
	var record string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/admin"),
		chromedp.WaitVisible(`main`, chromedp.ByQuery),
		chromedp.Evaluate(`[...new Set([...document.querySelectorAll('nav a[href^="/admin"]')].map(a => a.getAttribute('href')))]`, &paths),
		chromedp.Navigate(base+"/admin/entities/customers"),
		chromedp.WaitVisible(`main`, chromedp.ByQuery),
		chromedp.Evaluate(`(document.querySelector('tbody a[href^="/admin/entities/customers/"]') || {}).getAttribute?.('href') || ''`, &record),
	); err != nil {
		t.Fatalf("collect admin screens: %v", err)
	}
	if len(paths) < 5 || record == "" {
		t.Fatalf("setup: sidebar links %v, record %q", paths, record)
	}
	paths = append(paths, record, "/admin/shortcuts", "/admin/account")

	// Each locale in turn: the pseudo locale, then Spanish, whose
	// real words run longest in places the pseudo text does not.
	for _, loc := range []struct{ tag, dashboard string }{
		{pseudoLocale, i18nui.Pseudo(i18nui.Defaults[i18nui.KeyAdminDashboard])},
		{"es", "Panel"},
	} {
		checkLocaleOverflow(t, ctx, base, loc.tag, loc.dashboard, paths)
	}
}

// checkLocaleOverflow browses paths in one locale at desktop and phone
// widths and reports what overflows.
func checkLocaleOverflow(t *testing.T, ctx context.Context, base, tag, dashboard string, paths []string) {
	t.Helper()
	// The locale took: the dashboard heading is in it.
	var heading string
	if err := chromedp.Run(ctx,
		network.SetExtraHTTPHeaders(network.Headers{"Accept-Language": tag}),
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(base+"/admin"),
		chromedp.Text(`h1`, &heading, chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(heading, dashboard) {
		t.Fatalf("setup: the admin did not render in %s: heading %q, want %q", tag, heading, dashboard)
	}

	for _, size := range []struct {
		name string
		w, h int64
	}{{"desktop", 1280, 800}, {"phone", 375, 812}} {
		for _, path := range paths {
			var problems []string
			if err := chromedp.Run(ctx,
				chromedp.EmulateViewport(size.w, size.h),
				chromedp.Navigate(base+path),
				chromedp.WaitVisible(`main`, chromedp.ByQuery),
				chromedp.Sleep(300*time.Millisecond),
				chromedp.Evaluate(overflowProbe, &problems),
			); err != nil {
				t.Fatalf("%s %s %s: %v", tag, size.name, path, err)
			}
			for _, p := range problems {
				t.Errorf("%s %s %s: %s", tag, size.name, path, p)
			}
		}
	}
}
