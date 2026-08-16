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
		// Browsers normalize '\' to '/' at the authority boundary, so each
		// of these is //evil.example by the time it is resolved. `/\` is
		// the one that matters most: it starts with '/', so before the
		// backslash rule it passed as an ordinary relative reference.
		{"\\\\evil.example/x", "double backslash normalizes to protocol-relative"},
		{"\\/evil.example/x", "backslash-slash normalizes to protocol-relative"},
		{"/\\evil.example/x", "slash-backslash normalizes to protocol-relative"},
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

func TestImageSourceAcceptsRasterDataURLs(t *testing.T) {
	for _, u := range []string{
		"data:image/jpeg;base64,/9j/4AAQ",
		"data:image/png;base64,iVBORw0KGgo=",
		"data:image/gif;base64,R0lGODlh",
		"data:image/webp;base64,UklGRg==",
		"data:image/avif;base64,AAAAIGZ0",
		"data:image/PNG;BASE64,iVBORw0K", // case-insensitive
		"data:image/png,rawbytes",        // non-base64 payload is legal
		"/photo.jpg",                     // ordinary resource URLs still pass
		"https://cdn.example.com/a.png",
	} {
		if !OK(u, ImageSource) {
			t.Errorf("OK(%q, ImageSource) = false, want true", u)
		}
	}
}

func TestImageSourceRejectsDangerousDataURLs(t *testing.T) {
	for _, u := range []string{
		"data:text/html;base64,PHNjcmlwdD4=",
		"data:text/html,<script>alert(1)</script>",
		// SVG in an <img> is script-disabled in browsers, but it is a
		// markup surface this package never needs to emit, so it stays out
		// of the allow-list rather than relying on that guarantee.
		"data:image/svg+xml;base64,PHN2Zz4=",
		"data:image/svg+xml,<svg onload=alert(1)>",
		"data:application/javascript,alert(1)",
		"data:,plain",
		"data:image",     // no comma, no payload
		"data:image/png", // media type but no payload delimiter
		"data:imagex/png;base64,AAAA",
		"javascript:alert(1)",
		"vbscript:msgbox",
		"data:image/png;base64,AA%0dAA", // CRLF smuggling
	} {
		if OK(u, ImageSource) {
			t.Errorf("OK(%q, ImageSource) = true, want false", u)
		}
	}
}

// The looser image policy must not leak into the policies that guard
// <script src> / <link href> / <a href>.
func TestDataURLsStillRejectedByOtherPolicies(t *testing.T) {
	for _, p := range []Policy{Anchor, Resource} {
		if OK("data:image/png;base64,iVBORw0K", p) {
			t.Errorf("policy %d must reject data: image URLs", p)
		}
	}
}
