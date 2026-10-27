package print

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

var metaCSP = regexp.MustCompile(`http-equiv="Content-Security-Policy" content="([^"]*)"`)

// docCSP returns the in-document policy of a rendered print shell.
func docCSP(t *testing.T, html string) string {
	t.Helper()
	m := metaCSP.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no CSP meta in the document:\n%s", html)
	}
	return m[1]
}

// The PDF document is about:blank to Chromium, an opaque origin, so the
// shell's style-src 'self' refused the absolute app.css link BaseURL
// produces: the documented configuration never carried the app's tokens.
// The PDF policy names BaseURL's origin in style-src and nowhere else;
// the HTML route keeps the header policy byte for byte.
func TestPDFCSPAllowsBaseURLStyles(t *testing.T) {
	fr := &fakeRenderer{}
	b := New(Config{
		DefaultAccess: Public,
		AppCSSURL:     "/__gofastr/app.css",
		BaseURL:       "https://app.example.com:8443",
		PDFRenderer:   fr,
	}).Document(Document{Name: "doc", Path: "/doc", Build: docBuild("x")})
	h := mount(t, b)

	getHost(t, h, "/print/doc/pdf", "evil.attacker.example")
	csp := docCSP(t, fr.gotHTML)
	want := render.Escape("style-src 'self' 'unsafe-inline' https://app.example.com:8443;")
	if !strings.Contains(csp, want) {
		t.Fatalf("PDF CSP does not allow styles from BaseURL's origin: %q", csp)
	}
	if strings.Count(csp, "app.example.com") != 1 {
		t.Errorf("BaseURL's origin widened more than style-src: %q", csp)
	}
	if strings.Contains(csp, "evil.attacker.example") {
		t.Fatalf("SSRF: the request Host reached the PDF CSP: %q", csp)
	}

	page := get(t, h, "/print/doc").Body.String()
	if got := docCSP(t, page); got != render.Escape(printCSP) {
		t.Errorf("HTML route's in-document CSP = %q, want printCSP", got)
	}
}

// BaseURL is spliced into a policy, so anything that is not a clean
// http(s) origin is refused: the PDF keeps printCSP and the stylesheet
// stays blocked, rather than a crafted value adding sources or
// directives.
func TestPDFCSPRefusesUncleanBaseURL(t *testing.T) {
	for _, base := range []string{
		"https://app.example.com; script-src *",
		"https://app.example.com%3Bscript-src",
		"https://user:pw@app.example.com",
		"javascript:alert(1)",
		"ftp://app.example.com",
		"app.example.com",
		"https://",
		"https://[::1]:8080",
		"https://app.example.com:80x",
		"https://app_example.com",
	} {
		fr := &fakeRenderer{}
		b := New(Config{
			DefaultAccess: Public,
			AppCSSURL:     "/__gofastr/app.css",
			BaseURL:       base,
			PDFRenderer:   fr,
		}).Document(Document{Name: "doc", Path: "/doc", Build: docBuild("x")})
		get(t, mount(t, b), "/print/doc/pdf")
		if got := docCSP(t, fr.gotHTML); got != render.Escape(printCSP) {
			t.Errorf("BaseURL %q widened the PDF CSP: %q", base, got)
		}
	}
}

func TestCSPOriginKeepsSchemeHostPort(t *testing.T) {
	for base, want := range map[string]string{
		"https://app.example.com":            "https://app.example.com",
		"https://app.example.com/":           "https://app.example.com",
		"http://127.0.0.1:8082":              "http://127.0.0.1:8082",
		"https://App.Example.com/base?x=1#f": "https://App.Example.com",
	} {
		if got := cspOrigin(base); got != want {
			t.Errorf("cspOrigin(%q) = %q, want %q", base, got, want)
		}
	}
}
