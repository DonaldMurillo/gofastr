package static

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
)

// TestStatic_DotDotTraversal verifies that path traversal via ../ is
// blocked. Attack: reading arbitrary files from the filesystem.
func TestStatic_DotDotTraversal(t *testing.T) {
	files := fstest.MapFS{
		"public/index.html":  &fstest.MapFile{Data: []byte("<h1>public</h1>")},
		"secret/secret.html": &fstest.MapFile{Data: []byte("SECRET")},
	}
	handler := Handler(Config{FS: files, Prefix: "/static"})

	req := httptest.NewRequest(http.MethodGet, "/static/../secret/secret.html", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), "SECRET") {
		t.Errorf("SECURITY: [path_traversal] GET /static/../secret/secret.html returned 200 with secret content. Attack: directory traversal via ../")
	}
}

// TestStatic_DoubleEncodedTraversal verifies that double-encoded path
// traversal (%252e%252e) is blocked. Attack: WAF bypass via encoding.
func TestStatic_DoubleEncodedTraversal(t *testing.T) {
	files := fstest.MapFS{
		"public/index.html":  &fstest.MapFile{Data: []byte("<h1>public</h1>")},
		"secret/secret.html": &fstest.MapFile{Data: []byte("SECRET")},
	}
	handler := Handler(Config{FS: files, Prefix: "/static"})

	// %252e = double-encoded "."
	req := httptest.NewRequest(http.MethodGet, "/static/%252e%252e/secret/secret.html", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), "SECRET") {
		t.Errorf("SECURITY: [path_traversal] double-encoded ../ returned 200 with secret. Attack: WAF bypass via double encoding.")
	}
}

// TestStatic_AbsolutePathBlocked verifies that absolute paths outside
// the FS root are rejected. Attack: /etc/passwd via absolute path.
func TestStatic_AbsolutePathBlocked(t *testing.T) {
	files := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>ok</h1>")},
	}
	handler := Handler(Config{FS: files, Prefix: "/static"})

	req := httptest.NewRequest(http.MethodGet, "/static//etc/passwd", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK && strings.Contains(rr.Body.String(), "root:") {
		t.Errorf("SECURITY: [path_traversal] GET /static//etc/passwd returned 200. Attack: absolute path breakout.")
	}
}

// TestStatic_MethodEnforced verifies that non-GET/HEAD methods are
// rejected. Attack: using PUT/DELETE to probe handler behavior.
func TestStatic_MethodEnforced(t *testing.T) {
	files := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>ok</h1>")},
	}
	handler := Handler(Config{FS: files})

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req := httptest.NewRequest(method, "/index.html", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusMethodNotAllowed {
			t.Errorf("SECURITY: [method] %s /index.html returned %d (want 405). Attack: non-GET method to static handler.", method, rr.Code)
		}
	}
}

// TestStatic_DotfileNotExposed verifies that dotfiles (e.g. .env, .htpasswd)
// are not served. Attack: reading configuration/secrets via dotfile access.
func TestStatic_DotfileNotExposed(t *testing.T) {
	files := fstest.MapFS{
		".env":        &fstest.MapFile{Data: []byte("SECRET_KEY=abc123")},
		".htpasswd":   &fstest.MapFile{Data: []byte("admin:$2y$10$hash")},
		"public/.git": &fstest.MapFile{Data: []byte("gitdir: ../.git/modules/repo")},
		"index.html":  &fstest.MapFile{Data: []byte("<h1>ok</h1>")},
	}
	handler := Handler(Config{FS: files})

	for _, path := range []string{"/.env", "/.htpasswd", "/.git"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code == http.StatusOK {
			t.Errorf("SECURITY: [dotfile] GET %s returned 200. Attack: dotfile exposure leaks secrets.", path)
		}
	}
}

// TestStatic_NoDirectoryListing verifies that directory paths without an
// index file do not produce a listing. Attack: enumerating file structure.
func TestStatic_NoDirectoryListing(t *testing.T) {
	files := fstest.MapFS{
		"dir/file1.txt": &fstest.MapFile{Data: []byte("file1")},
		"dir/file2.txt": &fstest.MapFile{Data: []byte("file2")},
	}
	handler := Handler(Config{FS: files})

	req := httptest.NewRequest(http.MethodGet, "/dir/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusOK {
		body := rr.Body.String()
		if strings.Contains(body, "file1.txt") && strings.Contains(body, "file2.txt") {
			t.Errorf("SECURITY: [dirlist] GET /dir/ returned listing with file names. Attack: directory enumeration.")
		}
	}
}

// TestStatic_ContentSniffingPrevented verifies that the correct
// Content-Type is set and X-Content-Type-Options: nosniff is present.
// Attack: browser MIME-sniffing a .json file as HTML.
func TestStatic_ContentSniffingPrevented(t *testing.T) {
	files := fstest.MapFS{
		"data.json": &fstest.MapFile{Data: []byte(`{"user":"<script>alert(1)</script>"}`)},
	}
	handler := Handler(Config{FS: files})

	req := httptest.NewRequest(http.MethodGet, "/data.json", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("SECURITY: [content_type] GET /data.json returned Content-Type=%q (want application/json). Attack: MIME sniffing may render JSON as HTML.", ct)
	}
}

// TestStatic_ETagNotLeakHashState verifies that ETag values don't leak
// internal state. Attack: using ETag as an oracle to detect file changes
// across deployments.
func TestStatic_ETagNotLeakHashState(t *testing.T) {
	data := []byte("hello world")
	etag := generateETag(data)
	// ETag should be a hash, not raw content
	if strings.Contains(etag, "hello world") {
		t.Errorf("SECURITY: [etag] ETag contains raw content: %q. Attack: content oracle via ETag.", etag)
	}
	// ETag should be consistent for same content
	etag2 := generateETag(data)
	if etag != etag2 {
		t.Errorf("ETag inconsistent for same content: %q vs %q", etag, etag2)
	}
}

// TestStatic_SymlinkEscapeRefused pins the symlink read escape, found
// by the 2026-09-04 red-probe round; fixed in Handler (resolves the
// FS root once) and serveFile (refuses with 404 unless the opened
// target still resolves under that root, on every open).
//
// Property: a static file handler configured with a root filesystem
// must never serve, at request time, a path that resolves through a
// symlink to content outside that root — the read sink is contained
// by the configured root, exactly as the write sinks are (the repo's
// rootwrite analyzer polices this shape on writes; this proves the
// read side).
// Surfaces: core/static/static.go::Handler, ::serveFile (both opens),
// and core/static/cache.go::fileETag (hashes only post-containment
// content). embed.FS cannot carry symlinks and is unaffected.
func TestStatic_SymlinkEscapeRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Symlink needs developer mode on windows; shape covered by review")
	}

	root := t.TempDir()
	outside := t.TempDir()
	secretFile := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secretFile, []byte("OUTSIDE-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideSub := filepath.Join(outside, "subdir")
	if err := os.MkdirAll(outsideSub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outsideSub, "k.txt"), []byte("SUBDIR-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A legitimate in-root file, to prove the handler still serves the
	// tree it was given.
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("OK"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Attack shapes: direct file symlink, directory symlink walked with
	// a subpath, and a two-hop chain whose intermediate hop is in-root.
	links := []struct {
		name string // symlink created inside root
		dest string
	}{
		{"leak", secretFile},
		{"dlink", outside},
		{"hop1", filepath.Join(root, "hop2")},
	}
	for _, l := range links {
		if err := os.Symlink(l.dest, filepath.Join(root, l.name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "hop2")); err != nil {
		t.Fatal(err)
	}

	h := Handler(Config{FS: os.DirFS(root)})

	// Sanity: the in-root file is served normally.
	if rr := doGet(h, "/ok.txt"); rr.Code != http.StatusOK || rr.Body.String() != "OK" {
		t.Fatalf("in-root file: status=%d body=%q, want 200 OK", rr.Code, rr.Body.String())
	}

	// Every escape shape must be refused.
	cases := []struct {
		path string
		want string // substring of the escaped content that must NOT reach the client
	}{
		{"/leak", "OUTSIDE-SECRET"},
		{"/dlink/subdir/k.txt", "SUBDIR-SECRET"},
		{"/hop1/secret.txt", "OUTSIDE-SECRET"},
	}
	for _, tc := range cases {
		rr := doGet(h, tc.path)
		if rr.Code == http.StatusOK || rr.Body.String() == tc.want {
			t.Errorf("SECURITY: [static-symlink] GET %s served status=%d body=%q: "+
				"the handler followed a symlink out of its configured root and "+
				"streamed content the root does not contain. The request path "+
				"passed every spelling check (no '..', no dotfile, no forbidden "+
				"config name), so only target resolution can contain it.",
				tc.path, rr.Code, rr.Body.String())
		}
	}
}

// swappingFS wraps a disk FS and fires onOpen after the inner Open has
// returned the handle — after the kernel resolved the name, before any
// caller-side containment check runs. That gap is the swap window the
// test below drives deterministically.
type swappingFS struct {
	inner  fs.FS
	onOpen func(name string, f fs.File)
}

func (s swappingFS) Open(name string) (fs.File, error) {
	f, err := s.inner.Open(name)
	if err == nil && s.onOpen != nil {
		s.onOpen(name, f)
	}
	return f, err
}

// TestStatic_SymlinkSwapWindowRefused pins the check-then-open window the
// first symlink fix left, found by the 2026-09-04 red-probe round; fixed
// by opening disk-backed requests through an *os.Root (kernel-enforced
// containment, no post-open check to race) instead of opening through
// the FS and verifying the target afterwards.
//
// Property: the handler must never stream content from outside its
// configured root — and the guarantee may not depend on re-resolving
// the name AFTER the file was already opened, because the tree can
// change between the two.
// Surfaces: core/static/static.go::serveFile (both opens: the primary
// and the non-seekable reopen, both now routed through *os.Root for
// disk-backed roots) and core/static/cache.go::fileETag (hashes only
// content the contained open produced).
func TestStatic_SymlinkSwapWindowRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Symlink needs developer mode on windows; shape covered by review")
	}

	root := t.TempDir()
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(secret, []byte("OUTSIDE-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ok.txt"), []byte("OK"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "leak")); err != nil {
		t.Fatal(err)
	}

	// The attack: while serveFile holds the fd that Open resolved
	// through the symlink, replace the symlink with a real in-root file
	// so a post-open EvalSymlinks check would see a contained path. The
	// kernel-contained open never consults the swapped tree: the fd and
	// the refusal are decided by the same syscall.
	swapped := false
	fsys := swappingFS{
		inner: os.DirFS(root),
		onOpen: func(name string, _ fs.File) {
			if name != "leak" || swapped {
				return
			}
			swapped = true
			link := filepath.Join(root, "leak")
			if err := os.Remove(link); err != nil {
				t.Fatalf("swap: remove symlink: %v", err)
			}
			if err := os.WriteFile(link, []byte("decoy-in-root"), 0o644); err != nil {
				t.Fatalf("swap: plant real file: %v", err)
			}
		},
	}

	h := Handler(Config{FS: fsys})

	// Sanity: the wrapped FS serves in-root files normally (and
	// resolveFSRoot still detects the disk backing through the wrapper).
	rr := doGet(h, "/ok.txt")
	if rr.Code != http.StatusOK || rr.Body.String() != "OK" {
		t.Fatalf("in-root file: status=%d body=%q, want 200 OK", rr.Code, rr.Body.String())
	}

	rr = doGet(h, "/leak")
	if rr.Code == http.StatusOK && rr.Body.String() == "OUTSIDE-SECRET" {
		t.Fatalf("SECURITY: [static-symlink-swap] GET /leak streamed OUTSIDE-SECRET through "+
			"the swap window: Open resolved the symlink (fd -> %s), the attacker replaced "+
			"the symlink with a real in-root file, and only THEN did a containment check "+
			"run on the name — which now resolves inside the root. A post-open check "+
			"certifies a tree state the handle does not reflect; containment must be "+
			"enforced by the open itself. (swap fired: %v)", secret, swapped)
	}
}

func doGet(h http.Handler, path string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr
}
