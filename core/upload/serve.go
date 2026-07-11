package upload

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
)

// scriptableHead reports whether a sniffed content type is one a
// browser will render and execute as active content. Served verbatim,
// an uploaded HTML document becomes a stored-XSS vector.
func scriptableHead(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.HasPrefix(ct, "text/html") ||
		strings.HasPrefix(ct, "application/xhtml") ||
		strings.HasPrefix(ct, "image/svg")
}

// scriptableExt reports whether the key's extension denotes scriptable
// content. SVG in particular is not recognized by
// [http.DetectContentType] (it sniffs as text/plain), so the extension
// is the reliable "derived" signal for it.
func scriptableExt(key string) bool {
	switch ext(key) {
	case "html", "htm", "xhtml", "svg":
		return true
	}
	return false
}

// ServeHandler returns an http.HandlerFunc that streams a file stored
// under storage back to the client. Mount it on the framework router
// with a catch-all key, e.g.
//
//	r.Get("/uploads/{key...}", upload.ServeHandler(storage))
//
// Semantics:
//
//   - GET and HEAD only; any other method yields 405 with an Allow header.
//   - The key is resolved from r.PathValue("key") (populated by the
//     router's {key...} wildcard), falling back to the URL path with a
//     leading slash stripped when no path value is set.
//   - The content type is sniffed from the first 512 bytes of the body
//     via [http.DetectContentType] — never from the client or the key.
//   - X-Content-Type-Options: nosniff is always set.
//   - Scriptable content (HTML, XHTML, SVG) is forced to
//     application/octet-stream with Content-Disposition: attachment so a
//     browser downloads it instead of rendering it (stored-XSS guard).
//   - Path-traversal defense is delegated entirely to the Storage
//     backend ([LocalStorage]'s sanitizeKey is the single enforcement
//     point); the handler performs no path manipulation of its own and
//     echoes no filesystem path on any error.
func ServeHandler(storage Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		key := r.PathValue("key")
		if key == "" {
			key = strings.TrimPrefix(r.URL.Path, "/")
		}

		rc, err := storage.Get(r.Context(), key)
		if err != nil {
			serveStoreError(w, err)
			return
		}
		defer rc.Close()

		// storage.Get returns an io.ReadCloser, not a Seeker, so sniff
		// the head into a buffer and re-assemble the full stream for the
		// response body via [io.MultiReader].
		head := make([]byte, 512)
		n, readErr := io.ReadFull(rc, head)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		head = head[:n]
		contentType := http.DetectContentType(head)

		w.Header().Set("X-Content-Type-Options", "nosniff")
		if scriptableHead(contentType) || scriptableExt(key) {
			// Stored-XSS guard: never let a browser render uploaded
			// HTML/SVG. Force a neutral, downloadable representation.
			// SVG sniffs as text/plain, so the key's extension is the
			// reliable "derived" signal for it.
			contentType = "application/octet-stream"
			w.Header().Set("Content-Disposition", "attachment")
		}
		w.Header().Set("Content-Type", contentType)

		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, io.MultiReader(bytes.NewReader(head), rc))
	}
}

// serveStoreError maps a Storage.Get error to an HTTP status without
// echoing any filesystem path. Missing keys are 404; keys rejected by
// backend sanitization (traversal / escaped paths) are 400; anything
// else is a generic 500.
func serveStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		// ErrNotFound wraps os.ErrNotExist, so this also matches any
		// backend that returns a stdlib not-exist error through it.
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrInvalidKey):
		http.Error(w, "invalid key", http.StatusBadRequest)
	default:
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
