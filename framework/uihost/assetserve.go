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

// ScriptHandler serves js as "application/javascript; charset=utf-8" with
// nosniff, a strong ETag, and immutable caching when the request's ?v=
// matches the content hash — the same serveVersionedText policy every
// /__gofastr script follows. The hash is computed once, at construction.
//
// It is the serving half for host-app page scripts registered via
// (*UIHost).RegisterExternalScript; see ScriptURL for the URL to register.
func ScriptHandler(js []byte) http.Handler {
	body := string(js)
	hash := hashStrings(body)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveVersionedText(w, r, "application/javascript; charset=utf-8", body, hash, false)
	})
}

// ScriptURL returns path plus "?v=" + the content hash of js: the URL to
// pass to RegisterExternalScript so the bytes ScriptHandler serves cache
// immutably and cache-bust automatically whenever js changes.
func ScriptURL(path string, js []byte) string {
	return path + "?v=" + hashStrings(string(js))
}
