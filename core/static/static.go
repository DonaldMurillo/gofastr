// Package static serves files from an fs.FS with content-hash ETags,
// Cache-Control, ranges, MIME detection and an SPA fallback.
package static

import (
	"embed"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// Config holds the configuration for serving static files.
type Config struct {
	// FS is the filesystem served. A disk-backed one is opened through an
	// *os.Root, so a symlink out of it is refused.
	FS fs.FS
	// Prefix is stripped, on a segment boundary, from the request path.
	Prefix string

	// MaxAge is the default cache duration for Cache-Control headers.
	// Zero means "no-cache".
	MaxAge time.Duration

	// IndexFile answers a directory path. Defaults to "index.html".
	IndexFile string

	// SPA answers a miss with IndexFile unless its last segment ends in
	// an asset extension (.js, .css, .png, .woff2, .json, ...).
	SPA bool

	// NotFoundFile, outside SPA mode, names a file that answers a miss
	// with status 404.
	NotFoundFile string

	// DirListing is reserved and ignored.
	DirListing bool

	digests *digestCache

	resolvedRoot string
	rootIdentity fs.FileInfo

	root *os.Root

	dirRequest string
}

func (c Config) defaults() Config {
	if c.IndexFile == "" {
		c.IndexFile = "index.html"
	}
	c.Prefix = strings.TrimRight(c.Prefix, "/")
	if c.Prefix != "" && !strings.HasPrefix(c.Prefix, "/") {
		c.Prefix = "/" + c.Prefix
	}
	return c
}

func stripPrefix(p, prefix string) string {
	if prefix == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, prefix); ok && (rest == "" || rest[0] == '/') {
		return rest
	}
	return p
}

// Handler returns an http.Handler that serves config's files to GET and
// HEAD.
func Handler(config Config) http.Handler {
	config = config.defaults()
	config.digests = newDigestCache()
	binding := &rootBinding{}
	binding.load(config.FS)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}

		if hasDotDotSegment(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		reqPath := strings.TrimPrefix(stripPrefix(path.Clean(r.URL.Path), config.Prefix), "/")

		if reqPath == "" {
			reqPath = config.IndexFile
		}

		if hasDotfileSegment(reqPath) {
			http.NotFound(w, r)
			return
		}
		if hasForbiddenConfigSegment(reqPath) {
			http.NotFound(w, r)
			return
		}
		aliased := hasWindowsAliasSegment(reqPath)

		config := config
		config.resolvedRoot, config.rootIdentity = binding.load(config.FS)
		if config.rootIdentity != nil {
			root, err := os.OpenRoot(config.resolvedRoot)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					http.NotFound(w, r)
				} else {
					i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
				}
				return
			}
			defer root.Close()
			info, err := openedRootInfo(root)
			if err != nil {
				i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
				return
			}
			if !os.SameFile(config.rootIdentity, info) {
				http.NotFound(w, r)
				return
			}
			config.root = root
		}
		listed := config.FS
		if config.root != nil {
			listed = config.root.FS()
		}
		if shortNameAlias(listed, reqPath) {
			http.NotFound(w, r)
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/") {
			config.dirRequest = reqPath
		}

		served := !aliased && serveFile(w, r, config, reqPath)
		if served {
			return
		}

		if config.SPA && reqPath != config.IndexFile && !isAssetPath(reqPath) {
			fallback := config
			fallback.MaxAge = 0
			if serveFile(w, r, fallback, config.IndexFile) {
				return
			}
		}

		if !config.SPA && config.NotFoundFile != "" && serveNotFoundFile(w, r, config) {
			return
		}

		http.NotFound(w, r)
	})
}

var assetExtensions = []string{
	".js", ".mjs", ".css", ".map",
	".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".avif", ".ico",
	".woff", ".woff2", ".ttf",
	".json", ".wasm", ".txt", ".xml",
}

func isAssetPath(p string) bool {
	ext := asciicase.Lower(path.Ext(path.Base(p)))
	return ext != "" && slices.Contains(assetExtensions, ext)
}

type rootBinding struct {
	mu       sync.Mutex
	bound    bool
	resolved string
	identity fs.FileInfo
}

func (b *rootBinding) load(fsys fs.FS) (string, fs.FileInfo) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.bound {
		return b.resolved, b.identity
	}
	resolved, known := resolveFSRoot(fsys)
	if !known {
		return "", nil
	}
	if resolved == "" {
		b.bound = true
		return "", nil
	}
	root, err := os.OpenRoot(resolved)
	if err != nil {
		return resolved, nil
	}
	defer root.Close()
	identity, err := openedRootInfo(root)
	if err != nil {
		return resolved, nil
	}
	b.bound, b.resolved, b.identity = true, resolved, identity
	return resolved, identity
}

func openedRootInfo(root *os.Root) (os.FileInfo, error) {
	f, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

func serveNotFoundFile(w http.ResponseWriter, r *http.Request, config Config) bool {
	r2 := r.Clone(r.Context())
	for _, h := range []string{"Range", "If-Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since"} {
		r2.Header.Del(h)
	}
	return serveFile(&notFoundWriter{ResponseWriter: w}, r2, config, strings.TrimPrefix(config.NotFoundFile, "/"))
}

type notFoundWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *notFoundWriter) WriteHeader(code int) {
	if w.wrote {
		return
	}
	w.wrote = true
	if code == http.StatusOK {
		h := w.Header()
		h.Del("ETag")
		h.Del("Last-Modified")
		h.Del("Accept-Ranges")
		h.Set("Cache-Control", "no-cache")
		code = http.StatusNotFound
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *notFoundWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func serveFile(w http.ResponseWriter, r *http.Request, config Config, name string) bool {
	if strings.IndexByte(name, 0) >= 0 {
		return false
	}
	if pre, err := config.stat(name); err == nil && !pre.Mode().IsRegular() && !pre.IsDir() {
		http.NotFound(w, r)
		return true
	}
	var f fs.File
	var err error
	if config.root != nil {
		f, err = config.root.Open(filepath.FromSlash(name))
	} else {
		//gofastr:allow(rootread) caller-supplied fs.FS: embed.FS carries no symlinks and os.DirFS is re-rooted through os.OpenRoot at install time; the *os.File branch above already goes through os.Root
		f, err = config.FS.Open(name)
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
			return false
		}
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return true
	}
	defer f.Close()

	if config.root == nil && !config.fileInRoot(f) {
		http.NotFound(w, r)
		return true
	}

	stat, err := f.Stat()
	if err != nil {
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return true
	}

	if stat.IsDir() {
		indexPath := path.Join(name, config.IndexFile)
		if config.dirRequest != "" && name == config.dirRequest {
			if !config.hasFile(indexPath) {
				return false
			}
			redirectToDir(w, r)
			return true
		}
		return serveFile(w, r, config, indexPath)
	}

	etag, consumed, err := fileETag(config.digests, f, stat, name, func() (fs.File, error) {
		return config.reopen(name)
	})
	if err != nil {
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return true
	}

	modTime := stat.ModTime()

	setCacheHeaders(w, etag, modTime, config.MaxAge)

	if code := preconditionStatus(r, etag, modTime); code != 0 {
		w.WriteHeader(code)
		return true
	}

	contentType := DetectFromName(name)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")

	if rs, ok := f.(io.ReadSeeker); ok {
		if _, err := rs.Seek(0, io.SeekEnd); err == nil {
			if r.Method == http.MethodHead && r.Header.Get("Range") != "" {
				r = r.Clone(r.Context())
				r.Header.Del("Range")
			}
			http.ServeContent(w, r, name, modTime, rs)
			return true
		}
		consumed = true
	}

	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return true
	}

	if consumed {
		reopened, err := config.reopen(name)
		if errors.Is(err, errOutsideRoot) {
			w.Header().Del("Content-Length")
			http.NotFound(w, r)
			return true
		}
		if err != nil {
			i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return true
		}
		defer reopened.Close()
		f = reopened
	}
	_, _ = io.Copy(w, f)
	return true
}

func (c Config) hasFile(name string) bool {
	if st, err := c.stat(name); err != nil || !st.Mode().IsRegular() {
		return false
	}
	f, err := c.reopen(name)
	if err != nil {
		return false
	}
	defer f.Close()
	st, err := f.Stat()
	return err == nil && st.Mode().IsRegular()
}

func (c Config) stat(name string) (fs.FileInfo, error) {
	if c.root != nil {
		return c.root.Stat(filepath.FromSlash(name))
	}
	return fs.Stat(c.FS, name) //gofastr:allow(rootread) type check only, no bytes read: the open after it and fileInRoot contain the read itself
}

func redirectToDir(w http.ResponseWriter, r *http.Request) {
	base := path.Base(path.Clean("/" + r.URL.Path))
	loc := "./" + (&url.URL{Path: base}).EscapedPath() + "/"
	if r.URL.RawQuery != "" {
		loc += "?" + textsafe.ScrubControlBytes(r.URL.RawQuery)
	}
	w.Header().Set("Location", loc)
	w.WriteHeader(http.StatusMovedPermanently)
}

var errOutsideRoot = errors.New("static: file resolves outside the root")

func (c Config) reopen(name string) (fs.File, error) {
	if c.root != nil {
		return c.root.Open(filepath.FromSlash(name))
	}
	//gofastr:allow(rootread) caller-supplied fs.FS: embed.FS carries no symlinks and os.DirFS is re-rooted through os.OpenRoot at install time; the *os.File branch above already goes through os.Root
	f, err := c.FS.Open(name)
	if err != nil {
		return nil, err
	}
	if !c.fileInRoot(f) {
		f.Close()
		return nil, errOutsideRoot
	}
	return f, nil
}

func resolveFSRoot(fsys fs.FS) (resolved string, known bool) {
	f, err := fsys.Open(".")
	if err != nil {
		return "", false
	}
	defer f.Close()
	of, ok := f.(*os.File)
	if !ok {
		return "", true
	}
	resolved, err = filepath.EvalSymlinks(of.Name())
	if err != nil {
		return "", false
	}
	return resolved, true
}

func (c Config) fileInRoot(f fs.File) bool {
	of, ok := f.(*os.File)
	if !ok || c.resolvedRoot == "" {
		return true
	}
	resolved, err := filepath.EvalSymlinks(of.Name())
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(c.resolvedRoot, resolved)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Mount registers Handler(config) on r under config.Prefix.
func Mount(r *router.Router, config Config) {
	config = config.defaults()
	handler := Handler(config)

	r.Get(config.Prefix+"/{path...}", handler)
}

func hasDotDotSegment(p string) bool {
	for _, component := range pathSegments(p) {
		if component == ".." {
			return true
		}
	}
	return false
}

// Refuses reports whether the handler refuses p by its spelling: a dot
// segment, a server config file name, or a Windows alias (a trailing dot
// or space, or a colon). A walked tree checks with it; a requested path
// with [RefusesIn].
func Refuses(p string) bool {
	return hasDotfileSegment(p) || hasForbiddenConfigSegment(p) || hasWindowsAliasSegment(p)
}

// RefusesIn reports [Refuses], or a segment shaped like a Windows 8.3
// short name (a "~" and a digit) that its directory in fsys does not list.
func RefusesIn(fsys fs.FS, p string) bool {
	return Refuses(p) || shortNameAlias(fsys, p)
}

func shortNameAlias(fsys fs.FS, p string) bool {
	if !listsShortNames(fsys) {
		return false
	}
	segs := pathSegments(p)
	for i, seg := range segs {
		if !hasShortNameShape(seg) {
			continue
		}
		dir := path.Join(segs[:i]...)
		if dir == "" {
			dir = "."
		}
		entries, err := fs.ReadDir(fsys, dir)
		if err != nil {
			return true
		}
		if !slices.ContainsFunc(entries, func(e fs.DirEntry) bool { return e.Name() == seg }) {
			return true
		}
	}
	return false
}

func listsShortNames(fsys fs.FS) bool {
	switch fsys.(type) {
	case embed.FS, *embed.FS:
		return false
	}
	return fsys != nil
}

func hasShortNameShape(segment string) bool {
	for i := strings.IndexByte(segment, '~'); i >= 0; {
		if i+1 < len(segment) && segment[i+1] >= '0' && segment[i+1] <= '9' {
			return true
		}
		next := strings.IndexByte(segment[i+1:], '~')
		if next < 0 {
			break
		}
		i += 1 + next
	}
	return false
}

func hasWindowsAliasSegment(p string) bool {
	for _, component := range pathSegments(p) {
		if component == "." || component == ".." {
			continue
		}
		if strings.ContainsRune(component, ':') || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
			return true
		}
	}
	return false
}

func hasDotfileSegment(p string) bool {
	for _, component := range pathSegments(p) {
		if component == "" || component == "." || component == ".." {
			continue
		}
		if strings.HasPrefix(component, ".") {
			return true
		}
	}
	return false
}

var forbiddenConfigFiles = map[string]struct{}{
	"web.config":             {},
	"global.asax":            {},
	"app.config":             {},
	"machine.config":         {},
	"applicationhost.config": {},
}

func hasForbiddenConfigSegment(p string) bool {
	for _, component := range pathSegments(p) {
		if _, ok := forbiddenConfigFiles[asciicase.Lower(component)]; ok {
			return true
		}
	}
	return false
}

func pathSegments(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' })
}
