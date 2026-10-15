package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/sitesearch"
)

// The first result a reader expects for what they would type into the
// header palette, over the site's real index. A page titled with the
// query is the answer: a body that repeats the word, or a longer title
// that only contains it, ranks below. Add the query a reader complains
// about here before changing the ranking.
func TestSearchTopResults(t *testing.T) {
	app := newTestApp(t)
	for q, want := range map[string]string{
		"form":            "/components/form",
		"forms":           "/components/form",
		"table":           "/components/datatable",
		"data table":      "/components/datatable",
		"datatable":       "/components/datatable",
		"stack":           "/components/stack",
		"cli":             "/docs/cli",
		"sidebar":         "/components/sidebar",
		"modal":           "/components/modal",
		"button":          "/components/button",
		"card":            "/components/card",
		"hero":            "/components/hero",
		"toast":           "/components/toast",
		"tabs":            "/components/tabs",
		"palette":         "/components/commandpalette",
		"command palette": "/components/commandpalette",
		"auth":            "/docs/auth",
		"migrations":      "/docs/migrations",
		"theming":         "/docs/theming",
		"theme":           "/docs/theming",
		"search":          "/docs/search",
		"site search":     "/docs/site-search",
		"static export":   "/docs/static-export",
		"deploy":          "/docs/deploy",
		"dark mode":       "/docs/theming",
		"pal":             "/components/commandpalette",
		"datat":           "/components/datatable",
	} {
		rec := httptest.NewRecorder()
		app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, sitesearch.Root+"query?limit=3&q="+url.QueryEscape(q), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%q: %d %s", q, rec.Code, rec.Body)
		}
		var body struct{ Results []sitesearch.Result }
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range body.Results {
			got = append(got, r.URL)
		}
		if len(got) == 0 || got[0] != want {
			t.Errorf("%q: top results %s, want %s first", q, strings.Join(got, " "), want)
		}
	}
}
