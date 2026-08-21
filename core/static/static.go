// Package static provides a static file server for Go's embed.FS (or any fs.FS),
// with ETag-based caching, configurable Cache-Control headers, MIME type
// detection, SPA fallback, and optional directory listing.
package static

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Config holds the configuration for serving static files.
type Config struct {
	// FS is the filesystem to serve files from (e.g. an embed.FS).
	FS fs.FS

	// Prefix is the URL path prefix to strip when mapping to filesystem paths.
	// For example, with Prefix="/static", a request for "/static/app.js"
	// serves "app.js" from FS.
	Prefix string

	// MaxAge is the default cache duration for Cache-Control headers.
	// Zero means "no-cache".
	MaxAge time.Duration

	// IndexFile is the name of the default file to serve for directory paths.
	// Defaults to "index.html".
	IndexFile string

	// SPA enables single-page application mode. When true, requests for
	// paths that don't match any file will serve IndexFile instead of 404.
	SPA bool

	// DirListing is reserved for a future release.
	// When implemented, enabling it will render an HTML directory listing
	// instead of returning 404 for directory paths that lack an index file.
	// Do not set this field. It is currently ignored.
	DirListing bool

	// digests memoises content digests for THIS handler's FS so the ETag
	// is not recomputed on every request. Handler allocates one per call;
	// see digestKey for why it must not be shared across filesystems.
	// Copying a Config shares the same cache, which is correct. The FS
	// travels with it.
	digests *sync.Map
}

// defaults fills in zero-value fields with sensible defaults.
func (c Config) defaults() Config {
	if c.IndexFile == "" {
		c.IndexFile = "index.html"
	}
	// Ensure prefix starts with /
	if c.Prefix != "" && !strings.HasPrefix(c.Prefix, "/") {
		c.Prefix = "/" + c.Prefix
	}
	return c
}

// Handler returns an http.Handler that serves static files according to the
// given Config.
func Handler(config Config) http.Handler {
	config = config.defaults()
	config.digests = &sync.Map{}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only serve GET and HEAD.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		// Reject `..` segments on the *raw* URL: running path.Clean
		// first (the previous behaviour) silently collapses
		// /static/../secret to /secret and lets the prefix check miss
		// the traversal entirely.
		if hasDotDotSegment(r.URL.Path) {
			http.NotFound(w, r)
			return
		}

		reqPath := path.Clean(r.URL.Path)
		if config.Prefix != "" {
			reqPath = strings.TrimPrefix(reqPath, config.Prefix)
		}
		reqPath = strings.TrimPrefix(reqPath, "/")

		if reqPath == "" {
			reqPath = config.IndexFile
		}

		// Refuse to serve dotfiles (.env, .git, .htpasswd, etc.): these
		// typically hold secrets or VCS metadata and must not be exposed
		// via the public static handler.
		if hasDotfileSegment(reqPath) {
			http.NotFound(w, r)
			return
		}
		// Refuse to serve well-known server-side config / metadata files
		// (web.config, .htaccess equivalents, ASP.NET app files). Even
		// when the backing FS is innocuous, an embed of a project tree
		// can accidentally ship these, and probing for them is a
		// standard fingerprinting step.
		if hasForbiddenConfigSegment(reqPath) {
			http.NotFound(w, r)
			return
		}

		// Try to open and serve the file.
		served := serveFile(w, r, config, reqPath)
		if served {
			return
		}

		// If SPA mode, try serving the index file as fallback.
		if config.SPA && reqPath != config.IndexFile {
			if serveFile(w, r, config, config.IndexFile) {
				return
			}
		}

		http.NotFound(w, r)
	})
}

// serveFile attempts to serve a file from the filesystem. Returns true if
// the file was successfully served.
func serveFile(w http.ResponseWriter, r *http.Request, config Config, name string) bool {
	f, err := config.FS.Open(name)
	if err != nil {
		// A genuinely missing file is a 404 (let the handler fall
		// through). Any other open error, permission denied, I/O
		// fault, unreadable backing store, is a server fault and must
		// surface as 500, not be masked as "not found".
		//
		// fs.ErrInvalid is on the 404 side of that line: fs.ValidPath
		// rejects any name that is not valid UTF-8, so os.DirFS and
		// embed.FS answer ErrInvalid for a URL like /%ff. That is a
		// malformed request, not a server fault. Treating it as one let
		// any client drive a 5xx with a two-character URL, and skipped
		// the SPA fallback on the way.
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrInvalid) {
			return false
		}
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return true
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return true
	}

	// If it's a directory, try to serve the index file.
	if stat.IsDir() {
		indexPath := path.Join(name, config.IndexFile)
		return serveFile(w, r, config, indexPath)
	}

	// ETag from a cached content digest. On a cache miss the file is
	// streamed through SHA-256 (never held in RAM); previously the whole
	// file was read and re-hashed on every request, and capped at 32MB,
	// which silently truncated larger files to a 200 with a
	// Content-Length/ETag that matched the truncated body.
	etag, consumed, err := fileETag(config.digests, f, stat, name)
	if err != nil {
		// A read fault that is not "not found" is a 500, not a 404.
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return true
	}

	// Get modification time.
	modTime := stat.ModTime()

	// Set cache headers.
	setCacheHeaders(w, etag, modTime, config.MaxAge)

	// Check conditional request (304 Not Modified).
	if checkPreconditions(r, etag, modTime) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}

	// Set content type. X-Content-Type-Options: nosniff prevents browsers
	// from MIME-sniffing a non-HTML response into HTML (e.g. promoting a
	// .jpg with embedded HTML into a script execution context). Cheap to
	// set on every response and required by every modern static guide.
	contentType := DetectFromName(name)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))

	// HEAD: headers only, no body.
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return true
	}

	// fileETag consumed the file on a cache miss; rewind before serving
	// the body. We stream directly to the ResponseWriter so arbitrarily
	// large files are never buffered in memory.
	//
	// fs.FS promises no seeking, so a conforming filesystem may hand back
	// a read-only handle. Reopen in that case: skipping the rewind would
	// copy from an exhausted reader and answer 200 with an EMPTY body
	// under the full Content-Length, a corrupt response.
	if rs, ok := f.(io.Seeker); ok {
		if _, err := rs.Seek(0, io.SeekStart); err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return true
		}
	} else if consumed {
		reopened, err := config.FS.Open(name)
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return true
		}
		defer reopened.Close()
		f = reopened
	}
	if _, err := io.Copy(w, f); err != nil {
		// Headers (and possibly part of the body) are already on the
		// wire. The status can no longer change. A mid-stream write
		// fault surfaces to the client as a truncated transfer, which
		// is the only honest signal left.
		return true
	}
	return true
}

// Mount registers the static file handler on the given router using the
// Config's Prefix as the path pattern.
func Mount(r *router.Router, config Config) {
	config = config.defaults()
	handler := Handler(config)

	if config.Prefix == "" || config.Prefix == "/" {
		// Root mount: catch-all handles everything including /
		r.Get("/{path...}", handler)
	} else {
		r.Get(config.Prefix+"/", handler)
		r.Get(config.Prefix+"/{path...}", handler)
	}
}

// hasDotDotSegment reports whether the *raw* (uncleaned) path contains a
// `..` segment. Run this before path.Clean so traversal segments can't be
// collapsed silently.
func hasDotDotSegment(p string) bool {
	for _, component := range strings.Split(p, "/") {
		if component == ".." {
			return true
		}
	}
	return false
}

// hasDotfileSegment reports whether any path component starts with a
// dot (excluding the bare "." and ".." segments path.Clean would have
// already resolved). Dotfiles routinely hold secrets (.env, .htpasswd)
// or VCS metadata (.git) and must not be served by a public static
// handler.
func hasDotfileSegment(p string) bool {
	for _, component := range strings.Split(p, "/") {
		if component == "" || component == "." || component == ".." {
			continue
		}
		if strings.HasPrefix(component, ".") {
			return true
		}
	}
	return false
}

// forbiddenConfigFiles is the set of well-known server-side config and
// app-metadata files we refuse to serve via the static handler. None of
// these have a legitimate reason to live behind the public file server,
// and probing for them is a standard reconnaissance step.
var forbiddenConfigFiles = map[string]struct{}{
	"web.config":             {}, // IIS site config
	"global.asax":            {}, // ASP.NET app entry
	"app.config":             {}, // .NET application config
	"machine.config":         {}, // .NET machine-wide config
	"applicationhost.config": {}, // IIS Express host config
}

// hasForbiddenConfigSegment matches the forbidden-config list case-
// insensitively so a request for `/Web.Config` is treated the same as
// `/web.config`.
func hasForbiddenConfigSegment(p string) bool {
	for _, component := range strings.Split(p, "/") {
		if component == "" {
			continue
		}
		if _, ok := forbiddenConfigFiles[strings.ToLower(component)]; ok {
			return true
		}
	}
	return false
}
