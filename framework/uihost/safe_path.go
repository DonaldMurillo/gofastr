package uihost

import (
	"net/url"
	"strings"
)

// isSafePartialRedirect returns true when p is safe to emit via the
// X-Gofastr-Location header on a partial-fetch response. The header
// is read by the runtime and fed into loadPage(), which fetches with
// credentials — so an unsafe value (scheme, absolute URL, protocol-
// relative, encoded backslash bypass) would turn the partial-redirect
// signal into a cross-origin credential leak.
//
// Requires: starts with a single '/', no scheme, no host, no
// backslash, no control bytes, not "//"-prefixed (protocol-relative).
// Both the raw input AND the percent-decoded path are checked — an
// attacker can supply "/%5Cevil.example" which the surface rules pass
// (no literal backslash) but the browser decodes to "/\evil…" and
// normalises to "//evil…" cross-origin.
func isSafePartialRedirect(p string) bool {
	if p == "" {
		return false
	}
	if !strings.HasPrefix(p, "/") {
		return false
	}
	if strings.HasPrefix(p, "//") {
		return false
	}
	if strings.ContainsAny(p, "\\\x00\r\n\t") {
		return false
	}
	u, err := url.Parse(p)
	if err != nil {
		return false
	}
	if u.Scheme != "" || u.Host != "" {
		return false
	}
	if strings.ContainsAny(u.Path, "\\\x00\r\n\t") {
		return false
	}
	if strings.HasPrefix(u.Path, "//") {
		return false
	}
	return true
}
