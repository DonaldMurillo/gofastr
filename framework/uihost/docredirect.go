package uihost

import (
	"net/http"
	"net/url"
)

// writeDocumentRedirect answers a runtime fetch whose policy sends the
// reader to an absolute http(s) URL: 200 with the URL in
// X-Gofastr-Document, which the runtime loads as a page. A 303 there
// is followed inside fetch, and a site that refuses CORS (an identity
// provider's login, say) failed the navigation with a "could not load"
// toast. It reports false, and writes nothing, for any other target,
// which keeps its 303.
func writeDocumentRedirect(w http.ResponseWriter, target string) bool {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Gofastr-Document", scrubCtl(target))
	w.WriteHeader(http.StatusOK)
	return true
}

// scriptFetch reports whether a request came from a script's fetch
// rather than a document load. Sec-Fetch-Mode is "navigate" for the
// latter and cannot be set by script; a request without it (curl, a
// browser that predates Fetch Metadata) counts as a document load.
func scriptFetch(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Mode") {
	case "cors", "same-origin":
		return true
	}
	return false
}
