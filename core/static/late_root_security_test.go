package static

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// The symlink containment is set up when Handler is built: it resolves
// the os.DirFS root and opens it as an *os.Root. A root that did not
// exist yet at that moment (a build step or a dev watcher that creates
// the public directory after the server starts) resolved to nothing, so
// every later request opened through the bare os.DirFS, which follows a
// symlink anywhere on disk. Containment must not depend on the
// directory existing when the handler was built.
func TestStaticContainsRootCreatedAfterHandler(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Symlink needs developer mode on windows")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "public")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("OUTSIDE-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := Handler(Config{FS: os.DirFS(root)})
	if rr := doGet(h, "/ok.txt"); rr.Code != http.StatusNotFound {
		t.Fatalf("missing root: status %d, want 404", rr.Code)
	}

	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("OK"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "leak")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dlink")); err != nil {
		t.Fatal(err)
	}

	if rr := doGet(h, "/ok.txt"); rr.Code != http.StatusOK || rr.Body.String() != "OK" {
		t.Fatalf("in-root file after the root appeared: status %d body %q, want 200 OK", rr.Code, rr.Body.String())
	}
	for _, p := range []string{"/leak", "/dlink/secret.txt"} {
		rr := doGet(h, p)
		if strings.Contains(rr.Body.String(), "OUTSIDE-SECRET") {
			t.Errorf("GET %s: status %d served a file outside the root through a symlink", p, rr.Code)
		}
	}
}
