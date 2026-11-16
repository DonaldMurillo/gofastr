package uihost

import (
	"fmt"
	stdhtml "html"
	"log/slog"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// Option configures a UIHost.
type Option func(*UIHost)

// WithCustomCSS adds extra CSS to inject into every page.
func WithCustomCSS(css string) Option {
	return func(ds *UIHost) {
		ds.customCSS = css
	}
}

// WithExtraScripts adds external <script src="…"> URLs to every full
// page, emitted deferred at the end of <head> after runtime.js and the
// per-screen action scripts (the same rail RegisterExternalScript
// writes). Use for dev-only tooling like livereload. CSP-safe: every URL
// becomes an external resource, no inline JS.
//
// Each src is held to RegisterExternalScript's same-origin grammar
// ("/x.js", optionally "/x.js?v=abc"): the tag loads under the default
// CSP's script-src 'self'. A src that fails it (a scheme, a host, a
// protocol-relative "//cdn…", a backslash, a control byte, a fragment, a
// "." or ".." segment) is dropped with a warning, the way the URL-typed
// head options (WithFavicon, WithPreconnect) drop an unsafe URL.
func WithExtraScripts(urls ...string) Option {
	return func(ds *UIHost) {
		for _, u := range urls {
			if err := ds.registerScript(externalScript{src: u}, "WithExtraScripts"); err != nil {
				slog.Default().Warn("uihost: script dropped from the page", "err", err)
			}
		}
	}
}

// WithHeadHTML injects raw HTML into every page's <head>. This is the
// escape hatch for head content the typed options do not cover. It is
// scrubbed, not injected verbatim: <script>, <style>, <iframe>, <svg> and
// the other active tag families, <base>/<img>/<embed>, meta-refresh,
// on*= handlers, autofocus and script-pulling <link>s are removed, and the
// scrub runs until its output stops changing, so a removal cannot splice
// a forbidden tag back together. HTML still changing after the pass cap
// (nesting built to outlast the scrub) is dropped whole and logged once.
// For safe, auto-escaped alternatives, see WithFavicon,
// WithThemeColor, WithDescription, WithOpenGraph, WithTwitterCard,
// WithCanonicalURL, and WithPreconnect.
//
// A <meta name="description"> tag in html is the site description, as
// with WithDescription: it moves into the per-page head and gives way to
// a screen's own description, so a page never carries two.
func WithHeadHTML(html string) Option {
	return func(ds *UIHost) {
		ds.headHTML = html
	}
}

// WithLang sets the document language (BCP-47 tag, e.g. "en", "fr", "pt-BR")
// for the host-emitted document shells: the 404, PWA offline, and embed
// shells that the host builds directly rather than through app.RenderPage. It
// overrides the app's own Lang ([app.App.WithLang]); both default to "en".
func WithLang(lang string) Option {
	return func(ds *UIHost) { ds.lang = lang }
}

// WithLangFunc sets a per-route document language for the host-emitted shells,
// so a 404 under /es/ is tagged Spanish instead of inheriting the site
// language. It is the host-shell counterpart of [app.App.WithLangFunc], which
// is what tags the app's own pages; a host that only sets one should set that
// one. Returning "" for a path falls back to the app's LangFunc, then to
// WithLang / [app.App.Lang] / "en", so an unconfigured host is unchanged.
func WithLangFunc(fn func(path string) string) Option {
	return func(ds *UIHost) { ds.langFunc = fn }
}

// WithNotFoundScreen overrides the default bare 404 fallback. When a
// request misses every registered screen, static file, and configured
// favicon, the host renders this component through the active layout,
// so the 404 page sees the same nav/footer chrome every other page
// gets. The component renders as any component does (RenderNotFound
// first, then RenderCtx, then Render) inside the default layout; an app
// with no root layout gets it in the framework's bare <main>.
func WithNotFoundScreen(c component.Component) Option {
	app.RefuseRetiredScreenMethods("not-found screen", c)
	return func(ds *UIHost) {
		ds.notFoundScreen = c
	}
}

// WithErrorScreen overrides the default bare 500 fallback for a
// whole-page resolver failure (app.PageError, :
// the component renders through the active layout's root, with the
// same chrome every page gets and NO error text of its own — the
// error is logged by the host, the page is a product surface.
func WithErrorScreen(c component.Component) Option {
	app.RefuseRetiredScreenMethods("error screen", c)
	return func(ds *UIHost) {
		ds.errorScreen = c
	}
}

// WithPageLoading sets the page-wide loading component: rendered once
// into every full page inside an inert <div data-fui-page-loading>
// (hidden until a navigation is in flight) and shown through the SAME
// state carrier the default progress strip keys off, html[aria-busy],
// in pure CSS. Setting it REPLACES the default strip (the
// html[aria-busy]::after rules are dropped from app.css so the two
// never stack).
//
// The component is presentational config (rendered once, no Load, no
// DI): style it in its registered component CSS, including any entry
// delay (an After equivalent is a transition-delay on the hidden
// state). An After/Min pair is NOT expressible in pure CSS and is not
// offered here: the per-outlet Loading declaration owns the anti-flash
// contract where it matters (the region swap); holding the page-wide
// flag would also hold aria-busy, which assistive tech should hear
// for exactly as long as the navigation is really in flight.
func WithPageLoading(c component.Component) Option {
	return func(ds *UIHost) {
		ds.pageLoading = c
	}
}

// WithFavicon adds a <link rel="icon"> tag to <head>. The host also
// auto-serves 204 No Content at the configured URL when no static file
// matches, so a host that ships no favicon doesn't 404 on every page
// load. Place a real file at the path in staticDir / staticFS to override.
// WithNoLiveChannel omits the <meta name="gofastr-sse"> tag, so the
// runtime never opens the island EventSource. For hosts that use no
// live islands and must keep pages network-idle-able: a long-lived
// EventSource counts as an in-flight request forever, which blocks
// waitForLoadState('networkidle') / waitUntil:'networkidle' — a common
// e2e pattern (60 spec files in the metacollector port alone).
func WithNoLiveChannel() Option {
	return func(ds *UIHost) { ds.noLiveChannel = true }
}

// WithTrustedProxies names the reverse proxies (IPs or CIDRs) in front of
// the host, so the island stream cap counts an anonymous visitor under the
// X-Forwarded-For address the proxy saw instead of the proxy's own. Without
// it, behind a proxy, every anonymous visitor shares the proxy address's
// budget of island.DefaultStreamsPerPeer streams. A header from a peer not
// in the list is ignored. See island.WithTrustedProxies.
//
// The same list decides whose X-Forwarded-Proto sets the scheme of the
// agent card's and the Link headers' absolute URLs when no BaseURL is
// configured. Behind a TLS-terminating proxy, list the proxy or set a
// BaseURL, or those URLs say http://.
//
// Without this option the host reads the list a framework.App puts on
// each request's context, its AppConfig.TrustedProxies, for both uses. A
// list given here replaces the App's for this host; it is not added to
// it.
func WithTrustedProxies(entries ...string) Option {
	return func(ds *UIHost) { ds.trustedProxies = append(ds.trustedProxies, entries...) }
}

func WithFavicon(href string) Option {
	return func(ds *UIHost) {
		// Same head-URL allow-list as every URL-typed head emitter: the
		// href is reflected into every page's <head>, a javascript:/data:
		// value there is executed by anything that follows the icon URL.
		if !isSafeHeadURL(href) {
			return
		}
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<link rel="icon" href="%s">`, stdhtml.EscapeString(href)))
		ds.faviconURL = href
	}
}

// WithThemeColor adds a <meta name="theme-color"> tag to <head>.
func WithThemeColor(color string) Option {
	return func(ds *UIHost) {
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="theme-color" content="%s">`, stdhtml.EscapeString(color)))
	}
}

// WithDescription sets the site's <meta name="description">: the one a
// page carries when it names no description of its own. A screen
// description (ScreenDescriber, ScreenSEO.Description) or a description
// tag in SEOScreen.HeadHTML replaces it, so a page never carries two. A
// description tag in WithHeadHTML is site-level too and takes
// precedence over this one.
func WithDescription(desc string) Option {
	return func(ds *UIHost) {
		ds.siteDescription = desc
	}
}

// WithRobotsMeta adds a sitewide <meta name="robots"> tag to <head>
// (e.g. "noindex" for a staging deploy). Per-screen directives via
// [ScreenRobots] or [SEO].Robots are emitted in addition to, and
// before, this global tag; crawlers apply the most restrictive
// combination. battery/sitesearch ignores this tag: a staging noindex
// keeps every page in the site's own search, and only a screen's own
// directive drops it.
func WithRobotsMeta(directives string) Option {
	return func(ds *UIHost) {
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="robots" content="%s">`, stdhtml.EscapeString(directives)))
	}
}

// WithOpenGraph adds Open Graph <meta property="og:..."> tags to <head>.
// Zero-value fields are omitted. URL-typed fields (Image, URL) are
// dropped if they fail the head-URL allow-list (http(s)/relative only).
// A `javascript:`/`data:` URL there is reflected XSS via any social
// preview crawler that auto-clicks the link.
func WithOpenGraph(og OG) Option {
	return func(ds *UIHost) {
		if og.Title != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:title" content="%s">`, stdhtml.EscapeString(og.Title)))
		}
		if og.Description != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:description" content="%s">`, stdhtml.EscapeString(og.Description)))
		}
		if og.Image != "" && isSafeHeadURL(og.Image) {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:image" content="%s">`, stdhtml.EscapeString(og.Image)))
		}
		if og.URL != "" && isSafeHeadURL(og.URL) {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:url" content="%s">`, stdhtml.EscapeString(og.URL)))
		}
		if og.Type != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta property="og:type" content="%s">`, stdhtml.EscapeString(og.Type)))
		}
	}
}

// WithTwitterCard adds Twitter Card <meta name="twitter:..."> tags to <head>.
// Zero-value fields are omitted. URL-typed fields are scheme-restricted
// per [WithOpenGraph].
func WithTwitterCard(tc TwitterCard) Option {
	return func(ds *UIHost) {
		if tc.Card != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:card" content="%s">`, stdhtml.EscapeString(tc.Card)))
		}
		if tc.Title != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:title" content="%s">`, stdhtml.EscapeString(tc.Title)))
		}
		if tc.Description != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:description" content="%s">`, stdhtml.EscapeString(tc.Description)))
		}
		if tc.Image != "" && isSafeHeadURL(tc.Image) {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:image" content="%s">`, stdhtml.EscapeString(tc.Image)))
		}
		if tc.Site != "" {
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="twitter:site" content="%s">`, stdhtml.EscapeString(tc.Site)))
		}
	}
}

// WithCanonicalURL adds a <link rel="canonical"> tag to <head>. Unsafe
// URLs (non-http(s)/relative) are dropped.
func WithCanonicalURL(url string) Option {
	return func(ds *UIHost) {
		if !isSafeHeadURL(url) {
			return
		}
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<link rel="canonical" href="%s">`, stdhtml.EscapeString(url)))
	}
}

// WithPreconnect adds <link rel="preconnect"> tags for the given origins.
// Use for early DNS/TCP/TLS connections to external resources (fonts, CDNs).
// Origins failing the head-URL allow-list are dropped, matching every other
// URL-typed head emitter.
func WithPreconnect(origins ...string) Option {
	return func(ds *UIHost) {
		for _, o := range origins {
			if !isSafeHeadURL(o) {
				continue
			}
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<link rel="preconnect" href="%s">`, stdhtml.EscapeString(o)))
		}
	}
}

// WithStaticDir sets the directory to serve static files from.
func WithStaticDir(dir string) Option {
	return func(ds *UIHost) {
		ds.staticDir = dir
	}
}

// WithPublicLLMMD opts the host into mounting the page-level LLM-friendly
// markdown routes (/llm-pages.md, /<screen>/llm.md). Disabled by default
// because the documents enumerate every screen and the data shape attached
// to it, useful for AI agents in trusted environments, schema disclosure
// elsewhere.
func WithPublicLLMMD() Option {
	return func(ds *UIHost) {
		ds.llmMDPublic = true
	}
}
