package static_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core/static"
)

// TestStatic_BlocksServerConfigFiles verifies that requests for well-
// known server-side config files return 404 even when the backing FS
// happens to contain a file with that name. These files commonly leak
// configuration / credentials and have no legitimate reason to live
// behind a public file server.
func TestStatic_BlocksServerConfigFiles(t *testing.T) {
	t.Parallel()
	fs := fstest.MapFS{
		"index.html":     {Data: []byte("home")},
		"web.config":     {Data: []byte("<?xml version=\"1.0\"?>")},
		"global.asax":    {Data: []byte("<%@ Application %>")},
		"app.config":     {Data: []byte("<configuration/>")},
		"machine.config": {Data: []byte("<configuration/>")},
	}
	h := static.Handler(static.Config{FS: fs})
	cases := []string{"/web.config", "/Web.Config", "/global.asax", "/app.config", "/machine.config"}
	for _, p := range cases {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusNotFound {
			t.Errorf("SECURITY: [static] GET %s returned %d, want 404. Server-side config files must not be served.", p, rr.Code)
		}
	}
}

// TestStatic_RefusesBackslashSeparatedHiddenFiles: a URL may carry a
// backslash (/sub%5C.env decodes to "sub\.env"). The dot-segment and
// config-file refusals split on "/" only, so that path read as one
// innocent segment, while a disk FS on Windows opens it through os.Root,
// where "\" IS a separator, and served sub/.env. Refuses must read "\" as
// the separator the filesystem may read it as.
func TestStatic_RefusesBackslashSeparatedHiddenFiles(t *testing.T) {
	t.Parallel()
	for _, p := range []string{`sub\.env`, `.git\config`, `sub\Web.Config`, `a/b\.htpasswd`} {
		if !static.Refuses(p) {
			t.Errorf("SECURITY: [static] Refuses(%q) = false: a backslash is a path separator on Windows, so this names a hidden or server config file", p)
		}
	}
	if static.Refuses(`assets/app.js`) {
		t.Errorf("Refuses(assets/app.js) = true, want false")
	}
}

// TestStatic_NoSniffHeader verifies every served file gets
// X-Content-Type-Options: nosniff to prevent the browser from
// interpreting a non-HTML response as HTML.
func TestStatic_NoSniffHeader(t *testing.T) {
	t.Parallel()
	fs := fstest.MapFS{
		"index.html": {Data: []byte("<h1>home</h1>")},
		"a.js":       {Data: []byte("console.log(1)")},
		"a.css":      {Data: []byte("body{}")},
		"a.json":     {Data: []byte("{}")},
		"a.jpg":      {Data: []byte("\xff\xd8\xff\xe0")},
	}
	h := static.Handler(static.Config{FS: fs})
	for _, p := range []string{"/", "/a.js", "/a.css", "/a.json", "/a.jpg"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("SECURITY: [static] GET %s missing X-Content-Type-Options: nosniff (got %q). Attack: MIME-sniff promotes a non-HTML response to script execution.", p, got)
		}
	}
}
