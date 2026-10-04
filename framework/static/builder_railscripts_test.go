package static

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	coreapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

// A script the app serves from its own router (WithExtraScripts plus a
// route) is on every page, so the export must ship it.
func TestBuildExportsAppServedRailScripts(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithExtraScripts("/__site/reducers.js?v=1"))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /__site/reducers.js", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("window.reducers = 1;"))
	})

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, Handler: mux}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "__site", "reducers.js"))
	if err != nil || string(got) != "window.reducers = 1;" {
		t.Fatalf("exported script = %q, %v", got, err)
	}
}

func TestBuildFailsOnUnservedRailScript(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithExtraScripts("/__site/missing.js"))

	_, err := (&Builder{Host: host, OutDir: t.TempDir(), Handler: http.NewServeMux()}).Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/__site/missing.js") {
		t.Fatalf("err = %v, want the unserved script named", err)
	}
}

func TestBuildWithoutHandlerSkipsRailScripts(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithExtraScripts("/__site/missing.js"))

	if _, err := (&Builder{Host: host, OutDir: t.TempDir()}).Build(context.Background()); err != nil {
		t.Fatalf("a Builder with no Handler must not fetch rail scripts: %v", err)
	}
}

// A previous build's copy in a reused OutDir must not stand in for the
// script the app serves now.
func TestBuildRefreshesStaleRailScript(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithExtraScripts("/__site/reducers.js"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__site/reducers.js", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fresh"))
	})

	out := t.TempDir()
	stale := filepath.Join(out, "__site", "reducers.js")
	if err := os.MkdirAll(filepath.Dir(stale), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Builder{Host: host, OutDir: out, Handler: mux}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(stale); string(got) != "fresh" {
		t.Fatalf("exported script = %q, want the app's current copy", got)
	}
}

// A /__gofastr/ rail script the builder does not generate (the plugin
// broker) is fetched like any other.
func TestBuildExportsFrameworkPathRailScript(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	const src = "/__gofastr/plugin/host/pluginhost.js"
	host := uihost.New(a, uihost.WithExtraScripts(src))
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+src, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("broker"))
	})

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, Handler: mux}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(out, filepath.FromSlash(src[1:]))); err != nil || string(got) != "broker" {
		t.Fatalf("exported broker = %q, %v", got, err)
	}
}

// The PWA registration script is on the rail but the builder writes its
// own base-path-aware copy, so it is never fetched.
func TestBuildLeavesPWARegisterToBuilder(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithPWA(uihost.PWAConfig{}))

	out := t.TempDir()
	b := &Builder{Host: host, OutDir: out, Handler: http.NewServeMux(), BasePath: "/docs"}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(out, "__gofastr", "pwa", "register.js"))
	if err != nil || !strings.Contains(string(got), "/docs/") {
		t.Fatalf("register.js = %q, %v; want the builder's base-path copy", got, err)
	}
}

// Only a same-origin path is the app's to serve: a CDN script loads from
// its own host on the static deploy, and a relative src is no route.
func TestBuildSkipsOffOriginRailScripts(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithExtraScripts(
		"https://cdn.example.com/a.js", "//cdn.example.com/b.js", "js/c.js", `/\cdn.example.com/d.js`))

	if _, err := (&Builder{Host: host, OutDir: t.TempDir(), Handler: http.NewServeMux()}).Build(context.Background()); err != nil {
		t.Fatalf("off-origin rail scripts must be left to the browser: %v", err)
	}
}

// A static host decodes the request path, so the file is written under
// the decoded name.
func TestBuildWritesDecodedRailScriptName(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithExtraScripts("/__site/a%20b.js"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__site/a b.js", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("spaced"))
	})

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, Handler: mux}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(out, "__site", "a b.js")); err != nil || string(got) != "spaced" {
		t.Fatalf("exported script = %q, %v", got, err)
	}
}

// A document-scoped script is fetched only when an exported route is in
// its scope: one scoped to a route the export skips must not fail it.
func TestBuildFetchesOnlyInScopeDocScripts(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	a.Register("/admin", &homeScreen{}, nil)
	host := uihost.New(a)
	if err := host.RegisterDocumentScript("/__site/home.js", func(p string) bool { return p == "/" }); err != nil {
		t.Fatal(err)
	}
	if err := host.RegisterDocumentScript("/__site/admin.js", func(p string) bool { return p == "/admin" }); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__site/home.js", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("home"))
	})

	out := t.TempDir()
	b := &Builder{Host: host, OutDir: out, Handler: mux, ExcludeRoutes: []string{"/admin"}}
	if _, err := b.Build(context.Background()); err != nil {
		t.Fatalf("a script scoped to an excluded route failed the export: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(out, "__site", "home.js")); err != nil || string(got) != "home" {
		t.Fatalf("in-scope document script = %q, %v", got, err)
	}
}

// The handler sees the src's query, as it would from a browser.
func TestBuildPassesRailScriptQuery(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithExtraScripts("/__site/v.js?variant=beta"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__site/v.js", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.URL.Query().Get("variant")))
	})

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, Handler: mux}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(out, "__site", "v.js")); string(got) != "beta" {
		t.Fatalf("exported script = %q, want the beta variant", got)
	}
}

// Two srcs that differ only in query share one file: the first written
// wins and the second is not fetched over it.
func TestBuildWritesOneFilePerRailPath(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithExtraScripts("/__site/v.js?v=1", "/__site/v.js?v=2"))
	var fetches int
	mux := http.NewServeMux()
	mux.HandleFunc("GET /__site/v.js", func(w http.ResponseWriter, r *http.Request) {
		fetches++
		_, _ = w.Write([]byte(r.URL.Query().Get("v")))
	})

	out := t.TempDir()
	if _, err := (&Builder{Host: host, OutDir: out, Handler: mux}).Build(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fetches != 1 {
		t.Fatalf("fetched %d times, want 1", fetches)
	}
}

// A rail src that lands on a rendered page would overwrite it.
func TestBuildRefusesRailScriptOnPage(t *testing.T) {
	a := coreapp.NewApp("Rail")
	a.Register("/", &homeScreen{}, nil)
	host := uihost.New(a, uihost.WithExtraScripts("/index.html"))
	mux := http.NewServeMux()
	mux.HandleFunc("GET /index.html", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("clobber"))
	})

	out := t.TempDir()
	_, err := (&Builder{Host: host, OutDir: out, Handler: mux}).Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "/index.html") {
		t.Fatalf("err = %v, want the page collision named", err)
	}
	if got, _ := os.ReadFile(filepath.Join(out, "index.html")); string(got) == "clobber" {
		t.Fatal("the rail script overwrote the rendered page")
	}
}
