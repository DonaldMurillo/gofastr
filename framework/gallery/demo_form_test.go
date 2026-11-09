package gallery

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

var (
	formTagRE   = regexp.MustCompile(`(?is)<form\b[^>]*>`)
	formAttrRE  = regexp.MustCompile(`(?is)\s([a-z-]+)="([^"]*)"`)
	componentAt = func(slug string) string { return "/components/" + slug }
)

func demoFormRoutes(t *testing.T) *router.Router {
	t.Helper()
	routes := router.New()
	MountDemoFormRoute(routes, componentAt)
	return routes
}

func postDemoForm(routes *router.Router, action string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, action, strings.NewReader("email=demo%40example.test&password=example-password"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	routes.ServeHTTP(rec, req)
	return rec
}

// Every POST form a catalog demo renders answers 303 back to its demo.
// They posted to the page itself ("?") or to a docs-site route, so a
// screen answered 405, or a host other than the docs site answered 404.
func TestDemoFormsRedirectToDemo(t *testing.T) {
	routes := demoFormRoutes(t)
	// The docs site serves a real wizard at this path; the demo walks it.
	hostOwned := map[string]bool{"/forms/wizard": true}
	posted := 0
	for _, e := range Catalog {
		for _, tag := range formTagRE.FindAllString(string(e.Demo()), -1) {
			attrs := map[string]string{}
			for _, m := range formAttrRE.FindAllStringSubmatch(tag, -1) {
				attrs[strings.ToLower(m[1])] = html.UnescapeString(m[2])
			}
			if strings.ToUpper(attrs["method"]) != http.MethodPost || hostOwned[attrs["action"]] {
				continue
			}
			posted++
			// Resolve the action the way the browser does, against the
			// demo's own page: "?" posts to the page itself.
			action, err := url.Parse(attrs["action"])
			if err != nil {
				t.Fatalf("%s: form action %q: %v", e.Slug, attrs["action"], err)
			}
			base, _ := url.Parse(componentAt(e.Slug))
			rec := postDemoForm(routes, base.ResolveReference(action).String())
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != componentAt(e.Slug) {
				t.Errorf("%s: form action %q answered %d Location %q, want 303 to %q",
					e.Slug, attrs["action"], rec.Code, rec.Header().Get("Location"), componentAt(e.Slug))
			}
		}
	}
	// form, conditionalfield, formrepeater, repeater, selection.
	if posted < 5 {
		t.Fatalf("found %d POST demo forms, want at least 5", posted)
	}
}

// The route redirects only to a catalog demo's page: a demo name it
// does not know, a URL in its place, or no name answers 404.
func TestDemoFormRefusesUnknownDemo(t *testing.T) {
	routes := demoFormRoutes(t)
	for _, demo := range []string{"", "no-such-demo", "https://evil.example", "//evil.example", "/components/form"} {
		rec := postDemoForm(routes, DemoFormPath+"?demo="+demo)
		if rec.Code != http.StatusNotFound || rec.Header().Get("Location") != "" {
			t.Errorf("demo=%q answered %d Location %q, want 404 and no Location", demo, rec.Code, rec.Header().Get("Location"))
		}
	}
}

// A host page that is not a local path is refused at mount, so the
// route can never send the browser to another origin.
func TestDemoFormRouteRefusesForeignPage(t *testing.T) {
	for _, page := range []string{"https://evil.example/x", "//evil.example/x", `/\evil.example`, "components/x", ""} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("demo page %q was accepted", page)
				}
			}()
			MountDemoFormRoute(router.New(), func(string) string { return page })
		}()
	}
}
