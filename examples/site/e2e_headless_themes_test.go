package main

// Browser-level (chromedp) proofs for the three showcase themes added
// beyond default and dense — soft, editorial, contrast — plus the
// theme switcher and the nesting fixture's B label, on every route of
// both pages.
//
// The per-theme proofs read COMPUTED style, in light and in dark: the
// declared option set must survive the whole compiler → scope-block →
// cascade path, per scheme, not merely exist in the theme struct.

import (
	"strconv"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// hlThemeProbe reads, from one page, everything the per-theme proofs
// assert on: the primary button's geometry, background, border and the
// theme's resolved primary colour (through a throwaway probe element,
// so a token spelling like #6D28D9 and a computed rgb(...) compare as
// the same string), and the scoped h1's font family.
const hlThemeProbe = `(() => {
  const el = document.querySelector('#hl-variant-primary');
  const cs = getComputedStyle(el);
  const probe = document.createElement('span');
  probe.style.backgroundColor = cs.getPropertyValue('--color-primary').trim();
  document.body.appendChild(probe);
  const primaryBG = getComputedStyle(probe).backgroundColor;
  probe.remove();
  const h1 = document.querySelector('div[class^="fui-theme-"] h1');
  return {
    radius: cs.borderRadius,
    height: el.getBoundingClientRect().height,
    bg: cs.backgroundColor,
    borderTopWidth: cs.borderTopWidth,
    borderTopColor: cs.borderTopColor,
    primary: primaryBG,
    headingFont: h1 ? getComputedStyle(h1).fontFamily : '',
  };
})()`

// hlThemeMetrics is the Go shape of hlThemeProbe's return.
type hlThemeMetrics struct {
	Radius, BG, BorderTopWidth, BorderTopColor, Primary, HeadingFont string
	Height                                                           float64
}

// TestE2E_HeadlessLanding_NewThemeComputedStyles proves each new
// theme's declared options reach the rendered button and heading, in
// light and in dark:
//
//   - soft draws a pill (computed radius at least half the height) and
//     the soft treatment (a background that is neither transparent nor
//     the primary fill);
//   - editorial sets the serif heading face and a zero button radius;
//   - contrast draws the outline treatment (transparent background, a
//     border in the primary colour — which flips with the scheme, so
//     the probe re-resolves it per scheme).
func TestE2E_HeadlessLanding_NewThemeComputedStyles(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	for _, scheme := range []struct{ name, force string }{
		{"light", hlForceLight},
		{"dark", `document.documentElement.setAttribute('data-color-scheme','dark')`},
	} {
		for _, tc := range []struct{ seg, name string }{
			{"soft", "Soft"},
			{"editorial", "Editorial"},
			{"contrast", "High contrast"},
		} {
			t.Run(tc.seg+"-"+scheme.name, func(t *testing.T) {
				ctx := newE2EBrowserCtx(t)
				var m hlThemeMetrics
				if err := chromedp.Run(ctx,
					chromedp.Navigate(base+landingRoutePath(tc.seg)),
					pageReady(),
					chromedp.Evaluate(scheme.force, nil),
					chromedp.Evaluate(hlThemeProbe, &m),
				); err != nil {
					t.Fatalf("chromedp: %v", err)
				}
				radiusPx, err := strconv.ParseFloat(strings.TrimSuffix(m.Radius, "px"), 64)
				if err != nil {
					t.Fatalf("%s/%s: computed border-radius %q is not a px length", tc.seg, scheme.name, m.Radius)
				}
				transparent := m.BG == "rgba(0, 0, 0, 0)"
				isPrimary := strings.EqualFold(m.BG, m.Primary)
				switch tc.seg {
				case "soft":
					if radiusPx < m.Height/2 {
						t.Errorf("soft/%s: radius %s on a %.0fpx button is not a pill (want at least half the height)", scheme.name, m.Radius, m.Height)
					}
					if transparent {
						t.Errorf("soft/%s: primary button background is transparent — the soft treatment's tint did not paint", scheme.name)
					}
					if isPrimary {
						t.Errorf("soft/%s: primary button background equals the primary fill %s — the treatment drew filled, not soft", scheme.name, m.Primary)
					}
				case "editorial":
					if !strings.HasPrefix(m.HeadingFont, `"Iowan Old Style"`) {
						t.Errorf("editorial/%s: heading font-family = %q, want it to start with \"Iowan Old Style\"", scheme.name, m.HeadingFont)
					}
					if m.Radius != "0px" {
						t.Errorf("editorial/%s: primary button border-radius = %q, want 0px", scheme.name, m.Radius)
					}
				case "contrast":
					if !transparent {
						t.Errorf("contrast/%s: primary button background %s is not transparent — the outline treatment's fill leaked in", scheme.name, m.BG)
					}
					if m.BorderTopWidth == "0px" {
						t.Errorf("contrast/%s: primary button has no top border — the outline treatment did not draw one", scheme.name)
					}
					if !strings.EqualFold(m.BorderTopColor, m.Primary) {
						t.Errorf("contrast/%s: border colour %s is not the primary %s — the outline must draw in the scheme's primary", scheme.name, m.BorderTopColor, m.Primary)
					}
				}
			})
		}
	}
}

// TestHeadlessThemeSwitcherSSR pins the switcher's server-rendered
// contract on every route of both pages, from the page HTML before any
// runtime script can decorate it: one link per registered route,
// exactly one aria-current="page", on the link that points at the
// current path and is labelled with the route's name.
func TestHeadlessThemeSwitcherSSR(t *testing.T) {
	for _, page := range []struct {
		name   string
		pathOf func(seg string) string
	}{
		{"landing", landingRoutePath},
		{"dashboard", dashboardRoutePath},
	} {
		for _, r := range landingRoutes {
			html := body(t, page.pathOf(r.Segment))
			want := `id="hl-theme-nav"`
			if !strings.Contains(html, want) {
				t.Fatalf("%s/%s: the theme nav is missing from the page", page.name, r.Segment)
			}
			// Slice the nav out of the page so the counts are the nav's
			// alone, then count its anchors.
			navStart := strings.Index(html, `id="hl-theme-nav"`)
			navEnd := strings.Index(html[navStart:], "</nav>")
			if navStart < 0 || navEnd < 0 {
				t.Fatalf("%s/%s: the theme nav does not close", page.name, r.Segment)
			}
			nav := html[navStart : navStart+navEnd]
			links := strings.Count(nav, "<a ")
			if links != len(landingRoutes) {
				t.Errorf("%s/%s: the nav has %d anchors, want %d (one per registered route)", page.name, r.Segment, links, len(landingRoutes))
			}
			current := strings.Count(nav, `aria-current="page"`)
			if current != 1 {
				t.Errorf("%s/%s: %d anchors carry aria-current=\"page\", want exactly 1", page.name, r.Segment, current)
				continue
			}
			// The current link points at the current path and is named
			// for the route. The anchor tag may order aria-current
			// before href, so read the whole tag, not the run before
			// the marker.
			marker := strings.Index(nav, `aria-current="page"`)
			tagStart := strings.LastIndex(nav[:marker], "<a ")
			tagEnd := strings.Index(nav[marker:], ">")
			tag := nav[tagStart : marker+tagEnd]
			if !strings.Contains(tag, `href="`+page.pathOf(r.Segment)+`"`) {
				t.Errorf("%s/%s: the aria-current anchor's href is not the current path %q (tag %q)", page.name, r.Segment, page.pathOf(r.Segment), tag)
			}
			linkEnd := tagStart + strings.Index(nav[tagStart:], "</a>")
			if !strings.Contains(nav[tagStart:linkEnd+len("</a>")], ">"+r.Name+"</a>") {
				t.Errorf("%s/%s: the aria-current anchor is not labelled %q", page.name, r.Segment, r.Name)
			}
		}
	}
}

// TestE2E_HeadlessLanding_ThemeSwitcher proves the switcher's
// behaviour in the browser on every route of both pages: clicking
// another route's link lands on that theme's SAME page. (The
// structural five-links/one-aria-current contract is pinned at the SSR
// level above — the runtime's activelink module also decorates nav
// links client-side, which would mask a missing server-side attribute
// here.)
func TestE2E_HeadlessLanding_ThemeSwitcher(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	for _, page := range []struct {
		name   string
		pathOf func(seg string) string
	}{
		{"landing", landingRoutePath},
		{"dashboard", dashboardRoutePath},
	} {
		for _, r := range landingRoutes {
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base+page.pathOf(r.Segment)),
				pageReady(),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			// Click the LAST route's link (never the current one on any
			// route but a two-route list): the browser must land on that
			// theme's same page.
			target := landingRoutes[len(landingRoutes)-1]
			if target.Segment == r.Segment {
				target = landingRoutes[0]
			}
			var landed string
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`document.querySelector('#hl-theme-nav a[href="`+page.pathOf(target.Segment)+`"]').click()`, nil),
				chromedp.WaitReady("body", chromedp.ByQuery),
				chromedp.Location(&landed),
			); err != nil {
				t.Fatalf("chromedp click: %v", err)
			}
			if !strings.HasPrefix(landed, base+page.pathOf(target.Segment)) {
				t.Errorf("%s/%s: clicking %s's link landed on %q, want %q", page.name, r.Segment, target.Segment, landed, page.pathOf(target.Segment))
			}
		}
	}
}

// TestE2E_HeadlessLanding_NestingLabelIsOtherName proves the nesting
// fixture's B boundary is labelled with the Name of the route whose
// theme is Other — not a segment branch — on every route: the
// three level headings read [this route, the Other route, this route
// again].
func TestE2E_HeadlessLanding_NestingLabelIsOtherName(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e: -short")
	}
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)
	for _, r := range landingRoutes {
		var labels []string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+landingRoutePath(r.Segment)),
			pageReady(),
			chromedp.Evaluate(`Array.from(document.querySelectorAll('#hl-nesting h3')).map(h => h.textContent.trim())`, &labels),
		); err != nil {
			t.Fatalf("chromedp: %v", err)
		}
		var otherName string
		for _, o := range landingRoutes {
			if o.Ref == r.Other {
				otherName = o.Name
			}
		}
		want := []string{r.Name, otherName, r.Name + " again"}
		if len(labels) != len(want) {
			t.Fatalf("%s: nesting fixture has %d level headings (%v), want %d", r.Segment, len(labels), labels, len(want))
		}
		for i, w := range want {
			if labels[i] != w {
				t.Errorf("%s: nesting level %d is labelled %q, want %q (the Other route's name)", r.Segment, i, labels[i], w)
			}
		}
	}
}
