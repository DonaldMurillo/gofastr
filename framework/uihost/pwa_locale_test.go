package uihost

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// pwaLocaleGet serves path as the framework's locale router hands it
// over: the prefix stripped and the locale on the context.
func pwaLocaleGet(t *testing.T, ds *UIHost, tag, path string) *httptest.ResponseRecorder {
	t.Helper()
	set, _ := ds.PathLocales()
	req := httptest.NewRequest("GET", path, nil)
	req = req.WithContext(i18n.WithPathLocale(req.Context(), set, tag))
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, req)
	return w
}

func localePWAHost(t *testing.T, cfg PWAConfig) *UIHost {
	t.Helper()
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	ds := New(a, WithPWA(cfg))
	ds.SetPathLocales(i18n.PathLocales{Default: "en", Prefixed: []string{"es"}})
	return ds
}

// A reader on /es/ installed an app that launched at / (2026-10-27 U5):
// every locale linked one manifest whose start_url had no prefix.
func TestPWAManifestPerLocale(t *testing.T) {
	ds := localePWAHost(t, PWAConfig{StartURL: "/app"})
	manifest := func(tag string) map[string]any {
		t.Helper()
		w := pwaLocaleGet(t, ds, tag, "/manifest.webmanifest")
		if w.Code != 200 {
			t.Fatalf("%s manifest: %d", tag, w.Code)
		}
		var m map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	es := manifest("es")
	if es["start_url"] != "/es/app" {
		t.Errorf("es start_url = %v, want /es/app", es["start_url"])
	}
	// One app across languages: the identity and the scope stay put, so
	// a reader who switches language stays inside the installed app.
	if es["id"] != "/app" || es["scope"] != "/" {
		t.Errorf("es id/scope = %v/%v, want /app and /", es["id"], es["scope"])
	}
	if en := manifest("en"); en["start_url"] != "/app" {
		t.Errorf("en start_url = %v, want /app", en["start_url"])
	}

	// Each page links its own locale's manifest.
	if body := pwaLocaleGet(t, ds, "es", "/").Body.String(); !strings.Contains(body, `<link rel="manifest" href="/es/manifest.webmanifest">`) {
		t.Errorf("es page should link /es/manifest.webmanifest:\n%s", body)
	}
	if body := pwaLocaleGet(t, ds, "en", "/").Body.String(); !strings.Contains(body, `<link rel="manifest" href="/manifest.webmanifest">`) {
		t.Errorf("en page should link /manifest.webmanifest:\n%s", body)
	}

	// The worker precaches every locale's manifest, as it does manifest.js.
	sw, err := ds.PWAServiceWorkerJS("")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, p := range pwaSWPrecache(t, sw) {
		got[p] = true
	}
	for _, p := range []string{"/manifest.webmanifest", "/es/manifest.webmanifest"} {
		if !got[p] {
			t.Errorf("precache is missing %s", p)
		}
	}
}

// The root start_url gets the bare prefix, and an absolute or
// already-prefixed one is left alone.
func TestPWAStartURLLocaleEdges(t *testing.T) {
	for start, want := range map[string]string{
		"/":                      "/es",
		"https://x.example/home": "https://x.example/home",
		"/es/app":                "/es/app",
	} {
		ds := localePWAHost(t, PWAConfig{StartURL: start})
		set, _ := ds.PathLocales()
		body, err := ds.PWAManifestJSON(i18n.WithPathLocale(context.Background(), set, "es"), "")
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			t.Fatal(err)
		}
		if m["start_url"] != want {
			t.Errorf("StartURL %q in es = %v, want %q", start, m["start_url"], want)
		}
	}
}
