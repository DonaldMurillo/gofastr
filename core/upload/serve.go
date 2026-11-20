package upload

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func scriptableHead(ct string) bool {
	ct = strings.ToLower(ct)
	return strings.HasPrefix(ct, "text/html") ||
		strings.HasPrefix(ct, "application/xhtml") ||
		strings.HasPrefix(ct, "image/svg") ||
		strings.HasPrefix(ct, "text/xml") ||
		strings.HasPrefix(ct, "application/xml") ||
		strings.HasPrefix(ct, "text/xsl") ||
		strings.HasPrefix(ct, "application/xslt")
}

func scriptableExt(key string) bool {
	switch ext(key) {
	case "html", "htm", "xhtml", "svg", "xml", "xsl", "xslt":
		return true
	}
	return false
}

// ServeHandler streams the object under r.PathValue("key") (or the URL
// path) to GET and HEAD with a sniffed Content-Type and nosniff, sends
// HTML, XHTML, SVG, XML and XSLT as an octet-stream attachment, and
// answers Range when storage is a RangeGetter.
func ServeHandler(storage Storage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			i18n.HTTPError(w, r, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
			return
		}

		key := r.PathValue("key")
		if key == "" {
			key = strings.TrimPrefix(r.URL.Path, "/")
		}

		if rg, ok := storage.(RangeGetter); ok {
			rs, err := rg.GetRange(r.Context(), key)
			if err != nil {
				serveStoreError(w, r, err)
				return
			}
			defer rs.Close()
			serveSeekable(w, r, key, rs)
			return
		}

		rc, err := storage.Get(r.Context(), key)
		if err != nil {
			serveStoreError(w, r, err)
			return
		}
		defer rc.Close()

		head := make([]byte, 512)
		n, readErr := io.ReadFull(rc, head)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		head = head[:n]

		setServeHeaders(w, key, http.DetectContentType(head))

		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = io.Copy(w, io.MultiReader(bytes.NewReader(head), rc))
	}
}

func serveSeekable(w http.ResponseWriter, r *http.Request, key string, rs io.ReadSeeker) {
	head := make([]byte, 512)
	n, readErr := io.ReadFull(rs, head)
	if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	if _, err := rs.Seek(0, io.SeekStart); err != nil {
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}

	setServeHeaders(w, key, http.DetectContentType(head[:n]))

	http.ServeContent(w, r, "", time.Time{}, rs)
}

func setServeHeaders(w http.ResponseWriter, key, contentType string) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if scriptableHead(contentType) || scriptableExt(key) {
		contentType = "application/octet-stream"
		w.Header().Set("Content-Disposition", "attachment")
	}
	w.Header().Set("Content-Type", contentType)
}

func serveStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		i18n.HTTPError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
	case errors.Is(err, ErrInvalidKey):
		i18n.HTTPError(w, r, http.StatusBadRequest, reasonInvalidKey)
	default:
		i18n.HTTPError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
	}
}
