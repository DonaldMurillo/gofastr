package static

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// A locale tree's pages link their own manifest, so the export writes
// one per locale with the prefixed start_url (2026-10-27 U5).
func TestExportWritesLocaleManifests(t *testing.T) {
	a := coreapp.NewApp("Meridian")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithPWA(uihost.PWAConfig{}))
	cat := i18n.NewMapCatalog()
	cat.Set("es", exportHello.Key(), i18n.Message{Text: "Hola"})
	tr := i18n.NewTranslator(cat, "en")
	set, err := i18n.NewPathLocales(tr)
	if err != nil {
		t.Fatal(err)
	}
	host.SetPathLocales(set)
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, BasePath: "/sub"}).Build(i18n.WithTranslator(context.Background(), tr)); err != nil {
		t.Fatalf("build: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(readOut(t, out, "es/manifest.webmanifest")), &m); err != nil {
		t.Fatalf("es manifest: %v", err)
	}
	if m["start_url"] != "/sub/es" || m["scope"] != "/sub/" || m["id"] != "/sub/" {
		t.Errorf("es manifest start_url/scope/id = %v / %v / %v, want /sub/es, /sub/, /sub/", m["start_url"], m["scope"], m["id"])
	}
	if err := json.Unmarshal([]byte(readOut(t, out, "manifest.webmanifest")), &m); err != nil {
		t.Fatalf("root manifest: %v", err)
	}
	if m["start_url"] != "/sub/" {
		t.Errorf("root manifest start_url = %v, want /sub/", m["start_url"])
	}
	if page := readOut(t, out, "es/index.html"); !strings.Contains(page, `<link rel="manifest" href="/sub/es/manifest.webmanifest">`) {
		t.Errorf("es page should link its own manifest:\n%s", page)
	}
	if sw := readOut(t, out, "service-worker.js"); !strings.Contains(sw, `"/sub/es/manifest.webmanifest"`) {
		t.Errorf("worker should precache the es manifest:\n%s", sw)
	}
}
