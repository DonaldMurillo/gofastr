package static

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// buildPWASite exports a minimal WithPWA app. An optional offline
// screen component overrides the framework default.
func buildPWASite(t *testing.T, basePath string, offline ...component.Component) string {
	t.Helper()
	var offlineScreen component.Component
	if len(offline) > 0 {
		offlineScreen = offline[0]
	}
	// Real static assets behind the declared icon + Precache paths —
	// entries that don't resolve would fail service-worker install.
	staticDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(staticDir, "icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, body := range map[string]string{
		filepath.Join("icons", "icon-192.png"): "png-192",
		"hero.png":                             "png-hero",
	} {
		if err := os.WriteFile(filepath.Join(staticDir, rel), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a := coreapp.NewApp("Meridian")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a,
		uihost.WithStaticDir(staticDir),
		uihost.WithPWA(uihost.PWAConfig{
			ThemeColor:    "#112233",
			Icons:         []uihost.PWAIcon{{Src: "/icons/icon-192.png", Sizes: "192x192", Type: "image/png"}},
			Precache:      []string{"/hero.png"},
			OfflineScreen: offlineScreen,
		}))
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, BasePath: basePath}).Build(context.Background()); err != nil {
		t.Fatalf("build: %v", err)
	}
	return out
}

func readOut(t *testing.T, out string, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("expected %s in export: %v", rel, err)
	}
	return string(b)
}

func TestBuildEmitsPWAAssets(t *testing.T) {
	out := buildPWASite(t, "")

	var m map[string]any
	if err := json.Unmarshal([]byte(readOut(t, out, "manifest.webmanifest")), &m); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
	if m["start_url"] != "/" || m["scope"] != "/" {
		t.Errorf("apex export should keep root start_url/scope, got %v / %v", m["start_url"], m["scope"])
	}

	sw := readOut(t, out, "service-worker.js")
	if !strings.Contains(sw, `"/__gofastr/runtime.js"`) || !strings.Contains(sw, `"/hero.png"`) {
		t.Errorf("sw precache should list shell + declared assets:\n%s", sw)
	}

	reg := readOut(t, out, "__gofastr/pwa/register.js")
	if !strings.Contains(reg, `register("/service-worker.js"`) {
		t.Errorf("register.js should target the root worker:\n%s", reg)
	}

	offline := readOut(t, out, "__gofastr/pwa/offline/index.html")
	if !strings.Contains(strings.ToLower(offline), "offline") {
		t.Errorf("offline page should render the offline screen:\n%s", offline)
	}

	page := readOut(t, out, "index.html")
	if !strings.Contains(page, `<link rel="manifest" href="/manifest.webmanifest">`) {
		t.Errorf("exported page should link the manifest:\n%s", page)
	}
}

func TestBuildPWABasePath(t *testing.T) {
	out := buildPWASite(t, "/sub")

	var m map[string]any
	if err := json.Unmarshal([]byte(readOut(t, out, "manifest.webmanifest")), &m); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
	if m["start_url"] != "/sub/" || m["scope"] != "/sub/" || m["id"] != "/sub/" {
		t.Errorf("base path should prefix start_url/scope/id, got %v / %v / %v", m["start_url"], m["scope"], m["id"])
	}
	icons := m["icons"].([]any)
	if icons[0].(map[string]any)["src"] != "/sub/icons/icon-192.png" {
		t.Errorf("icon src should be base-prefixed, got %v", icons[0])
	}

	sw := readOut(t, out, "service-worker.js")
	for _, want := range []string{
		`"/sub/__gofastr/runtime.js"`,
		`"/sub/hero.png"`,
		`"/sub/__gofastr/pwa/offline"`,
	} {
		if !strings.Contains(sw, want) {
			t.Errorf("sw should carry base-prefixed precache entry %s:\n%s", want, sw)
		}
	}
	if strings.Contains(sw, `"/__gofastr/runtime.js"`) {
		t.Errorf("sw must not keep unprefixed shell entries under a base path:\n%s", sw)
	}

	reg := readOut(t, out, "__gofastr/pwa/register.js")
	if !strings.Contains(reg, `register("/sub/service-worker.js"`) || !strings.Contains(reg, `scope: "/sub/"`) {
		t.Errorf("register.js should target the base-prefixed worker + scope:\n%s", reg)
	}

	offline := readOut(t, out, "__gofastr/pwa/offline/index.html")
	if !strings.Contains(offline, `href="/sub/__gofastr/app.css"`) {
		t.Errorf("offline page assets should be base-prefixed:\n%s", offline)
	}
}

// pwaStyledOffline renders two styled components — enough that the old
// bundle=true offline chrome would emit the (never-exported)
// comp-bundle URL into the precache.
type pwaStyledOffline struct{ styles []*registry.Style }

func (c pwaStyledOffline) Render() render.HTML {
	out := render.HTML("<p>offline</p>")
	for _, st := range c.styles {
		out += st.WrapHTML(render.HTML(`<div class="x">x</div>`))
	}
	return out
}

// TestBuildPWAPrecacheEntriesAllResolve: every URL the exported service
// worker precaches must exist as a file in the export — one 404 rejects
// the whole install and kills the PWA surface. The styled offline
// screen is what used to push an inexistent comp-bundle URL into the
// precache.
func TestBuildPWAPrecacheEntriesAllResolve(t *testing.T) {
	styles := make([]*registry.Style, 2)
	for i := range styles {
		name := fmt.Sprintf("pwa-ssg-comp-%d", ssgNameSeq.Add(1))
		styles[i] = registry.RegisterStyle(name, func(theme style.Theme) string {
			return style.NewComponentSheet(name, theme).
				Rule(".x").Set("color", "red").End().
				MustBuild()
		})
	}
	out := buildPWASite(t, "/sub", pwaStyledOffline{styles})
	sw := readOut(t, out, "service-worker.js")
	i := strings.Index(sw, "var PRECACHE = ")
	if i < 0 {
		t.Fatalf("no PRECACHE in sw:\n%s", sw)
	}
	line := sw[i+len("var PRECACHE = "):]
	line = line[:strings.Index(line, ";\n")]
	var precache []string
	if err := json.Unmarshal([]byte(line), &precache); err != nil {
		t.Fatalf("PRECACHE not JSON: %v", err)
	}
	for _, u := range precache {
		p := strings.TrimPrefix(u, "/sub")
		if q := strings.IndexAny(p, "?#"); q >= 0 {
			p = p[:q] // static hosts ignore the query
		}
		rel := strings.TrimPrefix(p, "/")
		if p == "/__gofastr/pwa/offline" {
			rel = "__gofastr/pwa/offline/index.html"
		}
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err != nil {
			t.Errorf("precached URL %s has no file in the export (%s): install would 404", u, rel)
		}
	}
}

func TestBuildNoPWAAssetsWithoutOption(t *testing.T) {
	a := coreapp.NewApp("x")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a)
	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out}).Build(context.Background()); err != nil {
		t.Fatalf("build: %v", err)
	}
	for _, rel := range []string{"manifest.webmanifest", "service-worker.js", "__gofastr/pwa/register.js", "__gofastr/pwa/offline/index.html"} {
		if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err == nil {
			t.Errorf("%s should not exist without WithPWA", rel)
		}
	}
}
