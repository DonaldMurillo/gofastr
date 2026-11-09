package gallery

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// DemoFormPath is the route every catalog demo form posts to, naming
// its demo in the query (DemoFormPath + "?demo=<slug>"). MountDemoFormRoute
// registers it.
const DemoFormPath = "/__gallery/demo-form"

// demoFormAction is the action a catalog demo's form posts to.
func demoFormAction(slug string) string { return DemoFormPath + "?demo=" + slug }

// MountDemoFormRoute registers DemoFormPath on routes: a POST that keeps
// nothing (the body is never read) and answers 303 See Other to the page
// that shows the demo named by its ?demo= query, so submitting a demo
// form lands back on the demo with no password in a URL. demoPage maps a
// catalog slug to that page: "/components/<slug>" on the docs site, the
// one preview page in the theme editor. A demo name the catalog does not
// hold answers 404.
//
// Every page demoPage returns must be a local path ("/…", never "//…" or
// "/\…"), so the redirect stays on the host's origin; MountDemoFormRoute
// asks for each slug once and panics on any other value. Call it once
// per router, beside MountDemoWidgets, before mounting the UIHost.
func MountDemoFormRoute(routes *router.Router, demoPage func(slug string) string) {
	pages := make(map[string]string, len(Catalog))
	for _, e := range Catalog {
		page := demoPage(e.Slug)
		if !localPath(page) {
			panic("gallery: demo page for " + e.Slug + " must be a local path, got " + page)
		}
		pages[e.Slug] = page
	}
	routes.Post(DemoFormPath, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, ok := pages[r.URL.Query().Get("demo")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		http.Redirect(w, r, page, http.StatusSeeOther)
	}))
}

// localPath reports whether p is a path on the current origin: it starts
// with one slash, the next byte is neither a slash nor a backslash (which
// browsers read as a host), and it carries no control byte.
func localPath(p string) bool {
	if len(p) == 0 || p[0] != '/' || strings.HasPrefix(p, "//") || strings.HasPrefix(p, `/\`) {
		return false
	}
	for i := 0; i < len(p); i++ {
		if p[i] < 0x20 || p[i] == 0x7f {
			return false
		}
	}
	u, err := url.Parse(p)
	return err == nil && u.Scheme == "" && u.Host == ""
}
