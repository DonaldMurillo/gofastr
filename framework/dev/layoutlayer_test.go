package dev

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

func TestLayoutLayerGate(t *testing.T) {
	cases := []struct {
		name, dev, env, optOut string
		want                   bool
	}{
		{"off without gofastr dev", "", "", "", false},
		{"on under gofastr dev", "1", "", "", true},
		{"opt-out wins", "1", "", "0", false},
		{"production wins", "1", "production", "", false},
		{"unparseable opt-out keeps it on", "1", "", "maybe", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("GOFASTR_DEV", c.dev)
			t.Setenv("GOFASTR_ENV", c.env)
			t.Setenv("GOFASTR_DEV_LAYOUT", c.optOut)
			if got := LayoutLayerEnabled(); got != c.want {
				t.Fatalf("LayoutLayerEnabled() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestLayoutLayerServesScript(t *testing.T) {
	r := router.New()
	RegisterLayoutLayer(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, LayoutLayerScriptURL, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status = %d, want 200", LayoutLayerScriptURL, rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/javascript; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	if !strings.Contains(rec.Body.String(), "data-cui-layout") {
		t.Fatalf("script does not read the layout markers:\n%.300s", rec.Body.String())
	}
}

// One injector per router splices every dev script registered on it,
// each once, whichever registered first.
func TestDevScriptsSplicedOnce(t *testing.T) {
	r := router.New()
	RegisterLayoutLayer(r)
	RegisterLiveReload(r)
	RegisterLayoutLayer(r)
	r.Get("/page", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>hi</p></body></html>"))
	}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	body := rec.Body.String()
	for _, url := range []string{LiveReloadScriptURL, LayoutLayerScriptURL} {
		if n := strings.Count(body, `src="`+url+`"`); n != 1 {
			t.Fatalf("%s spliced %d times, want 1:\n%s", url, n, body)
		}
	}
}

// A page that already links the layer (uihost adds it itself) keeps one tag.
func TestLayoutLayerNotSplicedTwice(t *testing.T) {
	r := router.New()
	RegisterLayoutLayer(r)
	page := `<html><body><script defer src="` + LayoutLayerScriptURL + `"></script></body></html>`
	r.Get("/page", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/page", nil))
	if rec.Body.String() != page {
		t.Fatalf("page carrying the layer was modified:\n%s", rec.Body.String())
	}
}

func TestMaybeRegisterLayoutLayerOff(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "")
	r := router.New()
	MaybeRegisterLayoutLayer(r)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, LayoutLayerScriptURL, nil))
	if rec.Code == http.StatusOK {
		t.Fatalf("%s served outside gofastr dev", LayoutLayerScriptURL)
	}
}
