package ui

// SiteHeader — the top bar that wraps every page of a content/marketing
// site (docs, landing, examples gallery). Composition is generic;
// brand glyph + action affordances stay per-consumer as slots.
//
// Layout: [brand]  ·  [primary nav]  ·  [right cluster: actions + mobile trigger]
//
// On phones the primary nav hides and a <details>-based drawer
// (data-fui-disclosure) opens with the same items inline. The runtime
// auto-closes the drawer on cross-page SPA navigation.

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// SiteHeaderLink configures one primary-nav entry.
type SiteHeaderLink struct {
	// Label is the visible text.
	Label string
	// Href is the navigation target.
	Href string
	// MatchPrefix, when true, lights the link active on any URL that
	// shares the Href as a prefix (e.g. /docs/foo matches /docs/).
	// Wires the data-fui-match-prefix runtime attribute.
	MatchPrefix bool
	// External, when true, opens in a new tab. Only honored in the
	// mobile drawer (the desktop nav stays internal-only by design).
	External bool
}

// SiteHeaderConfig configures a SiteHeader. Brand and Actions are
// slots — the framework owns layout + drawer mechanics; the consumer
// owns visual identity.
type SiteHeaderConfig struct {
	// Brand is the left-most slot. Usually a link with logo + wordmark
	// + optional status pill. No constraint on shape.
	Brand render.HTML
	// NavItems renders both the desktop nav and the mobile drawer list.
	NavItems []SiteHeaderLink
	// Actions is the right-cluster slot for search, theme toggle,
	// icon links, etc. Rendered before the mobile drawer trigger so
	// the trigram glyph sits at the far right.
	Actions render.HTML
	// MobileExtraLinks are appended only to the mobile drawer list
	// (e.g., "Home", "GitHub ↗"). Lets the consumer surface secondary
	// destinations on phones without cluttering the desktop bar.
	MobileExtraLinks []SiteHeaderLink
	// MobileNavAriaLabel labels the mobile <nav>. Defaults to
	// "Mobile primary".
	MobileNavAriaLabel string
	// PrimaryNavAriaLabel labels the desktop <nav>. Defaults to
	// "Primary".
	PrimaryNavAriaLabel string
	// NavUnderline opts the desktop nav links into an animated
	// underline-reveal on hover / focus / active, instead of the default
	// flat colour-only treatment. The underline colour is themeable via
	// --ui-site-header-nav-underline-color (defaults to the primary colour).
	NavUnderline bool
	// Class is appended to the ui-site-header wrapper.
	Class string
}

// SiteHeader renders a marketing/docs top bar. The wrapper is a
// <div> (not <header>) because the framework Layout already wraps
// component output in <header role="banner">. Doubling up would emit
// nested headers.
func SiteHeader(cfg SiteHeaderConfig) render.HTML {
	primaryLabel := cfg.PrimaryNavAriaLabel
	if primaryLabel == "" {
		primaryLabel = "Primary"
	}
	mobileLabel := cfg.MobileNavAriaLabel
	if mobileLabel == "" {
		mobileLabel = "Mobile primary"
	}

	navLink := func(item SiteHeaderLink) render.HTML {
		extra := html.Attrs{}
		if item.MatchPrefix {
			extra["data-fui-match-prefix"] = ""
		}
		if item.External {
			extra["rel"] = "external"
			extra["target"] = "_blank"
		}
		return html.Link(html.LinkConfig{Href: item.Href, Text: item.Label, ExtraAttrs: extra})
	}

	desktopChildren := make([]render.HTML, 0, len(cfg.NavItems))
	for _, item := range cfg.NavItems {
		// Desktop bar deliberately skips External (matches the typical
		// "marketing-y" choice that GitHub etc. belong with icon links
		// or in the mobile drawer, not in the primary nav).
		desktopItem := item
		desktopItem.External = false
		desktopChildren = append(desktopChildren, navLink(desktopItem))
	}
	primary := render.Tag("nav",
		map[string]string{"class": "ui-site-header__links", "aria-label": primaryLabel},
		desktopChildren...,
	)

	mobileChildren := make([]render.HTML, 0, len(cfg.NavItems)+len(cfg.MobileExtraLinks))
	for _, item := range cfg.NavItems {
		mobileChildren = append(mobileChildren, navLink(item))
	}
	for _, item := range cfg.MobileExtraLinks {
		mobileChildren = append(mobileChildren, navLink(item))
	}
	// SVG icons: menu (3 bars) shown in closed state, close (×) shown
	// in open state. The swap is purely CSS-driven (display: none on
	// the non-active icon based on the parent details[open] state).
	// stroke="currentColor" so they inherit the summary's text color.
	mobile := render.Tag("details",
		map[string]string{
			"class":                    "ui-site-header__mobile",
			"data-fui-disclosure":      "",
			"data-fui-disclosure-trap": "",
		},
		render.Tag("summary",
			map[string]string{"class": "ui-site-header__mobile-toggle", "aria-label": "Toggle navigation"},
			render.Raw(`<svg class="ui-site-header__icon ui-site-header__icon--menu" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="4" y1="6" x2="20" y2="6"/><line x1="4" y1="12" x2="20" y2="12"/><line x1="4" y1="18" x2="20" y2="18"/></svg>`),
			render.Raw(`<svg class="ui-site-header__icon ui-site-header__icon--close" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" aria-hidden="true"><line x1="6" y1="6" x2="18" y2="18"/><line x1="18" y1="6" x2="6" y2="18"/></svg>`),
		),
		render.Tag("nav",
			map[string]string{"class": "ui-site-header__mobile-links", "aria-label": mobileLabel},
			mobileChildren...,
		),
	)

	rightChildren := []render.HTML{}
	if cfg.Actions != "" {
		rightChildren = append(rightChildren, cfg.Actions)
	}
	rightChildren = append(rightChildren, mobile)
	right := html.Div(html.DivConfig{Class: "ui-site-header__right"}, rightChildren...)

	cls := "ui-site-header"
	if cfg.NavUnderline {
		cls += " ui-site-header--nav-underline"
	}
	if cfg.Class != "" {
		cls = cls + " " + cfg.Class
	}
	return siteHeaderStyle.WrapHTML(html.Div(html.DivConfig{Class: cls},
		cfg.Brand, primary, right,
	))
}

var siteHeaderStyle = registry.RegisterStyle("ui-site-header", siteHeaderCSS)

func siteHeaderCSS(_ style.Theme) string {
	// The CSS ships layout + drawer mechanics. Brand visuals + accent
	// colors stay with the consuming site's theme. We use CSS variables
	// with sensible fallbacks so an unthemed consumer still gets
	// something functional.
	return `[data-fui-comp="ui-site-header"] {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 16px);
  inline-size: 100%;
  block-size: 100%;
  padding-inline: var(--spacing-lg, 24px);
}
[data-fui-comp="ui-site-header"] .ui-site-header__links {
  display: flex;
  align-items: center;
  gap: var(--spacing-lg, 24px);
  margin-inline-start: var(--spacing-xl, 32px);
}
[data-fui-comp="ui-site-header"] .ui-site-header__links a {
  color: var(--ui-site-header-nav-color, currentColor);
  text-decoration: none;
  font-size: 14px;
}
[data-fui-comp="ui-site-header"] .ui-site-header__links a[data-fui-active] {
  color: var(--ui-site-header-nav-active-color, var(--color-primary, currentColor));
}

/* Opt-in underline-reveal variant (NavUnderline:true). A 1px rule wipes in
   from the left on hover / focus / active. Colour + vertical offset are
   themeable via the --ui-site-header-nav-underline-* vars. */
[data-fui-comp="ui-site-header"].ui-site-header--nav-underline .ui-site-header__links a {
  position: relative;
}
[data-fui-comp="ui-site-header"].ui-site-header--nav-underline .ui-site-header__links a::after {
  content: "";
  position: absolute;
  left: 0;
  right: 100%;
  bottom: var(--ui-site-header-nav-underline-bottom, -4px);
  height: 1px;
  background: var(--ui-site-header-nav-underline-color, var(--color-primary, currentColor));
  transition: right 200ms cubic-bezier(0.4, 0, 0.2, 1);
}
[data-fui-comp="ui-site-header"].ui-site-header--nav-underline .ui-site-header__links a:hover,
[data-fui-comp="ui-site-header"].ui-site-header--nav-underline .ui-site-header__links a:focus-visible {
  color: var(--ui-site-header-nav-active-color, var(--color-text, currentColor));
}
[data-fui-comp="ui-site-header"].ui-site-header--nav-underline .ui-site-header__links a:hover::after,
[data-fui-comp="ui-site-header"].ui-site-header--nav-underline .ui-site-header__links a:focus-visible::after,
[data-fui-comp="ui-site-header"].ui-site-header--nav-underline .ui-site-header__links a[data-fui-active]::after,
[data-fui-comp="ui-site-header"].ui-site-header--nav-underline .ui-site-header__links a[aria-current="page"]::after {
  right: 0;
}
@media (prefers-reduced-motion: reduce) {
  [data-fui-comp="ui-site-header"].ui-site-header--nav-underline .ui-site-header__links a::after {
    transition: none;
  }
}
[data-fui-comp="ui-site-header"] .ui-site-header__right {
  display: flex;
  align-items: center;
  gap: var(--spacing-sm, 8px);
  margin-inline-start: auto;
}
[data-fui-comp="ui-site-header"] .ui-site-header__mobile {
  display: none;
  position: relative;
}
[data-fui-comp="ui-site-header"] .ui-site-header__mobile-toggle {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  inline-size: 44px;
  block-size: 44px;
  background: transparent;
  border: 1px solid var(--ui-site-header-icon-border, transparent);
  border-radius: var(--radius-sm, 6px);
  cursor: pointer;
  list-style: none;
}
[data-fui-comp="ui-site-header"] .ui-site-header__mobile-toggle::-webkit-details-marker { display: none; }

/* Icon swap: menu in closed state, X in open state. Inline SVG paths
   inherit color via stroke="currentColor". Single source of visual
   truth — no pseudo-bar math, no transform animations to misalign. */
[data-fui-comp="ui-site-header"] .ui-site-header__icon { display: block; }
[data-fui-comp="ui-site-header"] .ui-site-header__icon--close { display: none; }
[data-fui-comp="ui-site-header"] details[open] .ui-site-header__icon--menu { display: none; }
[data-fui-comp="ui-site-header"] details[open] .ui-site-header__icon--close { display: block; }

/* Mobile drawer: position-, size-, and color-themable via CSS vars
   so a host can pick the popover-vs-sheet shape without overriding
   the rule. Defaults are a trigger-anchored popover at the top right;
   set --ui-site-header-drawer-position: fixed + inset values to
   convert to a viewport-anchored sheet. */
[data-fui-comp="ui-site-header"] .ui-site-header__mobile-links {
  position: var(--ui-site-header-drawer-position, absolute);
  inset-block-start: var(--ui-site-header-drawer-top, calc(100% + 8px));
  inset-inline-end: var(--ui-site-header-drawer-right, 0);
  inset-inline-start: var(--ui-site-header-drawer-left, auto);
  display: flex;
  flex-direction: column;
  gap: var(--spacing-xs, 4px);
  min-inline-size: var(--ui-site-header-drawer-min-width, 200px);
  padding: var(--spacing-sm, 8px);
  background: var(--color-surface, #fff);
  border: 1px solid var(--color-border, rgba(0,0,0,0.1));
  border-radius: var(--radius-md, 8px);
  box-shadow: var(--ui-site-header-drawer-shadow, 0 10px 30px rgba(0,0,0,0.18));
  z-index: 50;
}
[data-fui-comp="ui-site-header"] .ui-site-header__mobile-links a {
  display: block;
  padding: var(--spacing-sm, 8px) var(--spacing-md, 12px);
  color: currentColor;
  text-decoration: none;
  border-radius: var(--radius-sm, 6px);
}
[data-fui-comp="ui-site-header"] .ui-site-header__mobile-links a:hover,
[data-fui-comp="ui-site-header"] .ui-site-header__mobile-links a:focus-visible {
  background: var(--color-surface-soft, rgba(0,0,0,0.04));
}

@media (max-width: 720px) {
  [data-fui-comp="ui-site-header"] .ui-site-header__links { display: none; }
  [data-fui-comp="ui-site-header"] .ui-site-header__mobile { display: block; }
}
`
}
