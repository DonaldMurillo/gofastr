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

// splitFragment splits target at the first '#' into the portion before the
// fragment and the fragment itself (including the leading '#'), or (target, "")
// when target has no fragment.
func splitFragment(target string) (beforeFragment, fragment string) {
	if i := strings.IndexByte(target, '#'); i >= 0 {
		return target[:i], target[i:]
	}
	return target, ""
}

// WithQuery parses the query string of target before any #fragment, passes it
// to edit, and reassembles target with the updated query before the unchanged
// fragment. The query is re-encoded via url.Values.Encode, which sorts keys
// and drops pairs url.ParseQuery rejects (malformed percent-escapes and
// semicolon-separated pairs).
func WithQuery(target string, edit func(url.Values)) string {
	beforeFrag, frag := splitFragment(target)
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
