package framework

import (
	"cmp"
	"fmt"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// WithLocalePrefixes names the reader's language in the URL. The
// translator's fallback locale keeps the plain paths; every other
// catalog locale serves the same routes under /<tag>/, so "/es/pricing"
// is the pricing page in Spanish. The router reads the prefix off the
// path before matching (route patterns and screens never see it), the
// locale it names wins over every other source, and a request with no
// prefix is the default locale. "/en/pricing", for an English default,
// answers 308 to "/pricing".
//
// Responses keep the reader's locale: root-relative links, form
// actions, island and poll URLs and redirects in a "/es/" response gain
// the prefix on the way out (core-ui/urlprefix), and the UI host adds
// an hreflang alternate per locale to each page and to the sitemap.
// Link to another language with [i18n.LocalePath] on an anchor carrying
// hreflang.
//
// Panics without WithI18n, together with WithLocaleResolver (the URL
// must decide the language, or an hreflang="en" page could answer in
// French), when the catalog has a locale that cannot be a path segment,
// and at Start when a route's first segment names a locale.
func WithLocalePrefixes() AppOption {
	return func(a *App) {
		a.localePrefixes = true
	}
}

// installLocalePrefixes reads the locale set and installs the router's
// prefix split. NewApp calls it after every option has run.
func (a *App) installLocalePrefixes() {
	if a.translator == nil {
		panic("framework: WithLocalePrefixes requires WithI18n: the locales come from the translator's catalog")
	}
	if a.localeResolver != nil {
		panic("framework: WithLocalePrefixes is incompatible with WithLocaleResolver: the URL decides the language, " +
			"so a stored locale has nothing to choose. Send a reader to their language with a link built by i18n.LocalePath")
	}
	set, err := i18n.NewPathLocales(a.translator)
	if err != nil {
		panic("framework: WithLocalePrefixes: " + err.Error())
	}
	a.pathLocales = set
	a.router.PreRoute(routeLocale(set))
}

// routeLocale returns the router's pre-route hook for set: it strips a
// locale prefix off the request path and records the locale on the
// context, and redirects a default-locale prefix to the plain path.
func routeLocale(set i18n.PathLocales) func(*http.Request) (*http.Request, string) {
	return func(r *http.Request) (*http.Request, string) {
		tag, rest := set.Split(r.URL.Path)
		if tag == set.Default {
			// The target is the escaped path past its first segment, the
			// tag however it was spelled (/%65n/ is /en/ too; splitting
			// the escaped path missed it and answered with the request
			// itself), since a decoded %3F would start a query. Only a
			// same-site one: /en//evil.com must not answer //evil.com.
			target := "/"
			if esc := r.URL.EscapedPath(); len(esc) > 1 {
				if i := strings.IndexByte(esc[1:], '/'); i >= 0 {
					target = esc[1+i:]
				}
			}
			if !handler.IsSafeRelativePath(target) {
				return nil, "/"
			}
			if r.URL.RawQuery != "" {
				target += "?" + r.URL.RawQuery
			}
			return nil, target
		}
		r2 := r.WithContext(i18n.WithPathLocale(r.Context(), set, cmp.Or(tag, set.Default)))
		if from := r.Header.Get("X-Gofastr-From"); from != "" {
			fromTag, fromRest := set.Split(from)
			r2.Header = r.Header.Clone()
			switch {
			case fromTag == tag:
				if tag != "" {
					r2.Header.Set("X-Gofastr-From", fromRest)
				}
			default:
				// The page the reader came from is in another language:
				// nothing it kept on screen is chrome this page can reuse.
				r2.Header.Del("X-Gofastr-From")
			}
		}
		if tag == "" {
			return r2, ""
		}
		u := *r.URL
		u.Path = rest
		if raw, ok := strings.CutPrefix(u.RawPath, "/"+tag); ok && (raw == "" || raw[0] == '/') {
			u.RawPath = cmp.Or(raw, "/")
		} else {
			u.RawPath = ""
		}
		r2.URL = &u
		return r2, ""
	}
}

// pathLocaleFor returns r with the locale its path names on the
// context, without changing the URL: serveLocale's read of the split
// the router makes, for the server wrappers that run before it.
func (a *App) pathLocaleFor(r *http.Request) *http.Request {
	if !a.localePrefixes {
		return r
	}
	tag, _ := a.pathLocales.Split(r.URL.Path)
	return r.WithContext(i18n.WithPathLocale(r.Context(), a.pathLocales, cmp.Or(tag, a.pathLocales.Default)))
}

// checkLocaleRoutes refuses a route whose first segment names a locale:
// the router strips that segment before matching, so the route could
// never be reached. Start runs it once the route table is complete.
func (a *App) checkLocaleRoutes() {
	if !a.localePrefixes {
		return
	}
	var paths []string
	for _, rt := range a.router.Routes() {
		paths = append(paths, rt.Pattern)
	}
	for _, m := range a.mountables {
		if rp, ok := m.(interface{ RoutePatterns() []string }); ok {
			paths = append(paths, rp.RoutePatterns()...)
		}
	}
	for _, p := range paths {
		if tag, _ := a.pathLocales.Split(p); tag != "" {
			panic(fmt.Sprintf("framework: route %q starts with the locale segment %q: WithLocalePrefixes strips it "+
				"before routing, so the route is unreachable. Rename the route", p, tag))
		}
	}
}
