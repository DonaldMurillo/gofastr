package uihost

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
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
	w.Header().Set("X-Gofastr-Document", escapeNonASCII(scrubCtl(target)))
	w.WriteHeader(http.StatusOK)
	return true
}

// escapeNonASCII percent-encodes every byte >= 0x80, as http.Redirect
// does for Location. fetch decodes a header value as Latin-1, so raw
// UTF-8 would reach location.replace as different characters.
func escapeNonASCII(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= 0x80 {
			fmt.Fprintf(&b, "%%%02X", c)
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

// escapeLayerKey percent-encodes a layer key for X-Gofastr-Swap: every
// byte >= 0x80, and % itself, so the runtime's decodeURIComponent gives
// back exactly the key the DOM holds (a {param} value's %25 included).
// An ASCII key with no % goes out unchanged.
func escapeLayerKey(s string) string {
	if !strings.ContainsFunc(s, func(r rune) bool { return r == '%' || r >= 0x80 }) {
		return s
	}
	return escapeNonASCII(strings.ReplaceAll(s, "%", "%25"))
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

// carryQuery appends the request's query to a declarative redirect
// target, so /old-pricing?plan=pro lands on /pricing?plan=pro the way
// the trailing-slash redirect keeps its query. A key the target sets
// itself wins and the request's other pairs follow it in request
// order; a fragment in the target stays last.
func carryQuery(target, rawQuery string) string {
	if rawQuery == "" {
		return target
	}
	rest, frag, hasFrag := strings.Cut(target, "#")
	path, own, _ := strings.Cut(rest, "?")
	ownKeys := map[string]bool{}
	pairs := make([]string, 0, 4)
	for pair := range strings.SplitSeq(own, "&") {
		if pair == "" {
			continue
		}
		ownKeys[queryKey(pair)] = true
		pairs = append(pairs, pair)
	}
	for pair := range strings.SplitSeq(rawQuery, "&") {
		if pair == "" || ownKeys[queryKey(pair)] {
			continue
		}
		pairs = append(pairs, pair)
	}
	out := path
	if len(pairs) > 0 {
		out += "?" + strings.Join(pairs, "&")
	}
	if hasFrag {
		out += "#" + frag
	}
	return out
}

// queryKey is a query pair's decoded key, or its raw text when it does
// not decode.
func queryKey(pair string) string {
	k, _, _ := strings.Cut(pair, "=")
	if dk, err := url.QueryUnescape(k); err == nil {
		return dk
	}
	return k
}
