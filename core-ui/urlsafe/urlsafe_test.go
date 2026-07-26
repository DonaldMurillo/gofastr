package urlsafe

import "testing"

// This package is the single enforcement point for every surface that renders
// a caller-supplied URL — framework/ui's anchors and images, uihost's head
// tags, crud's stored media paths, apiversions' deprecation hints, and three
// core-ui/patterns builders. A hole here is a hole in all of them, so the
// table is deliberately exhaustive rather than illustrative.

func TestRejectedUnderEveryPolicy(t *testing.T) {
	bad := []struct{ url, why string }{
		{"", "empty is not a URL"},
		{"javascript:alert(1)", "script-executing scheme"},
		{"JavaScript:alert(1)", "scheme match is case-insensitive"},
		{"  javascript:alert(1)", "leading whitespace does not hide the scheme"},
		{"\tjavascript:alert(1)", "leading tab does not hide the scheme"},
		{"vbscript:msgbox(1)", "script-executing scheme"},
		{"data:text/html,<script>alert(1)</script>", "data: renders attacker HTML"},
		{"file:///etc/passwd", "local file scheme"},
		{"blob:https://x/y", "blob scheme"},
		{"filesystem:https://x/y", "filesystem scheme"},
		{"view-source:https://x", "view-source scheme"},
		{"chrome://settings", "browser-internal scheme"},
		{"//evil.example/x", "protocol-relative inherits the page scheme"},
		{"/x\x00y", "NUL byte"},
		{"/x\ny", "raw LF splits the attribute"},
		{"/x\ry", "raw CR splits the attribute"},
		{"/x\x7fy", "DEL byte"},
		{"/x%0dSet-Cookie:%20a=b", "percent-encoded CR smuggles a header"},
		{"/x%0aSet-Cookie:%20a=b", "percent-encoded LF smuggles a header"},
		{"/x%0D%0Ay", "percent-encoded CRLF, uppercase"},
	}
	for _, tc := range bad {
		for _, p := range []Policy{Anchor, Resource} {
			if OK(tc.url, p) {
				t.Errorf("SECURITY: OK(%q, %v) = true — %s", tc.url, p, tc.why)
			}
			if Clean(tc.url, p) != "" {
				t.Errorf("SECURITY: Clean(%q, %v) returned it — %s", tc.url, p, tc.why)
			}
		}
	}
}

func TestAcceptedUnderEveryPolicy(t *testing.T) {
	good := []string{
		"https://example.com/x",
		"HTTPS://example.com/x",
		"http://example.com/x",
		"/absolute/path",
		"relative/path",
		"logo.png",
		"./sibling",
		"../parent",
		"#fragment",
		"?query=1",
		"/path?q=1#frag",
		"/path/with:colon/after/slash",
		"?a=b:c",
		"#a:b",
	}
	for _, u := range good {
		for _, p := range []Policy{Anchor, Resource} {
			if !OK(u, p) {
				t.Errorf("OK(%q, %v) = false, want true", u, p)
			}
			if Clean(u, p) != u {
				t.Errorf("Clean(%q, %v) = %q, want the input back", u, p, Clean(u, p))
			}
		}
	}
}

// The policy split is the whole reason there are two constants: mailto: on an
// <a href> is a feature, and mailto: on an <img src> is a caller mistake at
// best. A single policy would have to pick one and be wrong somewhere.
func TestPolicySplit(t *testing.T) {
	for _, u := range []string{"mailto:a@b.com", "tel:+15551234", "MAILTO:a@b.com"} {
		if !OK(u, Anchor) {
			t.Errorf("OK(%q, Anchor) = false; navigational schemes belong on anchors", u)
		}
		if OK(u, Resource) {
			t.Errorf("OK(%q, Resource) = true; the browser must not fetch this as a subresource", u)
		}
	}
}

// A colon that appears after a path delimiter is not a scheme. Treating it as
// one would drop legitimate URLs, and the failure mode of an over-strict guard
// is a silently missing link rather than a visible error.
func TestColonAfterDelimiterIsNotAScheme(t *testing.T) {
	for _, u := range []string{"/a/b:c", "a/b:c", "?x=1:2", "#x:y"} {
		if !OK(u, Resource) {
			t.Errorf("OK(%q) = false; the colon is inside the path, not a scheme", u)
		}
	}
}
