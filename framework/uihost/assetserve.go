package uihost

import (
	"net/http"
	"strconv"
)

// serveVersionedText is the one policy for every text asset under
// /__gofastr/* (stylesheets, runtime JS, manifests): strong ETag + 304
// on every response, and immutable caching exactly when the request's
// ?v= matches the asset's current hash. An un-versioned (or stale-
// versioned) request gets no-cache, always revalidated and never wrong,
// but still benefits from the ETag round-trip.
//
// private marks per-session assets (compiled actions): shared caches
// must never store them, browser caches may.
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
		// Gated assets are NEVER immutable: a year-long browser cache
		// entry outlives the credential that earned it, so a later user
		// of the same profile (or the same user after sign-out) would be
		// served the script without the gate ever running. private +
		// no-cache keeps the gate on every request; the ETag makes each
		// one a body-less 304.
		h.Set("Cache-Control", "private, no-cache")
	} else if hash != "" && r.URL.Query().Get("v") == hash {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}

	if etag != "" && r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write([]byte(body))
}
