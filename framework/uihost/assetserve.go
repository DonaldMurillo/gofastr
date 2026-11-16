package uihost

import (
	"net/http"
	"strconv"

	corestatic "github.com/DonaldMurillo/gofastr/core/static"
)

func serveVersionedText(w http.ResponseWriter, r *http.Request, contentType, body, hash string, private bool) {
	h := w.Header()
	h.Set("Content-Type", contentType)
	h.Set("X-Content-Type-Options", "nosniff")

	etag := ""
	if hash != "" {
		etag = `"` + hash + `"`
		h.Set("ETag", etag)
	}

	if private {
		h.Set("Cache-Control", "private, no-cache")
	} else if hash != "" && r.URL.Query().Get("v") == hash {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}

	if etag != "" && corestatic.IfNoneMatch(r, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(body))
}

// ScriptHandler serves js with a strong ETag, caching it immutably when
// the request's ?v= matches its content hash (the URL ScriptURL builds).
func ScriptHandler(js []byte) http.Handler {
	body := string(js)
	hash := hashStrings(body)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveVersionedText(w, r, "application/javascript; charset=utf-8", body, hash, false)
	})
}

// ScriptURL returns path plus "?v=" and the content hash of js, the URL to
// register for a script ScriptHandler serves.
func ScriptURL(path string, js []byte) string {
	return path + "?v=" + hashStrings(string(js))
}
