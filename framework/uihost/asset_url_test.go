package uihost

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

func newAssetDirHost(t *testing.T) (*UIHost, *httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logo.svg"), []byte("<svg>v1</svg>"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := app.NewApp("asset-url-test")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a, WithStaticDir(dir))
	server := httptest.NewServer(ds)
	t.Cleanup(server.Close)
	return ds, server, dir
}

func TestAssetURLAddsContentHash(t *testing.T) {
	ds, _, _ := newAssetDirHost(t)
	got := ds.AssetURL("/logo.svg")
	if !strings.HasPrefix(got, "/logo.svg?v=") || len(got) <= len("/logo.svg?v=") {
		t.Fatalf("AssetURL = %q, want /logo.svg?v=<hash>", got)
	}
	if again := ds.AssetURL("/logo.svg"); again != got {
		t.Fatalf("AssetURL changed with no file change: %q then %q", got, again)
	}
}

func TestAssetURLChangesWithContent(t *testing.T) {
	ds, _, dir := newAssetDirHost(t)
	before := ds.AssetURL("/logo.svg")
	path := filepath.Join(dir, "logo.svg")
	if err := os.WriteFile(path, []byte("<svg>v2</svg>"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Push the mtime forward so a coarse-grained filesystem clock still
	// reports the edit.
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if after := ds.AssetURL("/logo.svg"); after == before {
		t.Fatalf("AssetURL kept %q after the file changed", after)
	}
}

func TestAssetURLLeavesMissingFile(t *testing.T) {
	ds, _, _ := newAssetDirHost(t)
	for _, p := range []string{"/missing.png", "/", "", "logo.svg", "/../logo.svg", "https://cdn.example/x.js"} {
		if got := ds.AssetURL(p); got != p {
			t.Errorf("AssetURL(%q) = %q, want it unchanged", p, got)
		}
	}
}

func TestVersionedAssetIsImmutable(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "0")
	ds, server, _ := newAssetDirHost(t)
	resp := getStatic(t, server.URL+ds.AssetURL("/logo.svg"))
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("versioned asset Cache-Control = %q, want immutable", cc)
	}
}

func TestStaleAssetVersionNotImmutable(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "0")
	_, server, _ := newAssetDirHost(t)
	for _, q := range []string{"", "?v=0000", "?v="} {
		resp := getStatic(t, server.URL+"/logo.svg"+q)
		if cc := resp.Header.Get("Cache-Control"); strings.Contains(cc, "immutable") {
			t.Errorf("GET /logo.svg%s Cache-Control = %q, want no immutable", q, cc)
		}
	}
}

func TestVersionedAssetNoStoreInDev(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "1")
	t.Setenv("GOFASTR_ENV", "")
	ds, server, _ := newAssetDirHost(t)
	resp := getStatic(t, server.URL+ds.AssetURL("/logo.svg"))
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("dev versioned asset Cache-Control = %q, want no-store", cc)
	}
}

func TestAssetURLReadsStaticFS(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "0")
	fsys := fstest.MapFS{"css/site.css": &fstest.MapFile{Data: []byte("a{}")}}
	a := app.NewApp("asset-url-fs-test")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)
	ds.SetStaticFS(fsys)
	server := httptest.NewServer(ds)
	t.Cleanup(server.Close)

	u := ds.AssetURL("/css/site.css")
	if !strings.HasPrefix(u, "/css/site.css?v=") {
		t.Fatalf("AssetURL = %q, want a versioned URL", u)
	}
	resp := getStatic(t, server.URL+u)
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("versioned static-FS asset Cache-Control = %q, want immutable", cc)
	}
}

func TestReplacedFileNotCertified(t *testing.T) {
	t.Setenv("GOFASTR_DEV", "0")
	ds, _, dir := newAssetDirHost(t)
	path := filepath.Join(dir, "logo.svg")
	old, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	// A deploy swaps the file in while a response is already serving
	// the old one.
	next := filepath.Join(dir, "logo.svg.new")
	if err := os.WriteFile(next, []byte("<svg>v2, longer</svg>"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(next, path); err != nil {
		t.Fatal(err)
	}
	newURL := ds.AssetURL("/logo.svg")

	w := httptest.NewRecorder()
	ds.markVersionedStatic(w, httptest.NewRequest("GET", newURL, nil), assetKey{rel: "logo.svg"}, old)
	if got := w.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("old bytes served under the new hash with Cache-Control %q", got)
	}

	// The same request served from the new file is certified, so the
	// refusal above came from the hash check, not an early return.
	cur, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer cur.Close()
	w = httptest.NewRecorder()
	ds.markVersionedStatic(w, httptest.NewRequest("GET", newURL, nil), assetKey{rel: "logo.svg"}, cur)
	if got := w.Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Fatalf("new bytes under the new hash: Cache-Control %q, want immutable", got)
	}
}
