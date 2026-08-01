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

import "slices"

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

	// Resource is for URLs the browser fetches on its own: <source src>,
	// <script src>, <link href>, and head/meta URLs. Accepts http and
	// https plus relative references — nothing else.
	Resource

	// ImageSource is Resource plus inline raster `data:` URIs, for the one
	// sink where those are a feature rather than a mistake: <img src>.
	//
	// It is a separate policy precisely so the loosening cannot reach
	// <script src> or <link href>, where a data: URI is a code-execution
	// path. Only the raster media types this project's image pipeline can
	// produce are accepted; `data:image/svg+xml` is excluded because SVG is
	// a markup surface, and relying on browsers keeping img-loaded SVG
	// script-disabled is a weaker guarantee than never emitting it.
	ImageSource
)

// imageDataMediaTypes is the allow-list of media types ImageSource accepts
// in a data: URI. Raster only — see the ImageSource doc comment.
var imageDataMediaTypes = []string{
	"image/jpeg",
	"image/png",
	"image/gif",
	"image/webp",
	"image/avif",
}

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
			scheme := strings.ToLower(trimmed[:i])
			// data: is gated on the media type, not the scheme alone, and
			// only ImageSource admits it at all.
			if scheme == "data" {
				return p == ImageSource && dataImageOK(trimmed)
			}
			return schemeOK(scheme, p)
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

// dataImageOK reports whether u is a `data:` URI carrying one of the
// allow-listed raster media types. u is the caller's original string; the
// media type is matched case-insensitively because `data:IMAGE/PNG` is
// equivalent per RFC 2397, and a case-sensitive check would be a trivial
// bypass of the allow-list.
func dataImageOK(u string) bool {
	const prefix = "data:"
	if len(u) <= len(prefix) || !strings.EqualFold(u[:len(prefix)], prefix) {
		return false
	}
	// A data: URI needs a payload delimiter; everything before it is the
	// media type plus optional parameters (";base64", ";charset=…").
	comma := strings.IndexByte(u, ',')
	if comma < 0 {
		return false
	}
	meta := strings.ToLower(u[len(prefix):comma])
	mediaType := meta
	if before, _, ok := strings.Cut(meta, ";"); ok {
		mediaType = before
	}
	return slices.Contains(imageDataMediaTypes, mediaType)
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

// CleanAnchor returns u when it is safe to render as an href / action /
// formaction value (http(s), relative, fragment, mailto, tel) under the
// Anchor policy, and "" otherwise. It is the one shared helper for anchor-
// style URL sinks; framework/ui and the breadcrumbs, nestedlist and tree
// patterns all delegate here instead of each re-wrapping Clean(u, Anchor).
func CleanAnchor(u string) string {
	return Clean(u, Anchor)
}
