package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
)

// safeURL returns u if it is safe to render as an href / src / action /
// formaction value, and "" otherwise. Safe URLs are http(s), relative
// paths, fragment-only references, and a small set of always-safe
// well-known schemes (mailto, tel). Everything else — javascript:,
// data:, vbscript:, file:, blob:, filesystem:, chrome:, view-source:,
// protocol-relative URLs, and any value with embedded CR/LF/NUL — is
// dropped. Encoded CR/LF (%0d/%0a) is also dropped because consumers
// that decode the URL would otherwise see header smuggling.
//
// This is the framework-side enforcement layer that earlier
// architecture iterations expected callers to handle. Callers that
// need a legitimate non-http(s) scheme can render the raw anchor via
// core-ui/html directly — UI builders no longer make that decision.
func safeURL(u string) string {
	if u == "" {
		return ""
	}
	// Reject control bytes outright.
	for i := 0; i < len(u); i++ {
		c := u[i]
		if c < 0x20 || c == 0x7f {
			return ""
		}
	}
	trimmed := strings.TrimLeft(u, " \t")
	low := strings.ToLower(trimmed)
	// Reject percent-encoded CR/LF as a downstream header-smuggling
	// primitive. Real content rarely contains those byte sequences.
	if strings.Contains(low, "%0d") || strings.Contains(low, "%0a") {
		return ""
	}
	// Protocol-relative URLs are ambiguous about origin trust.
	if strings.HasPrefix(trimmed, "//") {
		return ""
	}
	// Fragment-only or relative paths pass.
	if strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "?") || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") {
		return u
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		if c == ':' {
			scheme := strings.ToLower(trimmed[:i])
			switch scheme {
			case "http", "https", "mailto", "tel":
				return u
			default:
				return ""
			}
		}
		if c == '/' || c == '?' || c == '#' {
			// No scheme — relative reference, allowed.
			return u
		}
	}
	// No colon — bare relative reference.
	return u
}

// scrubAttrs filters an html.Attrs map, removing keys that look like
// inline event handlers (the on* family) or whose values contain
// control bytes. Returns a fresh Attrs so the caller's map is not
// mutated. Nil input yields nil.
//
// ExtraAttrs is a legitimate escape hatch — it lets callers add
// data-* / aria-* / hx-* / dir / lang etc. without a typed knob per
// case. The escape hatch stops at on-event handlers because those
// turn the escape hatch into a stored-XSS primitive when the host
// surface is dynamic (an article body, a search result, a CMS field).
func scrubAttrs(in html.Attrs) html.Attrs {
	if len(in) == 0 {
		return in
	}
	out := make(html.Attrs, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "on") {
			continue
		}
		// Attribute names with control bytes are always wrong.
		if hasControlBytes(k) || hasControlBytes(v) {
			continue
		}
		out[k] = v
	}
	return out
}

func hasControlBytes(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}
