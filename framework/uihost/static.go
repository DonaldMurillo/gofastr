package uihost

import (
	"io"
	"io/fs"
	"net/http"
	"net/textproto"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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

func serveStaticStream(w http.ResponseWriter, r *http.Request, name string, body io.Reader, size int64, mod time.Time) {
	h := w.Header()
	known := !mod.IsZero() && !mod.Equal(time.Unix(0, 0))
	if known {
		h.Set("Last-Modified", mod.UTC().Format(http.TimeFormat))
	}
	safe := r.Method == http.MethodGet || r.Method == http.MethodHead
	failed := false
	if im := r.Header.Get("If-Match"); im != "" {
		failed = !etagListHasStar(im)
	} else if t, err := http.ParseTime(r.Header.Get("If-Unmodified-Since")); known && err == nil {
		failed = mod.Truncate(time.Second).After(t)
	}
	notModified := false
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		notModified = etagListHasStar(inm)
	} else if t, err := http.ParseTime(r.Header.Get("If-Modified-Since")); safe && known && err == nil {
		notModified = !mod.Truncate(time.Second).After(t)
	}
	switch {
	case failed, notModified && !safe:
		w.WriteHeader(http.StatusPreconditionFailed)
		return
	case notModified:
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", corestatic.DetectFromName(name))
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = io.CopyN(w, body, size)
	}
}

// etagListHasStar reports whether an If-Match or If-None-Match list
// reaches "*" before a malformed tag, as net/http scans it; the stream
// path sends no ETag, so "*" is the only entry that can match.
func etagListHasStar(list string) bool {
	for {
		list = textproto.TrimString(list)
		switch {
		case list == "":
			return false
		case list[0] == ',':
			list = list[1:]
		case list[0] == '*':
			return true
		default:
			rest, ok := skipETag(list)
			if !ok {
				return false
			}
			list = rest
		}
	}
}

func skipETag(s string) (string, bool) {
	s = strings.TrimPrefix(s, "W/")
	if len(s) < 2 || s[0] != '"' {
		return "", false
	}
	for i := 1; i < len(s); i++ {
		switch c := s[i]; {
		case c == '"':
			return s[i+1:], true
		case c == 0x21 || c >= 0x23 && c <= 0x7E || c >= 0x80:
		default:
			return "", false
		}
	}
	return "", false
}

func fsEntryIsDir(f fs.File) bool {
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return false
	}
	_ = f.Close()
	return true
}
