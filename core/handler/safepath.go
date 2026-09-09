package handler

import (
	"net/url"
	"strings"
)

// IsSafeRelativePath reports whether p is a same-origin, path-only
// redirect target: it must start with a single "/", carry no scheme or
// host (also after percent-decoding, where "/%2Fevil" would become
// "//evil"), and contain none of backslash, NUL, CR, LF, or TAB in
// either the raw or the decoded form, since browsers decode before they
// navigate and any of those bytes changes what the redirect means.
//
// It is the one implementation of the check formerly duplicated as
// battery/auth's isSafeRelativePath (the "next" / return-to form field)
// and framework/uihost's isSafePartialRedirect (the X-Gofastr-Location
// partial-redirect header).
func IsSafeRelativePath(p string) bool {
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
