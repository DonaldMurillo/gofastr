package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// The crumb trail capitalizes a path segment's first letter. Upper-casing
// the first BYTE split a multi-byte letter: /étude rendered "�" plus
// a stray continuation byte, so the crumb read "�©tude" and the page held
// invalid UTF-8. Found live by the chaos pass on a deep link.
func TestCrumbCapitalizesMultiByteSegment(t *testing.T) {
	trackerEnv(t)
	app := buildApp()
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("init plugins: %v", err)
	}
	srv := httptest.NewServer(app.Router())
	t.Cleanup(srv.Close)

	for path, want := range map[string]string{
		"/%C3%A9tude":             "Étude",
		"/projects/%C3%BCber-ops": "Über-ops",
	} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		page := string(body)
		if !utf8.ValidString(page) {
			t.Errorf("%s: the page holds invalid UTF-8", path)
		}
		i := strings.Index(page, `data-cui-area="l:shell~crumbs"`)
		if i < 0 {
			t.Fatalf("%s: no crumb area", path)
		}
		crumbs := page[i:]
		if end := strings.Index(crumbs, "</nav>"); end >= 0 {
			crumbs = crumbs[:end]
		}
		if !strings.Contains(crumbs, ">"+want+"<") {
			t.Errorf("%s: crumbs do not name %q: %s", path, want, crumbs)
		}
	}
}
