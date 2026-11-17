package uihost

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/i18n"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime/minify"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// CustomCSS returns the extra CSS string injected into every page.
func (ds *UIHost) CustomCSS() string {
	return ds.customCSS
}

// AppCSS returns the app stylesheet under the app theme, the body
// /__gofastr/app.css serves and static export writes.
func (ds *UIHost) AppCSS() string {
	return ds.AppCSSFor(ds.activeTheme())
}

// AppCSSFor returns the app stylesheet under t without touching App.Theme.
// The caller content-addresses the result.
func (ds *UIHost) AppCSSFor(t style.Theme) string {
	out := t.CSSCustomProperties() + "\n"
	out += ds.frameworkCSS()
	if ds.hasInterceptingRoute() {
		out += app.InterceptOverlayCSS() + "\n"
	}
	if overrides := style.AllThemeOverridesCSS(); overrides != "" {
		out += overrides + "\n"
	}
	out += ds.customCSS
	contrib := style.NewStyleSheet(t)
	style.Apply(contrib)
	if css := contrib.CSS(); css != "" {
		out += "\n" + css
	}
	return out
}

func (ds *UIHost) frameworkCSS() string {
	if ds.pageLoading != nil {
		return frameworkBuiltinCSS + frameworkPageLoadingCSS + frameworkDimCSS
	}
	return frameworkBuiltinCSS + frameworkProgressStripCSS + frameworkDimCSS
}

const frameworkBuiltinCSS = `
/* Border-box everywhere: padding/borders count toward declared widths, so
   padded full-width bars don't overflow the viewport. The single most common
   reset every app needs — shipping it here means no app re-declares it. */
*, *::before, *::after { box-sizing: border-box; }
/* The hidden attribute wins over every component's own display rule.
   ui.Button sets display: inline-flex on its element, which beat the
   UA's [hidden] { display: none } and left a server-rendered
   hidden="" button visible (a pixel check of desktop-focus caught all
   four timer buttons on screen at once). One reset here, so no
   component needs its own [hidden] rule. */
[hidden] { display: none !important; }
/* Base surface + typography floor. The page picks up the theme background/text
   tokens (so it's readable on any OS canvas, light or dark) and the --font-body
   family. Headings use --font-heading (falls back to body). Without an explicit
   body font-family the UA default (Times/serif) renders, which reads as
   unstyled. Apps override via their own WithCustomCSS (appended after this
   block — last rule wins). */
html { background-color: var(--color-background, #fff); }
body {
  margin: 0;
  min-height: 100vh;
  background-color: var(--color-background, #fff);
  color: var(--color-text, #09090B);
  font-family: var(--font-body, 'Inter', system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Helvetica, Arial, sans-serif);
  line-height: var(--leading-normal, 1.5);
  -webkit-text-size-adjust: 100%;
  -webkit-font-smoothing: antialiased;
}
h1, h2, h3, h4 { font-family: var(--font-heading, var(--font-body, inherit)); }
/* Columns of numbers (money, counts) align when figures are tabular. */
[data-cui-comp="ui-data-table"] td,
[data-cui-comp="ui-stat-card"],
[data-cui-comp="ui-bar-chart"],
[data-cui-comp="ui-detail-list"] .fui-detail-list__value,
.ui-money,
td[data-align="end"] {
  font-variant-numeric: tabular-nums;
  font-feature-settings: "tnum" 1, "lnum" 1;
}
/* The SPA runtime focuses the swap target (<main id="main-content"> or
   a nested .layout-content cell, both tabindex="-1") after a
   client-side nav so screen readers announce the new content. It is a
   programmatic focus target, not a tabbable control, so the focus ring
   is noise there — EXCEPT when focus is keyboard-visible, where the
   ring is the only signal of where focus went. The runtime also passes
   focusVisible:false for pointer-initiated navigations; this rule is
   the cross-browser belt for engines without the FocusOptions member
   and for any host CSS that keys a ring on bare :focus. */
#main-content:focus:not(:focus-visible),
main[tabindex="-1"]:focus:not(:focus-visible),
.layout-content[tabindex="-1"]:focus:not(:focus-visible) { outline: none; }
/* Visually-hidden helper, one recipe under two names: .cui-visually-hidden
   is the kernel's (the SPA route-announce region core-ui/app renders),
   .fui-visually-hidden is framework/ui's (CommandPalette's SR-only
   trigger, CopyButton's status region, the toast tone word). Apps never
   call ui.BaseCSS() to opt in; the helper is part of the built-in
   auto-emitted app.css floor. An app that wants its own recipe
   overrides either class from its own sheet — last rule wins. */
.cui-visually-hidden, .fui-visually-hidden {
  position: absolute !important;
  width: 1px; height: 1px;
  padding: 0; margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
/* Skip-link: visually hidden until focused, then revealed as a
   visible overlay so keyboard users can jump to #main-content.
   Apps can override via their own .skip-link / .skip-link:focus
   rules. */
.skip-link {
  position: absolute !important;
  width: 1px; height: 1px;
  padding: 0; margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
  border: 0;
}
.skip-link:focus {
  position: fixed !important;
  top: var(--spacing-md, 8px); inset-inline-start: var(--spacing-md, 8px);
  width: auto; height: auto;
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  margin: 0;
  overflow: visible;
  clip: auto;
  white-space: normal;
  z-index: calc(var(--z-toast, 500) + 1);
  background: var(--color-text, #09090B);
  color: var(--color-background, #FFFFFF);
  border-radius: var(--radii-sm, 6px);
  font: var(--text-sm, 0.875rem) var(--font-body, system-ui, -apple-system, sans-serif);
  text-decoration: none;
}
/* SPA-nav failure toast — shown when loadPage can't fetch the new
   page (offline, server error). Positioned bottom-right; auto-hides
   after 4s via the runtime. Strict-CSP-clean (no inline styles). */
.cui-nav-toast {
  position: fixed;
  inset-inline-end: var(--spacing-lg, 16px); bottom: var(--spacing-lg, 16px);
  z-index: var(--z-toast, 500);
  max-width: calc(100vw - var(--spacing-2xl, 32px));
  padding: calc(var(--spacing-sm, 4px) * 3) var(--spacing-lg, 16px);
  /* Reuses the theme's always-dark inkwell pair rather than hardcoding a
     palette or inventing an inverse-surface token. CodeSurface/CodeText are
     deliberately non-inverting and contrast-tuned in both schemes, which is
     exactly what a transient overlay wants — and unlike literal hex, a themed
     app's toast now tracks its palette. */
  background: var(--color-code-surface, #18181B);
  color: var(--color-code-text, #E4E4E7);
  border-radius: var(--radii-md, 8px);
  font: var(--text-sm, 0.875rem) var(--font-body, system-ui, -apple-system, sans-serif);
  box-shadow: var(--shadow-lg, 0 10px 15px -3px rgba(0,0,0,0.10), 0 4px 6px -4px rgba(0,0,0,0.10));
  opacity: 0;
  transform: translateY(8px);
  transition: opacity var(--duration-fast, 150ms), transform var(--duration-fast, 150ms);
  pointer-events: none;
}
.cui-nav-toast.is-visible {
  opacity: 1;
  transform: translateY(0);
}
/* data-cui-flash-on-update: the runtime adds .cui-flash to a
   signal-bound element after each update and removes it after
   data-cui-flash-duration-ms. The highlight fades from a primary tint
   back to the element's own background. Motion-averse users get no
   animation (the runtime also skips the class for them). */
@media (prefers-reduced-motion: no-preference) {
  .cui-flash {
    animation: cui-flash var(--duration-slow, 400ms) var(--easing-ease-out, ease-out);
  }
}
@keyframes cui-flash {
  from { background-color: color-mix(in srgb, var(--color-primary, #18181B) 25%, transparent); }
}
/* Progress indicator on slow SPA navigation. The bar tracks the theme's
   primary color via the token — a literal hex here would keep the bar
   indigo in a teal app, and would ignore a per-request theme variant
   entirely (the whole point of app.css being theme-addressed). The
   fallback only applies if a host serves this CSS with no :root tokens. */
html[aria-busy="true"] {
  cursor: progress;
}
`

const frameworkProgressStripCSS = `html[aria-busy="true"]::after {
  content: '';
  position: fixed;
  inset: 0 0 auto 0;
  height: var(--ui-nav-progress-height, 2px);
  background: linear-gradient(90deg, transparent, currentColor 50%, transparent);
  animation: fui-nav-progress 1s linear infinite;
  z-index: calc(var(--z-toast, 500) + 1);
  pointer-events: none;
  color: var(--color-primary, #18181B);
}
/* The sweep runs in the reading direction. */
html[aria-busy="true"]:dir(rtl)::after { animation-direction: reverse; }
@keyframes fui-nav-progress {
  0% { transform: translateX(-100%); }
  100% { transform: translateX(100%); }
}`

const frameworkPageLoadingCSS = `[data-fui-page-loading] {
  visibility: hidden;
  opacity: 0;
  transition: opacity var(--duration-fast, 150ms) var(--easing-ease-in-out, ease), visibility var(--duration-fast, 150ms);
  pointer-events: none;
}
html[aria-busy="true"] [data-fui-page-loading] {
  visibility: visible;
  opacity: 1;
  pointer-events: auto;
}`

const frameworkDimCSS = `
/* the navigator also marks every outlet,
   area and swap slot the in-flight navigation will change with
   aria-busy="true"; dim those regions so the user sees WHICH parts of
   the kept layout are about to change. The delay lives here, not in
   JS: a response that lands before the delay elapses cancels the
   transition before a single dimmed frame paints, so fast (<100ms)
   navigations never flicker, while the clear restores opacity with no
   delay (the busy rule stops matching). 
   a region holding cloned loading content (data-cui-loadstate) is
   NOT dimmed — the loading content replaces the old content, dimming
   it would double the signal. */
[data-cui-outlet], [data-cui-area], [data-cui-layout-slot] {
  transition: opacity var(--duration-fast, 150ms) var(--easing-ease-in-out, ease);
}
[data-cui-outlet][aria-busy="true"],
[data-cui-area][aria-busy="true"],
[data-cui-layout-slot][aria-busy="true"] {
  opacity: var(--opacity-muted, 0.6);
  transition-delay: var(--duration-fast, 150ms);
}
[data-cui-outlet][data-cui-loadstate],
[data-cui-area][data-cui-loadstate],
[data-cui-layout-slot][data-cui-loadstate] {
  opacity: 1;
  transition-delay: 0s;
}
/* the loading content's default enter
   and exit — a fade. Author CSS overrides either side by keying the
   same state attributes from a component's registered style; the
   runtime waits for the region's own animationend (capped at 400ms)
   before replacing exit content, so a longer exit delays the swap and
   an absent one costs nothing. */
[data-cui-outlet][data-cui-loadstate="shown"],
[data-cui-area][data-cui-loadstate="shown"],
[data-cui-layout-slot][data-cui-loadstate="shown"] {
  animation: fui-load-in var(--duration-fast, 150ms) var(--easing-ease-in-out, ease) both;
}
[data-cui-outlet][data-cui-loadstate="exit"],
[data-cui-area][data-cui-loadstate="exit"],
[data-cui-layout-slot][data-cui-loadstate="exit"] {
  animation: fui-load-out var(--duration-fast, 150ms) var(--easing-ease-in-out, ease) both;
}
@keyframes fui-load-in { from { opacity: 0; } }
@keyframes fui-load-out { to { opacity: 0; } }
`

// ActiveTheme returns App.Theme, or the default theme when it is unset.
func (ds *UIHost) ActiveTheme() style.Theme {
	return ds.activeTheme()
}

// ComponentCSSFiles maps each registered component's sheet path
// ("/__gofastr/comp/<name>.css") to its CSS under the app theme.
func (ds *UIHost) ComponentCSSFiles() map[string]string {
	all := registry.All()
	if len(all) == 0 {
		return nil
	}
	theme := ds.activeTheme()
	out := make(map[string]string, len(all))
	for _, e := range all {
		out["/__gofastr/comp/"+e.Name+".css"] = e.ServedCSSFor(theme)
	}
	return out
}

func (ds *UIHost) handleAppCSS(w http.ResponseWriter, r *http.Request) {
	if hash := r.URL.Query().Get("t"); hash != "" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		if t, ok := ds.themeVariant(hash); ok {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			fmt.Fprint(w, minify.ServedCSS(ds.AppCSSFor(t)))
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		body, _ := ds.appCSSCached()
		fmt.Fprint(w, body)
		return
	}

	body, hash := ds.appCSSCached()
	serveVersionedText(w, r, "text/css; charset=utf-8", body, hash, false)
}

func (ds *UIHost) appCSSCached() (body, hash string) {
	ds.appCSSOnce.Do(func() {
		ds.appCSSContribN = style.ContributedCount()
		ds.appCSSLayoutsSeen.Store(ds.App.LayoutsVersion())
		ds.warnSheetlessTransitions()
		ds.appCSSBody = minify.ServedCSS(ds.AppCSS())
		ds.appCSSHash = style.CSSFingerprint(ds.appCSSBody)
	})
	if style.ContributedCount() != ds.appCSSContribN {
		ds.appCSSFrozenWarn.Do(func() {
			slog.Warn("uihost: style.Contribute called after the first page render: app.css is frozen per process; contribute at package init, before Mount")
		})
	}
	if gen := ds.App.LayoutsVersion(); gen != ds.appCSSLayoutsSeen.Load() {
		ds.appCSSLayoutsSeen.Store(gen)
		ds.warnSheetlessTransitions()
	}
	return ds.appCSSBody, ds.appCSSHash
}

// warnSheetlessTransitions names, once each, the layouts whose
// transitions have no sheet: NewLayout ran after the component catalog
// froze, so their transitions do not run.
func (ds *UIHost) warnSheetlessTransitions() {
	ds.transitionWarnMu.Lock()
	defer ds.transitionWarnMu.Unlock()
	for _, l := range ds.App.Layouts() {
		if l.TransitionSheet() != "" || l.TransitionCSS() == "" || ds.transitionWarned[l] {
			continue
		}
		if ds.transitionWarned == nil {
			ds.transitionWarned = map[*app.Layout]bool{}
		}
		ds.transitionWarned[l] = true
		slog.Warn("uihost: layout built after the first page render froze the component catalog, so its transitions do not run; build layouts before Mount",
			"layout", l.Name)
	}
}

func (ds *UIHost) handleColorSchemeJS(w http.ResponseWriter, r *http.Request) {
	js, err := runtime.ColorSchemeJS()
	if err != nil {
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	serveVersionedText(w, r, "application/javascript; charset=utf-8", js, colorSchemeHash(), false)
}

// colorSchemeMeta opens every page head: uihost/color-scheme-meta-is-static.
const colorSchemeMeta = `<meta name="color-scheme" content="light dark">`

var (
	colorSchemeHashOnce sync.Once
	colorSchemeHashVal  string
)

func colorSchemeHash() string {
	colorSchemeHashOnce.Do(func() {
		if js, err := runtime.ColorSchemeJS(); err == nil {
			colorSchemeHashVal = style.CSSFingerprint(js)
		}
	})
	return colorSchemeHashVal
}

func (ds *UIHost) componentCSSTags(page string, bundle bool, always ...string) string {
	names, held := pageSheetNames(page, always...)
	if len(names) == 0 && len(held) == 0 {
		return ""
	}
	theme := ds.activeTheme()
	th := style.ThemeHash(theme)
	links := ds.stylesheetLinks(names, bundle, theme, th)
	prefetch := prefetchLinks(held, setOf(names), theme, th)
	if links != "" && prefetch != "" {
		return links + "\n" + prefetch
	}
	return links + prefetch
}

func pageSheetNames(page string, always ...string) (names, held []string) {
	used, held := registry.ScanSplit(page)
	eager := registry.EagerNames()
	seen := setOf(used)
	for _, n := range eager {
		seen[n] = struct{}{}
	}
	for _, n := range always {
		seen[n] = struct{}{}
	}
	names = make([]string, 0, len(seen))
	for n := range seen {
		if _, ok := registry.Lookup(n); ok && safeComponentName(n) {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names, held
}

func setOf(names []string) map[string]struct{} {
	m := make(map[string]struct{}, len(names))
	for _, n := range names {
		m[n] = struct{}{}
	}
	return m
}

func prefetchLinks(held []string, linked map[string]struct{}, theme style.Theme, th string) string {
	b := borrowBuilder()
	defer returnBuilder(b)
	for _, n := range held {
		if _, ok := linked[n]; ok || !safeComponentName(n) {
			continue
		}
		e, ok := registry.Lookup(n)
		if !ok {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(b, `<link rel="prefetch" href="/__gofastr/comp/%s.css?v=%s">`, n, e.VersionForHash(theme, th))
	}
	return b.String()
}

func (ds *UIHost) stylesheetLinks(names []string, bundle bool, theme style.Theme, th string) string {
	if len(names) == 0 {
		return ""
	}
	if len(names) == 1 || !bundle {
		b := borrowBuilder()
		defer returnBuilder(b)
		for i, n := range names {
			e, ok := registry.Lookup(n)
			if !ok {
				continue
			}
			if !safeComponentName(n) {
				continue
			}
			if i > 0 {
				b.WriteByte('\n')
			}
			fmt.Fprintf(b, `<link rel="stylesheet" href="/__gofastr/comp/%s.css?v=%s" data-cui-style="%s" id="cui-css-%s">`,
				n, e.VersionForHash(theme, th), n, n)
		}
		return b.String()
	}
	return fmt.Sprintf(`<link rel="stylesheet" href="%s" data-cui-bundle="%s">`,
		liveSheetURL(names, theme, th), strings.Join(names, ","))
}

func liveSheetURL(names []string, theme style.Theme, th string) string {
	if len(names) == 1 {
		e, ok := registry.Lookup(names[0])
		if !ok || !safeComponentName(names[0]) {
			return ""
		}
		return "/__gofastr/comp/" + names[0] + ".css?v=" + e.VersionForHash(theme, th)
	}
	versions := make([]string, 0, len(names))
	for _, n := range names {
		e, ok := registry.Lookup(n)
		if !ok {
			continue
		}
		versions = append(versions, e.VersionForHash(theme, th))
	}
	return "/__gofastr/comp-bundle.css?names=" + strings.Join(names, ",") + "&v=" + hashStrings(versions...)
}

func safeComponentName(n string) bool {
	if n == "" {
		return false
	}
	for _, r := range n {
		if !(r == '_' || r == ':' || r == '.' || r == '-' ||
			r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

func (ds *UIHost) handleComponentCSS(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/__gofastr/comp/")
	name = strings.TrimSuffix(name, ".css")
	if !validNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	e, ok := registry.Lookup(name)
	if !ok {
		http.NotFound(w, r)
		return
	}
	theme := ds.requestTheme(r)
	css := e.ServedCSSFor(theme)
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	if v := r.URL.Query().Get("v"); v != "" && v == e.VersionFor(theme) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	fmt.Fprint(w, css)
}

func (ds *UIHost) handleCompBundleCSS(w http.ResponseWriter, r *http.Request) {
	namesParam := r.URL.Query().Get("names")
	if namesParam == "" {
		http.NotFound(w, r)
		return
	}
	names := strings.Split(namesParam, ",")
	theme := ds.requestTheme(r)
	th := style.ThemeHash(theme)
	versions := make([]string, 0, len(names))
	bodies := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	skipped := false
	for _, n := range names {
		n = strings.TrimSpace(n)
		if !validNameRe.MatchString(n) {
			http.NotFound(w, r)
			return
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		e, ok := registry.Lookup(n)
		if !ok {
			skipped = true
			continue
		}
		versions = append(versions, e.VersionForHash(theme, th))
		bodies = append(bodies, e.ServedCSSForHash(theme, th))
	}
	if len(bodies) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	wantV := hashStrings(versions...)
	if v := r.URL.Query().Get("v"); v != "" && v == wantV && !skipped {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	for _, body := range bodies {
		fmt.Fprint(w, body)
		fmt.Fprint(w, "\n")
	}
}

func (ds *UIHost) activeTheme() style.Theme {
	if ds.App != nil && ds.App.Theme != nil {
		return *ds.App.Theme
	}
	return style.DefaultTheme()
}

func loadModeString(m registry.LoadMode) string {
	switch m {
	case registry.LoadAlways:
		return "always"
	case registry.LoadPrewarm:
		return "prewarm"
	default:
		return "auto"
	}
}

func hashStrings(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:6])
}

// ReadCustomCSSFile returns the content of the file at path, or "" when it
// cannot be read.
func ReadCustomCSSFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
