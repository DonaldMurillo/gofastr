package uihost

import (
	"fmt"
	stdhtml "html"
	"log/slog"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Option configures a UIHost.
type Option func(*UIHost)

// WithCustomCSS adds extra CSS to every page.
func WithCustomCSS(css string) Option {
	return func(ds *UIHost) {
		ds.customCSS = css
	}
}

// WithExtraScripts adds deferred same-origin <script src> URLs to every full
// page; a src that fails RegisterExternalScript's grammar is dropped with a warning.
func WithExtraScripts(urls ...string) Option {
	return func(ds *UIHost) {
		for _, u := range urls {
			if err := ds.registerScript(externalScript{src: u}, "WithExtraScripts"); err != nil {
				slog.Default().Warn("uihost: script dropped from the page", "err", err)
			}
		}
	}
}

// WithHeadHTML injects scrubbed raw HTML into every page's <head>; prefer the
// typed head options. A description meta in it is the site description.
func WithHeadHTML(html string) Option {
	return func(ds *UIHost) {
		ds.headHTML = html
	}
}

// WithLang sets the document language of host-built shells (404, PWA offline,
// embed), overriding the app's Lang.
func WithLang(lang string) Option {
	return func(ds *UIHost) { ds.lang = lang }
}

// WithLangFunc sets a per-route language for host-built shells; "" for a path
// falls back to the app's LangFunc, then WithLang.
func WithLangFunc(fn func(path string) string) Option {
	return func(ds *UIHost) { ds.langFunc = fn }
}

// WithNotFoundScreen renders c, through the default layout, for a request no
// screen, static file or favicon matches.
func WithNotFoundScreen(c component.Component) Option {
	app.RefuseRetiredScreenMethods("not-found screen", c)
	return func(ds *UIHost) {
		ds.notFoundScreen = c
	}
}

// WithErrorScreen renders c, through the default layout and with no error text,
// for a whole-page resolver failure (app.PageError).
func WithErrorScreen(c component.Component) Option {
	app.RefuseRetiredScreenMethods("error screen", c)
	return func(ds *UIHost) {
		ds.errorScreen = c
	}
}

// WithPageLoading renders c once into every full page, shown while a
// navigation is in flight, in place of the default progress strip.
func WithPageLoading(c component.Component) Option {
	return func(ds *UIHost) {
		ds.pageLoading = c
	}
}

// WithNoLiveChannel omits the gofastr-sse meta, so the runtime opens no
// island EventSource.
func WithNoLiveChannel() Option {
	return func(ds *UIHost) { ds.noLiveChannel = true }
}

// WithTrustedProxies names the proxies whose X-Forwarded-For the island stream
// cap and whose X-Forwarded-Proto the absolute discovery URLs trust. It
// replaces, not extends, the App's AppConfig.TrustedProxies for this host.
func WithTrustedProxies(entries ...string) Option {
	return func(ds *UIHost) { ds.trustedProxies = append(ds.trustedProxies, entries...) }
}

// WithFavicon adds a <link rel="icon">; the host answers 204 at href while no
// static file is there.
func WithFavicon(href string) Option {
	return func(ds *UIHost) {
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

// WithDescription sets the description of a page that names none of its own.
func WithDescription(desc string) Option {
	return func(ds *UIHost) {
		ds.siteDescription = desc
	}
}

// WithRobotsMeta adds a sitewide <meta name="robots">, emitted after any
// per-screen robots directive.
func WithRobotsMeta(directives string) Option {
	return func(ds *UIHost) {
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<meta name="robots" content="%s">`, stdhtml.EscapeString(directives)))
	}
}

// WithOpenGraph adds the non-empty Open Graph tags; an unsafe Image or URL is
// dropped.
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

// WithTwitterCard adds the non-empty Twitter Card tags; an unsafe Image is
// dropped.
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

// WithCanonicalURL adds a <link rel="canonical">; an unsafe URL is dropped.
func WithCanonicalURL(url string) Option {
	return func(ds *UIHost) {
		if !isSafeHeadURL(url) {
			return
		}
		ds.headTags = append(ds.headTags, fmt.Sprintf(`<link rel="canonical" href="%s">`, stdhtml.EscapeString(url)))
	}
}

// WithPreconnect adds a <link rel="preconnect"> per safe origin.
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

// WithFontPreload adds a <link rel="preload" as="font"> per web font, so
// the browser fetches the woff2 files FontFaceCSS(dir, fonts...) names
// with the page instead of after the stylesheet: the swap from the
// fallback lands sooner. Preload only the faces above the fold; a font
// whose URL fails the head-URL allow-list is dropped.
func WithFontPreload(dir string, fonts ...style.WebFont) Option {
	return func(ds *UIHost) {
		seen := map[string]bool{}
		for _, f := range fonts {
			// FontFaceCSS keeps the first rule per family; preload only that file.
			if !f.Declared() || seen[f.FaceFamily()] {
				continue
			}
			seen[f.FaceFamily()] = true
			href := style.FontURL(dir, f)
			if !isSafeHeadURL(href) {
				continue
			}
			ds.headTags = append(ds.headTags, fmt.Sprintf(`<link rel="preload" as="font" type="font/woff2" href="%s" crossorigin>`, stdhtml.EscapeString(href)))
		}
	}
}

// WithStaticDir sets the directory to serve static files from.
func WithStaticDir(dir string) Option {
	return func(ds *UIHost) {
		ds.staticDir = dir
	}
}

// WithPublicLLMMD mounts /llm-pages.md and each screen's llm.md.
func WithPublicLLMMD() Option {
	return func(ds *UIHost) {
		ds.llmMDPublic = true
	}
}
