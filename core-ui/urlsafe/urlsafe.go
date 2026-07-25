// Package urlsafe holds the one URL-scheme allow-list every surface that
// renders a caller-supplied URL runs through.
//
// It exists because the same ~40-line guard had been re-derived five times
// — framework/ui.safeURL, framework/uihost.isSafeHeadURL,
// framework/crud.isSafeMediaURL, framework/experimental/apiversions, and
// core-ui/patterns/combobox.safePushHref — while core-ui/html, the layer
// all of them render through, had none. Copies drift; a copy that does not
// exist is worse. New URL sinks call this package rather than growing a
// sixth copy.
package urlsafe

import "strings"

// Policy names the scheme set a surface accepts. The split is not
// decoration: `mailto:` on an <a href> is a feature and `mailto:` on an
// <img src> or <script src> is a caller mistake at best.
type Policy int

const (
	// Anchor is for navigational URLs the user activates: <a href>,
	// <form action>, <area href>. Accepts http, https, mailto, tel, plus
	// relative, absolute-path, query-only and fragment-only references.
	Anchor Policy = iota

	// Resource is for URLs the browser fetches on its own: <img src>,
	// <source src>, <script src>, <link href>, and head/meta URLs.
	// Accepts http and https plus relative references — nothing else.
	Resource
)

// OK reports whether u may be rendered into a URL attribute under p.
//
// Rejected for every policy, before any scheme check:
//   - C0 control bytes and DEL, which split attributes and headers
//   - percent-encoded CR/LF, which smuggles a header line past any
//     consumer that decodes the URL before re-emitting it
//   - protocol-relative (`//host/…`) references, which silently inherit
//     the page scheme and are ambiguous about origin trust
//
// The empty string is not a URL and is rejected; callers that want to
// treat "" as "no URL" should check for it before calling.
func OK(u string, p Policy) bool {
	if u == "" {
		return false
	}
	for i := 0; i < len(u); i++ {
		if c := u[i]; c < 0x20 || c == 0x7f {
			return false
		}
	}
	trimmed := strings.TrimLeft(u, " \t")
	low := strings.ToLower(trimmed)
	if strings.Contains(low, "%0d") || strings.Contains(low, "%0a") {
		return false
	}
	if strings.HasPrefix(trimmed, "//") {
		return false
	}
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "#") ||
		strings.HasPrefix(trimmed, "?") || strings.HasPrefix(trimmed, "./") ||
		strings.HasPrefix(trimmed, "../") {
		return true
	}
	// Walk to the first delimiter. A ':' before any of / ? # means the
	// prefix was a scheme; any other delimiter first means there is no
	// scheme and this is a relative reference.
	for i := 0; i < len(trimmed); i++ {
		switch trimmed[i] {
		case ':':
			return schemeOK(strings.ToLower(trimmed[:i]), p)
		case '/', '?', '#':
			return true
		}
	}
	// No colon at all — a bare relative reference like "logo.png".
	return true
}

func schemeOK(scheme string, p Policy) bool {
	switch scheme {
	case "http", "https":
		return true
	case "mailto", "tel":
		return p == Anchor
	default:
		return false
	}
}

// Clean returns u when OK(u, p), and "" otherwise. Convenient for the
// common `attr = urlsafe.Clean(caller, urlsafe.Anchor)` shape where an
// empty value means "omit the attribute".
func Clean(u string, p Policy) string {
	if OK(u, p) {
		return u
	}
	return ""
}
