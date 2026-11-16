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

// SetStaticFS sets an embedded filesystem for serving static files.
//
// A disk-backed os.DirFS value is re-rooted through os.OpenRoot before
// installation, so every later read (the two serve sites and the PWA
// manifest lookup) is kernel-contained exactly like WithStaticDir's: a
// symlink inside the directory pointing outside it is refused rather
// than followed. If the directory cannot be opened as a root, the FS
// is replaced by one whose every open fails closed with that error —
// serving symlink-following reads from a directory that was not
// verifiable at installation time is the worse failure. embed.FS and
// every other fs.FS pass through unchanged; a custom FS must not
// follow symlinks (os.Root.FS() is the spelling). Only files are
// served: a directory falls through to the screens, as under
// WithStaticDir.
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

// staticRootOf returns the os.Root over staticDir, opening it on first
// use. All staticDir reads go through it: os.Root refuses symlinked path
// components that escape the static tree in the kernel, with no window
// between the containment check and the open, where the previous
// filepath.Abs + HasPrefix check was lexical only and served files from
// outside the tree through a symlinked directory or file (2026-09-04
// red-probe round). A staticDir that fails to open roots nothing and
// static serving falls through to page rendering, matching the previous
// behavior of an os.Stat miss on a missing directory.
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

// staticRel cleans a request path into its root-relative form for os.Root
// calls: cleaning "/"+path collapses traversal segments against the leading
// slash, so the result is either "" (the root itself) or a clean relative
// path. os.Root then contains the rest (symlinked components, interior
// "..").
func staticRel(urlPath string) string {
	return strings.TrimPrefix(path.Clean("/"+urlPath), "/")
}

// staticServable reports whether rel, a slash path below fsys (the
// static directory's root or the static FS), may be served: not one
// core/static refuses (a dot segment such as .env or .git/config, a
// server config file, a Windows alias spelling, or an 8.3 short name no
// entry of its directory carries), except that a leading .well-known/ is
// the public namespace RFC 8615 reserves, so the files under it are
// served unless they are dotfiles themselves.
func staticServable(fsys fs.FS, rel string) bool {
	if sub, ok := strings.CutPrefix(rel, ".well-known/"); ok {
		wk, err := fs.Sub(fsys, ".well-known")
		return err == nil && !corestatic.RefusesIn(wk, sub)
	}
	return !corestatic.RefusesIn(fsys, rel)
}

// resolvesStaticOrScreen reports whether serveOrRender would produce
// content for this request's path rather than a 404: a static file
// (filesystem or embedded FS), a registered screen, or the favicon
// shortcut. It MIRRORS serveOrRender's resolution steps without serving
// (serveOrRender interleaves resolution with ServeFile/redirect calls,
// so the steps can't be shared outright). Any change to serveOrRender's
// resolution order or checks must be reflected here; the agreement is
// pinned by TestResolvePredicateMatchesServeOrRender.
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
		// Trailing-slash canonical path.
		if path != "/" && !strings.HasSuffix(path, "/") {
			if _, _, ok := ds.App.Router.Resolve(path + "/"); ok {
				return true
			}
		}
		// Concrete-URL llm.md for a dynamic route (mirrors
		// serveOrRender's dynamicPageLLMMD branch, incl. the
		// WithPublicLLMMD opt-in gate).
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

// devStaticNoStore marks a project static response uncacheable while the
// process runs under `gofastr dev` (#219). Project assets serve at
// unversioned URLs (/nav.js, /app.css) with Last-Modified but no explicit
// freshness, so browsers apply heuristic caching and keep executing the OLD
// script after an edit even though a fresh fetch returns the new bytes.
// Only a dev restart cleared it. no-store closes that loop; production is
// untouched: when dev.Enabled() is false no header is set at all.
func devStaticNoStore(w http.ResponseWriter) {
	if dev.Enabled() {
		w.Header().Set("Cache-Control", "no-store")
	}
}

// fsEntryIsDir reports whether an opened static-FS entry is a directory,
// and closes it when it is: the caller falls through to the screens and
// never reads it. A Stat error is not a directory, so the handle stays
// the caller's to serve and close as before.
func fsEntryIsDir(f fs.File) bool {
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return false
	}
	_ = f.Close()
	return true
}
