package uihost

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

//go:embed testdata/staticfs
var embedStaticRoot embed.FS

// TestSetStaticFS_DirFSSymlinkEscapeRefused pins the symlink-following
// read out of a host-supplied os.DirFS, found by the 2026-09-04
// red-probe round; fixed in SetStaticFS by re-rooting DirFS-backed
// values through os.OpenRoot(dir).FS(), so every later read (both serve
// sites and the PWA manifest lookup) is kernel-contained like
// WithStaticDir's. TestSetStaticFS_EmbedFSKeepsServing is the embed
// twin: embed.FS cannot carry symlinks and passes through unchanged.
//
// Family: F3 path canonicalization at filesystem sinks (host-supplied
// fs.FS read without symlink containment).
// Property: whatever fs.FS a host installs through SetStaticFS, the
// static serving path must never stream content from outside the tree
// the host named. WithStaticDir already guarantees this through an
// *os.Root; the FS-option route must not be the weaker sibling.
// Surfaces: framework/uihost/uihost.go:SetStaticFS (installs
// ds.staticFS via reRootDirFS), and the consumers,
// resolvesStaticOrScreen and serveOrRender (ds.staticFS.Open +
// http.ServeFileFS), plus pwa.go's fs.ReadFile.

func TestSetStaticFS_DirFSSymlinkEscapeRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Symlink needs developer mode on windows; shape covered by review")
	}

	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.css")
	if err := os.WriteFile(secret, []byte("body{background:url(OUTSIDE-SECRET)}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "inside.css"), []byte("body{margin:0}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "leak.css")); err != nil {
		t.Fatal(err)
	}

	a := app.NewApp("staticfs-escape-test")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)
	ds.SetStaticFS(os.DirFS(root))
	server := httptest.NewServer(ds)
	t.Cleanup(server.Close)

	// Sanity: in-tree files still serve through the option.
	resp, err := http.Get(server.URL + "/inside.css")
	if err != nil {
		t.Fatal(err)
	}
	inside, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(inside) != "body{margin:0}" {
		t.Fatalf("in-tree static file: status=%d body=%q, want 200 with the file bytes", resp.StatusCode, string(inside))
	}

	// The escape: the symlink must not stream the outside file.
	resp, err = http.Get(server.URL + "/leak.css")
	if err != nil {
		t.Fatal(err)
	}
	leak, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK && string(leak) == "body{background:url(OUTSIDE-SECRET)}" {
		t.Fatalf("SECURITY: [uihost-staticfs-symlink] GET /leak.css streamed OUTSIDE-SECRET through a symlink escaping the directory handed to SetStaticFS(os.DirFS(...)): the host-supplied FS is served verbatim, so the kernel containment WithStaticDir already has is missing on this route.")
	}
}

// The embed twin: an embed.FS cannot carry symlinks and must keep serving
// through the option unchanged after the re-root fix.
func TestSetStaticFS_EmbedFSKeepsServing(t *testing.T) {
	embedStatic, err := fs.Sub(embedStaticRoot, "testdata/staticfs")
	if err != nil {
		t.Fatal(err)
	}

	a := app.NewApp("staticfs-embed-test")
	a.RegisterScreen(app.NewScreen("/", &testHomeComp{}).WithTitle("Home"), nil)
	ds := New(a)
	ds.SetStaticFS(embedStatic)
	server := httptest.NewServer(ds)
	t.Cleanup(server.Close)

	resp, err := http.Get(server.URL + "/inside.css")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "body{margin:0}" {
		t.Fatalf("embedded static file: status=%d body=%q, want 200 with the embedded bytes", resp.StatusCode, string(body))
	}
}
