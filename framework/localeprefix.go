package framework

import (
	"cmp"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// WithLocalePrefixes serves every catalog locale but the fallback under
// /<tag>/, so "/es/pricing" is the pricing page in Spanish; the prefix
// wins over every other locale source, and links in a prefixed response
// keep it. NewApp panics without WithI18n, with WithLocaleResolver, or
// when a locale cannot be a path segment; Start panics on a route whose
// first segment names a locale.
func WithLocalePrefixes() AppOption {
	return func(a *App) {
		a.localePrefixes = true
	}
}

func (a *App) installLocalePrefixes() {
	if a.translator == nil {
		panic("framework: WithLocalePrefixes requires WithI18n: the locales come from the translator's catalog")
	}
	if a.localeResolver.Resolve != nil {
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

func routeLocale(set i18n.PathLocales) func(*http.Request) (*http.Request, string) {
	return func(r *http.Request) (*http.Request, string) {
		tag, rest := set.Split(r.URL.Path)
		if tag == set.Default {
			target := escapedRest(r.URL)
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
				r2.Header.Del("X-Gofastr-From")
			}
		}
		if tag == "" {
			return r2, ""
		}
		u := *r.URL
		u.Path = rest
		if u.RawPath != "" {
			u.RawPath = escapedRest(r.URL)
		}
		r2.URL = &u
		return r2, ""
	}
}

func escapedRest(u *url.URL) string {
	if esc := u.EscapedPath(); len(esc) > 1 {
		if i := strings.IndexByte(esc[1:], '/'); i >= 0 {
			return esc[1+i:]
		}
	}
	return "/"
}

func (a *App) pathLocaleFor(r *http.Request) *http.Request {
	if !a.localePrefixes {
		return r
	}
	tag, _ := a.pathLocales.Split(r.URL.Path)
	return r.WithContext(i18n.WithPathLocale(r.Context(), a.pathLocales, cmp.Or(tag, a.pathLocales.Default)))
}

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
