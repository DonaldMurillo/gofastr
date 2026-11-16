package uihost

import (
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/rootfs"

	corestatic "github.com/DonaldMurillo/gofastr/core/static"
	"github.com/DonaldMurillo/gofastr/framework/dev"
)

// StaticDir returns the configured static directory path.
func (ds *UIHost) StaticDir() string {
	return ds.staticDir
}

// SetStaticFS sets the filesystem static files are served from. An
// os.DirFS is re-rooted so a symlink out of it is refused; any other
// fs.FS is used as given and must not follow symlinks.
func (ds *UIHost) SetStaticFS(fsys fs.FS) {
	ds.staticFS = rootfs.ConfineDirFS(fsys)
}

// HasStaticFS reports whether an embedded static FS is configured.
func (ds *UIHost) HasStaticFS() bool {
	return ds.staticFS != nil
}

// StaticFS returns the configured embedded static FS, or nil if none.
func (ds *UIHost) StaticFS() fs.FS {
	return ds.staticFS
}

func (ds *UIHost) staticRootOf() *rootfs.Root {
	ds.staticRootOnce.Do(func() {
		if ds.staticDir == "" {
			return
		}
		root, err := rootfs.Open(ds.staticDir)
		if err != nil {
			return
		}
		ds.staticRoot = root
	})
	return ds.staticRoot
}

func staticRel(urlPath string) string {
	return strings.TrimPrefix(path.Clean("/"+urlPath), "/")
}

func staticServable(fsys fs.FS, rel string) bool {
	if sub, ok := strings.CutPrefix(rel, ".well-known/"); ok {
		wk, err := fs.Sub(fsys, ".well-known")
		return err == nil && !corestatic.RefusesIn(wk, sub)
	}
	return !corestatic.RefusesIn(fsys, rel)
}

func (ds *UIHost) resolvesStaticOrScreen(r *http.Request) bool {
	path := r.URL.Path
	if path == "/favicon.ico" {
		return true
	}
	if path != "/" {
		if root := ds.staticRootOf(); root != nil {
			if rel := staticRel(path); rel != "" && staticServable(root.FS(), rel) {
				if info, err := root.Stat(filepath.FromSlash(rel)); err == nil && !info.IsDir() {
					return true
				}
			}
		}
		if ds.staticFS != nil {
			cleanPath := strings.TrimPrefix(path, "/")
			if cleanPath != "" && staticServable(ds.staticFS, cleanPath) {
				//gofastr:allow(rootread) caller-supplied fs.FS: embed.FS carries no symlinks and os.DirFS is re-rooted through os.OpenRoot at install time; the *os.File branch above already goes through os.Root
				if f, err := ds.staticFS.Open(cleanPath); err == nil && !fsEntryIsDir(f) {
					f.Close()
					return true
				}
			}
		}
	}
	if ds.faviconURL != "" && path == ds.faviconURL {
		return true
	}
	if ds.App != nil {
		path := routePath(r)
		if _, _, ok := ds.App.Router.Resolve(path); ok {
			return true
		}
		if path != "/" && !strings.HasSuffix(path, "/") {
			if _, _, ok := ds.App.Router.Resolve(path + "/"); ok {
				return true
			}
		}
		if ds.pageDocsOn() && strings.HasSuffix(path, "/llm.md") {
			base := strings.TrimSuffix(path, "/llm.md")
			if base == "" {
				base = "/"
			}
			if scr, _, ok := ds.App.Router.Resolve(base); ok && !scr.NoLLMMD {
				return true
			}
		}
	}
	return false
}

func devStaticNoStore(w http.ResponseWriter) {
	if dev.Enabled() {
		w.Header().Set("Cache-Control", "no-store")
	}
}

func fsEntryIsDir(f fs.File) bool {
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return false
	}
	_ = f.Close()
	return true
}
