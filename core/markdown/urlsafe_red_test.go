//go:build red

// Property: markdown link/image URL guards must enforce the canonical
// urlsafe allow-list (core-ui/urlsafe) like every other URL sink, not a
// parallel deny-list (inline.go isDangerousURLScheme) that admits
// file:, ftp:, blob:, ws:, and protocol-relative references.
//
// Surfaces: core/markdown inline renderer — <a href> via safeLinkURL,
// <img src> via safeImageURL.
//
// Finding: [x](//evil.example), [x](file:///etc/passwd), [x](ftp://h/f)
// render href/src verbatim. urlsafe.OK(..., Anchor) and OK(...,
// ImageSource) reject protocol-relative and non-http(s) schemes. The
// deny-list drifts from the allow-list every other surface applies.
//
// Fix direction: route safeLinkURL through urlsafe.Clean(url,
// urlsafe.Anchor) and safeImageURL through urlsafe.Clean(url,
// urlsafe.ImageSource), keeping "#" as the rejection output.
// Severity: production-facing (user-authored markdown is rendered on
// public pages).

package markdown

import (
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

var redHrefRe = regexp.MustCompile(`<a href="([^"]*)"`)
var redSrcRe = regexp.MustCompile(`<img src="([^"]*)"`)

// firstHref extracts the href of the first <a> in rendered HTML.
func firstHref(t *testing.T, html string) string {
	t.Helper()
	m := redHrefRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no <a href> in rendered output: %q", html)
	}
	return m[1]
}

func firstSrc(t *testing.T, html string) string {
	t.Helper()
	m := redSrcRe.FindStringSubmatch(html)
	if m == nil {
		t.Fatalf("no <img src> in rendered output: %q", html)
	}
	return m[1]
}

// TestMarkdownRedAnchorSchemeAllowlist: every href emitted from a
// markdown link must satisfy urlsafe.Anchor.
func TestMarkdownRedAnchorSchemeAllowlist(t *testing.T) {
	for _, url := range []string{
		"//evil.example",
		"file:///etc/passwd",
		"ftp://evil.example/x",
		"blob:https://evil.example/1",
	} {
		html := string(RenderHTML("[x](" + url + ")"))
		href := firstHref(t, html)
		if href != "#" && !urlsafe.OK(href, urlsafe.Anchor) {
			t.Errorf("href %q (from link %q) fails the urlsafe Anchor policy; markdown uses a deny-list instead of the canonical allow-list", href, url)
		}
	}
}

// TestMarkdownRedImageSchemeAllowlist: every src emitted from a
// markdown image must satisfy urlsafe.ImageSource.
func TestMarkdownRedImageSchemeAllowlist(t *testing.T) {
	for _, url := range []string{
		"//evil.example/a.png",
		"file:///etc/passwd.png",
		"blob:https://evil.example/1",
	} {
		html := string(RenderHTML("![x](" + url + ")"))
		src := firstSrc(t, html)
		if src != "#" && !urlsafe.OK(src, urlsafe.ImageSource) {
			t.Errorf("src %q (from image %q) fails the urlsafe ImageSource policy; markdown uses a deny-list instead of the canonical allow-list", src, url)
		}
	}
}
