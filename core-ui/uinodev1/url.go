package uinodev1

// IsValidHostRelative reports whether s is a host-relative same-origin
// path that is safe to use as a link/image target produced by a third-party
// module. This is the semantic URL guard required by design §9 — it is NOT
// a substitute for CSP, and CSP is not a substitute for it.
//
// Acceptance criteria (all must hold):
//
//   - s is non-empty.
//   - s starts with a single "/": absolute same-origin path syntax.
//   - s does NOT start with "//": that is scheme-relative (e.g.
//     "//evil.com/x") and would be resolved against an attacker's origin.
//   - s contains no scheme: any "scheme:" form (javascript:, data:,
//     vbscript:, blob:, file:, http:, https:, …) requires the bytes
//     before the first ":" to be a valid scheme, which is impossible
//     here because s starts with "/". We additionally reject any
//     dangerous scheme token appearing later in the string as
//     defense-in-depth (see hasDangerousScheme).
//   - s contains no backslashes ("\"): browsers treat "\" as "/" in
//     some URL-parsing contexts (the "magic backslash" bug), which
//     can defeat scheme checks.
//   - s contains no whitespace, control bytes, or DEL: these are used
//     to smuggle past parsers and to construct header-splitting /
//     redirect-to-evil payloads.
//
// Rejected examples (non-exhaustive):
//
//	""                          // empty
//	"foo"                       // relative, not host-relative
//	"//evil.com/x"              // scheme-relative
//	"/\\evil.com"               // backslash smuggling
//	"/path\nwith-newline"       // control char
//	"javascript:alert(1)"       // scheme (also fails the leading-slash check)
//	"https://example.com"       // absolute off-origin (fails leading-slash)
//	"data:text/html,..."        // scheme
//	"vbscript:msgbox"           // scheme
//	"blob:..."                  // scheme
//	"file:///etc/passwd"        // scheme
//
// Accepted examples (non-exhaustive):
//
//	"/"
//	"/dashboard"
//	"/users/42/edit"
//	"/search?q=hello&pg=2"
//	"/path/with-dots/../up"
func IsValidHostRelative(s string) bool {
	if s == "" {
		return false
	}
	// Must start with a single '/'. Anything else is either a scheme
	// (javascript:, https:, …) or a relative path (foo, ./foo) — both
	// rejected by design §9 for module-originated URLs.
	if s[0] != '/' {
		return false
	}
	// Reject scheme-relative "//host" — would resolve off-origin.
	if len(s) >= 2 && s[1] == '/' {
		return false
	}
	for i := range s {
		b := s[i]
		// Reject backslash: browsers coerce "\" to "/" in URL parsing,
		// which can defeat the "//host" check above (e.g. "/\\host").
		if b == '\\' {
			return false
		}
		// Reject whitespace (space + tab + newline + CR + ...), control
		// bytes (0x00–0x1F), and DEL (0x7F). These are classic smuggling
		// vectors for parser-confusion and header-splitting payloads.
		// 0x20 is space; we include it because trailing/leading space is
		// a known browser-parser confusion vector.
		if b <= 0x20 || b == 0x7F {
			return false
		}
	}
	// Defense-in-depth: scan for a scheme-like token anywhere. Because
	// s already starts with '/', a literal "scheme:" form is impossible
	// at position 0 — but a value like "/path?next=blob:evil" or a
	// fragment could carry a scheme that confuses a downstream parser.
	// We reject any dangerous scheme token as a contiguous substring.
	if hasDangerousScheme(s) {
		return false
	}
	return true
}

// dangerousSchemes is the denylist used by hasDangerousScheme. The list
// is intentionally short and explicit; "http(s)" are NOT here because a
// host-relative path may LEGITIMATELY contain "http" as a substring
// (e.g. "/proxy?url=http://example.com" is a same-origin path, and the
// security issue there lives at the proxy endpoint, not the URL string).
var dangerousSchemes = []string{
	"javascript:",
	"data:",
	"vbscript:",
	"blob:",
	"file:",
}

// hasDangerousScheme reports whether s contains any of the dangerous
// scheme tokens, case-insensitively, as a contiguous substring.
func hasDangerousScheme(s string) bool {
	lo := make([]byte, len(s))
	for i := range s {
		b := s[i]
		if b >= 'A' && b <= 'Z' {
			b += 'a' - 'A'
		}
		lo[i] = b
	}
	for _, scheme := range dangerousSchemes {
		if containsSubstring(string(lo), scheme) {
			return true
		}
	}
	return false
}

// containsSubstring is a tiny strings.Contains replacement that keeps
// this package stdlib-only and allocation-free for short inputs.
func containsSubstring(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j := range needle {
			if haystack[i+j] != needle[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
