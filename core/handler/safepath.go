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

// WithQuery parses the query string of target before any #fragment, passes it
// to edit, and reassembles target with the updated query before the unchanged
// fragment. Malformed percent-escapes in an existing query are skipped rather
// than failing.
func WithQuery(target string, edit func(url.Values)) string {
	beforeFrag, frag := target, ""
	if i := strings.IndexByte(target, '#'); i >= 0 {
		beforeFrag, frag = target[:i], target[i:]
	}
	path, rawQuery, _ := strings.Cut(beforeFrag, "?")
	q, _ := url.ParseQuery(rawQuery)
	if q == nil {
		q = url.Values{}
	}
	if edit != nil {
		edit(q)
	}
	if len(q) == 0 {
		return path + frag
	}
	return path + "?" + q.Encode() + frag
}
