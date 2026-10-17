package cssweight

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, routes map[string]string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// TestMeasureCountsInlineAndLinked: both <style> text and linked sheets
// count, a sheet linked twice counts once, and a non-stylesheet link
// (an icon, a preload) counts not at all.
func TestMeasureCountsInlineAndLinked(t *testing.T) {
	base := serve(t, map[string]string{
		"/": `<!doctype html><html><head>
<style>a{color:red}</style>
<link rel="Stylesheet preload" href="/b.css">
<link rel="stylesheet" href="/a.css">
<link rel="stylesheet" href="a.css">
<link rel="icon" href="/favicon.css">
</head><body><style>b{}</style></body></html>`,
		"/a.css": strings.Repeat("x", 100),
		"/b.css": strings.Repeat("y", 50),
	})
	w, err := Measure(http.DefaultClient, base+"/")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Sheets) != 4 {
		t.Fatalf("sheets = %+v, want 2 inline + 2 linked", w.Sheets)
	}
	if got, want := w.Bytes(), len("a{color:red}")+len("b{}")+100+50; got != want {
		t.Errorf("Bytes = %d, want %d", got, want)
	}
	if got, want := w.Inline(), len("a{color:red}")+len("b{}"); got != want {
		t.Errorf("Inline = %d, want %d", got, want)
	}
	if w.Gzip() <= 0 || w.Gzip() >= w.Bytes()+4*30 {
		t.Errorf("Gzip = %d for %d raw bytes", w.Gzip(), w.Bytes())
	}
}

// TestMeasureRefusesMissingSheet: a sheet that answers 404 is an error,
// not a zero-byte sheet, so a broken link can't pass the cap.
func TestMeasureRefusesMissingSheet(t *testing.T) {
	base := serve(t, map[string]string{
		"/": `<link rel="stylesheet" href="/gone.css">`,
	})
	if _, err := Measure(http.DefaultClient, base+"/"); err == nil {
		t.Fatal("a 404 sheet measured without error")
	}
}

// TestMeasureRefusesErrorPage: a page that answers 404 is an error too.
func TestMeasureRefusesErrorPage(t *testing.T) {
	base := serve(t, map[string]string{})
	if _, err := Measure(http.DefaultClient, base+"/nope"); err == nil {
		t.Fatal("a 404 page measured without error")
	}
}

// TestMeasureRefusesRedirect: a page that redirects is an error, so a
// signed-in gate whose session lapsed doesn't weigh the login page.
func TestMeasureRefusesRedirect(t *testing.T) {
	base := serve(t, map[string]string{"/login": `<style>a{}</style>`})
	srv := httptest.NewServer(http.RedirectHandler(base+"/login", http.StatusSeeOther))
	t.Cleanup(srv.Close)
	if _, err := Measure(http.DefaultClient, srv.URL+"/app"); err == nil {
		t.Fatal("a redirected page measured without error")
	}
}

// TestMeasureRefusesSelfRedirect: a redirect back to the same URL (set a
// cookie, come back) is still a redirect, and Measure must not follow it.
func TestMeasureRefusesSelfRedirect(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			http.Redirect(w, r, r.URL.Path, http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`<style>a{}</style>`))
	}))
	t.Cleanup(srv.Close)
	if _, err := Measure(http.DefaultClient, srv.URL+"/app"); err == nil {
		t.Fatal("a self-redirect measured without error")
	}
	if hits != 1 {
		t.Fatalf("Measure followed the redirect: %d requests", hits)
	}
}

// TestMeasureUsesBaseHref: a relative sheet resolves against <base href>
// the way the browser resolves it, not against the page URL.
func TestMeasureUsesBaseHref(t *testing.T) {
	base := serve(t, map[string]string{
		"/dashboard":      `<base href="/assets/"><link rel="stylesheet" href="app.css">`,
		"/assets/app.css": strings.Repeat("z", 100),
	})
	w, err := Measure(http.DefaultClient, base+"/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	if w.Bytes() != 100 {
		t.Fatalf("Bytes = %d, want the 100-byte /assets/app.css", w.Bytes())
	}
}

// TestMeasureRefusesImport: @import loads CSS the gate would not weigh,
// so a sheet or <style> that uses it is an error, not an undercount.
func TestMeasureRefusesImport(t *testing.T) {
	for name, page := range map[string]string{
		"linked": `<link rel="stylesheet" href="/a.css">`,
		"inline": `<style>@IMPORT url("/big.css");</style>`,
	} {
		t.Run(name, func(t *testing.T) {
			base := serve(t, map[string]string{
				"/":        page,
				"/a.css":   `@import "/big.css"; a{}`,
				"/big.css": strings.Repeat("q", 1000),
			})
			if _, err := Measure(http.DefaultClient, base+"/"); err == nil {
				t.Fatal("an @import measured without error")
			}
		})
	}
}
