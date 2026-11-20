// Package urlsafe is the URL-scheme allow-list for caller-supplied URLs.
package urlsafe

import "slices"

import "strings"

// Policy names the scheme set a surface accepts.
type Policy int

const (
	// Anchor is for URLs the user activates: <a href>, <form action>.
	Anchor Policy = iota

	// Resource is for URLs the browser fetches: <script src>, <link href>.
	Resource

	// ImageSource is Resource plus raster data: URIs, for <img src>.
	ImageSource
)

var imageDataMediaTypes = []string{
	"image/jpeg",
	"image/png",
	"image/gif",
	"image/webp",
	"image/avif",
}

// OK reports whether u may be rendered into a URL attribute under p.
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
	if strings.IndexByte(trimmed, '\\') >= 0 {
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
	for i := 0; i < len(trimmed); i++ {
		switch trimmed[i] {
		case ':':
			scheme := strings.ToLower(trimmed[:i])
			if scheme == "data" {
				return p == ImageSource && dataImageOK(trimmed)
			}
			return schemeOK(scheme, p)
		case '/', '?', '#':
			return true
		}
	}
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

func dataImageOK(u string) bool {
	const prefix = "data:"
	if len(u) <= len(prefix) || !strings.EqualFold(u[:len(prefix)], prefix) {
		return false
	}
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

// Clean returns u when OK(u, p), and "" otherwise.
func Clean(u string, p Policy) string {
	if OK(u, p) {
		return u
	}
	return ""
}

// CleanAnchor is Clean(u, Anchor).
func CleanAnchor(u string) string {
	return Clean(u, Anchor)
}
