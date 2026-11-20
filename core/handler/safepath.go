package handler

import (
	"net/url"
	"strings"
)

// IsSafeRelativePath reports whether p is a single-slash, path-only
// redirect target with no backslash, NUL, CR, LF or tab, raw or decoded.
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

// SafeRelativePathOr returns p if IsSafeRelativePath accepts it. Otherwise it
// returns the caller's trusted fallback unchanged.
func SafeRelativePathOr(p, fallback string) string {
	if IsSafeRelativePath(p) {
		return p
	}
	return fallback
}
