// Pins, found by the 2026-09-04 red-probe round, that static-file
// resolution under staticDir contained request paths lexically
// (filepath.Abs + HasPrefix) only, so a symlinked directory or file
// inside the static tree served content from outside it; fixed by
// routing every staticDir read through an *os.Root, which refuses
// symlink escapes in the kernel with no window between check and open.
//
// Property: reads, stats, and serves under the static root must stay
// under the root even when a path component is a symlink pointing
// outside it. A symlink whose target stays inside the root still works.
//
// Surfaces: serveOrRender (staticDir branch), resolvesStaticOrScreen
// (staticDir branch), pwaStaticBytes (staticDir branch).
package uihost

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
)

func TestStaticDirSymlinkEscapeRefused(t *testing.T) {
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "leaked.txt"), []byte("TOPSECRET-OUTSIDE-STATIC-ROOT"), 0o600); err != nil {
		t.Fatal(err)
	}

	staticDir := t.TempDir()
	// Directory component inside the static root pointing outside it.
	if err := os.Symlink(outside, filepath.Join(staticDir, "assets")); err != nil {
		t.Fatal(err)
	}
	// Leaf file inside the static root pointing outside it.
	if err := os.Symlink(filepath.Join(outside, "leaked.txt"), filepath.Join(staticDir, "leak.txt")); err != nil {
		t.Fatal(err)
	}
	// A real file and an in-root symlink, both of which must keep serving.
	if err := os.WriteFile(filepath.Join(staticDir, "real.txt"), []byte("public"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(staticDir, "link.txt")); err != nil {
		t.Fatal(err)
	}

	a := uiapp.NewApp("static-symlink-test")
	layout := uiapp.NewLayout("main").
		WithHeader(&testHeaderComp{}).
		WithFooter(&testFooterComp{})
	a.SetDefaultLayout(layout)
	a.RegisterScreen(uiapp.NewScreen("/screen", &testHomeComp{}).WithTitle("S"), nil)

	ds := New(a)
	ds.staticDir = staticDir

	serve := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		ds.serveOrRender(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}

	// Directory-component escape: /assets/leaked.txt must not serve the
	// outside file.
	if w := serve("/assets/leaked.txt"); strings.Contains(w.Body.String(), "TOPSECRET-OUTSIDE-STATIC-ROOT") {
		t.Errorf("SECURITY: [uihost-static-symlink] /assets/leaked.txt served outside-root bytes through a symlinked directory (status %d)", w.Code)
	}
	// Leaf escape: /leak.txt must not serve the outside file.
	if w := serve("/leak.txt"); strings.Contains(w.Body.String(), "TOPSECRET-OUTSIDE-STATIC-ROOT") {
		t.Errorf("SECURITY: [uihost-static-symlink] /leak.txt served outside-root bytes through a symlinked file (status %d)", w.Code)
	}
	// The MethodNotAllowed fall-through predicate must agree: neither
	// path resolves.
	for _, p := range []string{"/assets/leaked.txt", "/leak.txt"} {
		if ds.resolvesStaticOrScreen(httptest.NewRequest(http.MethodGet, p, nil)) {
			t.Errorf("SECURITY: [uihost-static-symlink] resolvesStaticOrScreen(%s) claims a symlink-escaped path resolves", p)
		}
	}
	// The PWA version fingerprint must not read through the escape.
	if b := ds.pwaStaticBytes("/assets/leaked.txt"); b != nil {
		t.Errorf("SECURITY: [uihost-static-symlink] pwaStaticBytes read outside-root bytes through a symlinked directory (%d bytes)", len(b))
	}
	if b := ds.pwaStaticBytes("/leak.txt"); b != nil {
		t.Errorf("SECURITY: [uihost-static-symlink] pwaStaticBytes read outside-root bytes through a symlinked file (%d bytes)", len(b))
	}

	// Control: real file still serves.
	if w := serve("/real.txt"); w.Code != http.StatusOK || w.Body.String() != "public" {
		t.Errorf("real file: status %d body %q, want 200 \"public\"", w.Code, w.Body.String())
	}
	// Control: a symlink whose target stays inside the root still serves.
	if w := serve("/link.txt"); w.Code != http.StatusOK || w.Body.String() != "public" {
		t.Errorf("in-root symlink: status %d body %q, want 200 \"public\"", w.Code, w.Body.String())
	}
}
