package uihost

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Alternates name the page they stand for: the path arrives decoded, so
// a %3F in it is a literal "?" that stays part of the path, escaped.
func TestLocaleLinksEscapePath(t *testing.T) {
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}
	var got []string
	for _, l := range localeLinks(set, "https://x.test", "/docs/a?b c") {
		got = append(got, l.Lang+" "+l.URL)
	}
	want := "en https://x.test/docs/a%3Fb%20c|es https://x.test/es/docs/a%3Fb%20c|x-default https://x.test/docs/a%3Fb%20c"
	if strings.Join(got, "|") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(got, "|"), want)
	}
}

// A language switcher in the chrome links /es/, which the router serves
// as / in Spanish; strict boot must resolve it as /. A prefixed link to
// a page nobody serves is still a dead link.
func TestStrictLinkUnderLocalePrefix(t *testing.T) {
	ds := linkHost(t, strictSiteOptions, chromeFooter(
		footerLink{Label: "Español", Href: "/es/"},
		footerLink{Label: "Términos", Href: "/es/terms"},
	))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	msg := bootCheckPanic(t, ds, nil)
	if !strings.Contains(msg, "/es/terms") {
		t.Fatalf("the dead /es/terms passed strict boot:\n%s", msg)
	}
	if strings.Contains(msg, `"/es/"`) || strings.Contains(msg, "/es/ ") {
		t.Errorf("the switcher's /es/ was flagged:\n%s", msg)
	}
}

// The meta refresh is no link the page rewriter reads, so a stale tab
// on /es/a must be sent back to /es/a, not to the English page.
func TestStaleMarkupReloadKeepsPrefix(t *testing.T) {
	set := i18n.PathLocales{Default: "en", Prefixed: []string{"es"}}
	r := httptest.NewRequest("GET", "/a?x=1", nil)
	r = r.WithContext(i18n.WithPathLocale(r.Context(), set, "es"))
	w := httptest.NewRecorder()
	writeStaleMarkupReload(w, r, "/a", "")
	if !strings.Contains(w.Body.String(), `url='/es/a?x=1'`) {
		t.Errorf("refresh lost the locale prefix:\n%s", w.Body.String())
	}
}

// A Disallow names the app's own paths, and every locale serves them
// under its prefix too: /es/app is the same private screen as /app.
func TestRobotsRulesCoverPrefixes(t *testing.T) {
	ds := New(app.NewApp("x"), WithRobots(RobotsConfig{
		Allow:    []string{"/pricing"},
		Disallow: []string{"/app", "/__gofastr/"},
	}))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es", "fr"}})
	doc, _ := ds.RobotsTXT("")
	for _, line := range []string{
		"Disallow: /app\n", "Disallow: /es/app\n", "Disallow: /fr/app\n",
		"Disallow: /es/__gofastr/\n", "Allow: /fr/pricing\n",
	} {
		if !strings.Contains(doc, line) {
			t.Errorf("robots.txt lacks %q:\n%s", line, doc)
		}
	}
	if strings.Contains(doc, "/en/") {
		t.Errorf("robots.txt names the default locale's prefix:\n%s", doc)
	}
}
