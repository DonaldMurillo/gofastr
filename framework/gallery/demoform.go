package gallery

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// DemoFormPath is the route every catalog demo form posts to, naming its
// demo in the query (DemoFormPath + "?demo=<slug>").
const DemoFormPath = "/__gallery/demo-form"

func demoFormAction(slug string) string { return DemoFormPath + "?demo=" + slug }

// MountDemoFormRoute registers DemoFormPath on routes: a POST that keeps
// nothing and answers 303 See Other to demoPage(slug) for the demo its
// ?demo= query names, or 404 for a name the catalog does not hold. It
// panics when demoPage returns anything but a local path for a slug.
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

func localPath(p string) bool {
	if len(p) == 0 || p[0] != '/' || strings.HasPrefix(p, "//") || strings.HasPrefix(p, `/\`) {
		return false
	}
	u, err := url.Parse(p)
	return err == nil && u.Scheme == "" && u.Host == ""
}
