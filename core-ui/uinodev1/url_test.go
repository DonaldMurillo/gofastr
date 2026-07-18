package uinodev1

import "testing"

// TestIsValidHostRelative is the exhaustive URL-guard table required by
// design §9 (and the adversarial-case inventory). Every dangerous scheme,
// scheme-relative form, off-origin absolute URL, and whitespace/backslash
// smuggling vector MUST be rejected; only host-relative same-origin paths
// are accepted.
func TestIsValidHostRelative(t *testing.T) {
	// Each case is a (input, want) pair. The comment explains WHY each
	// rejected input is a real attack vector, not a theoretical concern.
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// --- accepted: host-relative same-origin paths ---
		{"root path", "/", true},
		{"simple path", "/dashboard", true},
		{"nested path", "/users/42/edit", true},
		{"with query", "/search?q=hello&pg=2", true},
		{"with fragment", "/docs/intro#section", true},
		{"with dot segments", "/a/../b", true},
		{"trailing slash", "/dashboard/", true},
		{"single char after slash", "/x", true},
		{"many segments", "/a/b/c/d/e/f/g/h", true},

		// --- rejected: empty / non-host-relative ---
		{"empty", "", false},
		{"relative bare", "foo", false},
		{"relative dot-slash", "./foo", false},
		{"relative dot-dot", "../escape", false},

		// --- rejected: scheme-relative ("//host") — off-origin ---
		{"scheme-relative bare", "//evil.com", false},
		{"scheme-relative path", "//evil.com/x", false},
		{"scheme-relative with https", "//evil.com/https://x", false},

		// --- rejected: dangerous schemes (also fail leading-slash, but
		//     listed explicitly so the test inventory is self-documenting) ---
		{"javascript scheme", "javascript:alert(1)", false},
		{"javascript scheme mixed case", "JaVaScRiPt:alert(1)", false},
		{"data scheme html", "data:text/html,<script>", false},
		{"vbscript scheme", "vbscript:msgbox", false},
		{"blob scheme", "blob:https://evil.com/uuid", false},
		{"file scheme", "file:///etc/passwd", false},

		// --- rejected: off-origin absolute URLs ---
		{"https absolute", "https://example.com", false},
		{"http absolute", "http://example.com/x", false},
		{"https absolute with port", "https://example.com:8443/x", false},
		{"localhost absolute", "http://localhost:3000", false},

		// --- rejected: backslash smuggling (magic-backslash browser bug) ---
		{"backslash host", "/\\evil.com", false},
		{"backslash path", "/path\\to", false},
		{"backslash before scheme", "/\\javascript:alert(1)", false},

		// --- rejected: whitespace / control bytes ---
		{"leading space", " /dashboard", false},
		{"trailing space", "/dashboard ", false},
		{"embedded tab", "/da\tshboard", false},
		{"embedded newline", "/da\nshboard", false},
		{"embedded cr", "/da\rshboard", false},
		{"embedded nul", "/da\x00shboard", false},
		{"embedded del", "/da\x7fshboard", false},
		{"embedded vt", "/da\x0bshboard", false},
		{"embedded ff", "/da\x0cshboard", false},

		// --- rejected: dangerous scheme tokens embedded in path
		//     (defense-in-depth — a downstream parser that extracts the
		//     query value and navigates could otherwise be confused) ---
		{"javascript in query", "/path?next=javascript:alert(1)", false},
		{"data in fragment", "/path#data:text/html,x", false},
		{"blob in path", "/blob:uuid", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := IsValidHostRelative(c.in)
			if got != c.want {
				t.Fatalf("IsValidHostRelative(%q) = %v; want %v", c.in, got, c.want)
			}
		})
	}
}
