// Package b holds rootread fixtures for the fs.FS-mediated spelling,
// reduced from core/static serveFile (probe
// TestStaticSymlinkEscapeRefused) under different identifiers: an
// HTTP-serving function opening a caller-controlled name on a
// caller-supplied io/fs.FS, with the quiet postures beside it.
package b

import (
	"embed"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"
)

// brochure is the handler shape: the FS arrives from the caller, the
// name arrives from the request.
type brochure struct {
	assets fs.FS
}

func (b *brochure) servePage(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		http.NotFound(w, r)
		return
	}
	f, err := b.assets.Open(name) // want `read under a root with lexical containment only`
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	_, _ = io.Copy(w, f)
}

// serveSheet reads a request-named file through the package form.
func serveSheet(w http.ResponseWriter, r *http.Request, sheets fs.FS, name string) {
	data, err := fs.ReadFile(sheets, name) // want `read under a root with lexical containment only`
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(data)
}

// statSheet is the fs.Stat form.
func statSheet(w http.ResponseWriter, r *http.Request, sheets fs.FS, name string) {
	if _, err := fs.Stat(sheets, name); err != nil { // want `read under a root with lexical containment only`
		http.NotFound(w, r)
		return
	}
}

// serveGenerated wires the fs inside the handler: a DirFS over a
// caller-supplied root is the disk-backed spelling.
func serveGenerated(w http.ResponseWriter, r *http.Request, exportDir string) {
	fsys := os.DirFS(exportDir)
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if _, err := fsys.Open(name); err != nil { // want `read under a root with lexical containment only`
		http.NotFound(w, r)
		return
	}
}

// embedded is a concrete embed.FS: an embed cannot hold symlinks, so
// the same handler shape over it is quiet.
var embedded embed.FS

func (b *brochure) serveEmbedded(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if _, err := embedded.Open(name); err != nil {
		http.NotFound(w, r)
		return
	}
}

// packageFSVar is a package-level fs variable: its construction is
// invisible here, and the function never sees the boundary.
var packageFSVar fs.FS

func serveFromPackageVar(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if _, err := packageFSVar.Open(name); err != nil {
		http.NotFound(w, r)
		return
	}
}

// loadSheet has no HTTP parameter: a library helper whose caller owns
// both the fs and the name is one trust domain, and the site that
// matters is the caller that mixes a request in.
func loadSheet(sheets fs.FS, name string) ([]byte, error) {
	return fs.ReadFile(sheets, name)
}

// serveIndex reads a literal name: nothing caller-controlled.
func serveIndex(w http.ResponseWriter, r *http.Request, assets fs.FS) {
	data, err := fs.ReadFile(assets, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, _ = w.Write(data)
}
