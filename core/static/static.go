// Package static provides a static file server for Go's embed.FS (or any fs.FS),
// with ETag-based caching, configurable Cache-Control headers, MIME type
// detection, SPA fallback, and optional directory listing.
package static

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gofastr/gofastr/core/router"
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
	// Do not set this field — it is currently ignored.
	DirListing bool
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

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only serve GET and HEAD.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		// Clean the request path and strip the prefix.
		reqPath := path.Clean(r.URL.Path)
		if config.Prefix != "" {
			reqPath = strings.TrimPrefix(reqPath, config.Prefix)
		}
		// Ensure leading slash is removed for filesystem lookup.
		reqPath = strings.TrimPrefix(reqPath, "/")

		if reqPath == "" {
			reqPath = config.IndexFile
		}

		// Prevent directory traversal.
		if containsDotDot(reqPath) {
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
		return false
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return false
	}

	// If it's a directory, try to serve the index file.
	if stat.IsDir() {
		indexPath := path.Join(name, config.IndexFile)
		return serveFile(w, r, config, indexPath)
	}

	// Read file content for ETag generation.
	data, err := io.ReadAll(f)
	if err != nil {
		return false
	}

	// Generate ETag from content.
	etag := generateETag(data)

	// Get modification time.
	modTime := stat.ModTime()

	// Set cache headers.
	setCacheHeaders(w, etag, modTime, config.MaxAge)

	// Check conditional request (304 Not Modified).
	if checkPreconditions(r, etag, modTime) {
		w.WriteHeader(http.StatusNotModified)
		return true
	}

	// Set content type.
	contentType := DetectFromName(name)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))

	// Write the file content.
	w.Write(data)
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

// containsDotDot checks if the path contains ".." components that could
// be used for directory traversal.
func containsDotDot(p string) bool {
	// Clean the path using filepath.Clean for OS-specific checks,
	// but use path.Clean for URL path cleaning.
	cleaned := path.Clean(p)
	for _, component := range strings.Split(cleaned, "/") {
		if component == ".." {
			return true
		}
	}
	return false
}
