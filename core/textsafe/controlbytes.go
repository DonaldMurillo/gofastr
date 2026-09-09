package textsafe

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// HasControlBytes reports whether s carries any ASCII C0 control byte
// (0x00–0x1F) or DEL (0x7F). It walks bytes, not runes: a valid UTF-8
// sequence never contains a byte below 0x20, so the byte walk answers
// exactly "does a 7-bit control byte appear". This is deliberately NOT
// the rune-based IsUnsafe/ContainsUnsafe set above — C1 controls and
// the zero-width/bidi codepoints are out of scope; the callers it
// replaces gate header values, log fast paths, cache keys, and file
// names on the ASCII control range only.
//
// Replaces the five byte-identical copies: cmd/repolint hasControlChar,
// core/handler needsHeaderSanitize, core/middleware containsCtrl,
// framework/ui hasCtl (screen_cache.go) and hasControlBytes (safety.go).
func HasControlBytes(s string) bool {
	for i := range s {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// SanitizeControlBytes removes every C0 control byte and DEL from s,
// leaving all other bytes — including non-ASCII — untouched. The fast
// path returns s unchanged when it is already clean. Response header
// values, CORS tokens, route-group prefixes, and DSL literals pass
// through this so a CR/LF/NUL cannot smuggle a second header line or a
// forged log line downstream. It REMOVES the bytes rather than
// percent-encoding them; percent-encoding is ScrubControlBytes' contract,
// for values rendered into log lines.
//
// Replaces the four body-identical copies: core/handler
// sanitizeHeaderValue, core/middleware stripCtrlBytes, framework/
// routegroup stripPrefixCtrlBytes, framework/dsl stripDSLControlBytes.
func SanitizeControlBytes(s string) string {
	if !HasControlBytes(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := range s {
		c := s[i]
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// needsControlScrub is the fast-path probe for ScrubControlBytes. It
// must flag a SUPERSET of what the encoder rewrites: the C0 controls
// and DEL, plus every non-ASCII byte (which may open an unsafe rune or
// itself be a stray 8-bit C1 control). A clean ASCII string returns
// unchanged without entering the encoder; a shape the probe misses
// would be logged raw, so the superset rule is load-bearing — the
// earlier hand-written probe omitted most of the C0 range (SOH, EOT,
// FS, …) and those leaked.
func needsControlScrub(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return r < 0x20 || r == 0x7f || r >= utf8.RuneSelf
	})
}

// ScrubControlBytes percent-encodes every character that can forge,
// break, or reorder a rendered log line in a request-derived value, URL
// path, method, or a panic that embeds a request string: the C0
// controls and DEL, the C1 controls (U+0080–U+009F — the 8-bit CSI
// 0x9B and OSC 0x9D drive terminal escapes exactly as ESC-[ does, NEL
// 0x85 breaks the line), and the zero-width/bidi set (RLO and friends
// visually rewrite the logged path). An attacker then can't forge a
// fake log entry, reorder one, or smuggle a terminal-control payload
// into an operator's tail/less session. slog's JSON handler escapes C0
// for valid JSON but leaves C1/bidi runes raw (verified 2026-09-05: a
// raw C2 9B lands in the encoded line), and a JSON-escaped \r\n is
// still visible to text grep, with naive log shippers rendering the
// injected payload on its own line.
//
// r.URL.Path is percent-DECODED, so %0d%0a / %c2%9b / %e2%80%ae in the
// raw request are a real CRLF / U+009B / U+202E by the time they reach
// any sink here. Stray non-UTF-8 bytes in 0x80..0x9F are the 8-bit C1
// forms on the wire (a bare 0x9B from %9B) and are encoded like their
// rune counterparts; other invalid bytes pass through untouched.
//
// Replaces the two byte-identical copies: core/middleware
// scrubControlBytes (logging.go) and battery/log scrubControlBytes
// (middleware.go), which had drifted into maintained parity.
func ScrubControlBytes(s string) string {
	if !needsControlScrub(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, "%%%02x", c)
			} else {
				b.WriteByte(c)
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 1 {
			// Stray invalid byte: in 0x80..0x9F it is an 8-bit
			// C1 control on the wire, encode it.
			if c <= 0x9f {
				fmt.Fprintf(&b, "%%%02x", c)
			} else {
				b.WriteByte(c)
			}
			i++
			continue
		}
		if IsUnsafe(r) {
			for j := i; j < i+size; j++ {
				fmt.Fprintf(&b, "%%%02x", s[j])
			}
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}
