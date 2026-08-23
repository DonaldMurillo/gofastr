package uihost

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// newStaticDirHost serves one project static file (nav.js) out of a temp
// static dir on the same shape a generated project uses.
func newStaticDirHost(t *testing.T) *httptest.Server {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "nav.js"), []byte("console.log('nav v2')"), 0644); err != nil {
		t.Fatal(err)
	}
	a := app.NewApp("static-cache-test")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	server := httptest.NewServer(New(a, WithStaticDir(dir)))
	t.Cleanup(server.Close)
	return server
}

// getStatic fetches url and returns the response with a drained body.
func getStatic(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// --- #219: project static assets must not be heuristically cached in dev ---

func TestDevServesStaticWithNoStore(t *testing.T) {
	server := newStaticDirHost(t)
	// Pin both vars so the outcome doesn't depend on the outer env.
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")

	resp := getStatic(t, server.URL+"/nav.js")
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("dev static response Cache-Control = %q, want no-store", cc)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "console.log('nav v2')" {
		t.Errorf("dev static body = %q, want the file bytes", body)
	}
}

func TestDevServesStaticFSWithNoStore(t *testing.T) {
	fsys := fstest.MapFS{"app.css": &fstest.MapFile{Data: []byte("body{margin:0}")}}
	a := app.NewApp("static-cache-fs-test")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)
	ds.SetStaticFS(fsys)
	server := httptest.NewServer(ds)
	t.Cleanup(server.Close)

	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")

	resp := getStatic(t, server.URL+"/app.css")
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Errorf("dev static-FS response Cache-Control = %q, want no-store", cc)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "body{margin:0}" {
		t.Errorf("dev static-FS body = %q, want the file bytes", body)
	}
}

func TestProdStaticSetsNoCacheControl(t *testing.T) {
	server := newStaticDirHost(t)

	// dev.Enabled() is evaluated per request, so one host can serve both
	// non-dev configurations.
	t.Run("dev flag off", func(t *testing.T) {
		t.Setenv("GOFASTR_DEV", "0")
		resp := getStatic(t, server.URL+"/nav.js")
		if cc := resp.Header.Get("Cache-Control"); cc != "" {
			t.Errorf("non-dev static response Cache-Control = %q, want empty", cc)
		}
	})
	t.Run("production env wins", func(t *testing.T) {
		t.Setenv("GOFASTR_ENV", "production")
		t.Setenv("GOFASTR_DEV", "1")
		resp := getStatic(t, server.URL+"/nav.js")
		if cc := resp.Header.Get("Cache-Control"); cc != "" {
			t.Errorf("production static response Cache-Control = %q, want empty", cc)
		}
	})
}
