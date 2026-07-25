package static

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSSGParamNoTraversal asserts that StaticPaths param values containing
// path separators or ".." can never produce a build-output file outside
// OutDir. The expansion must reject the value rather than substitute it raw.
func TestSSGParamNoTraversal(t *testing.T) {
	ctx := context.Background()
	pattern := "/products/:slug"

	cases := []struct {
		name string
		slug string
	}{
		{"happy", "alpha"},                     // legitimate slug must still expand
		{"dotdot", "../../../etc/cron.d/evil"}, // classic traversal
		{"embedded_slash", "a/b/c"},            // single separator escapes the leaf dir
		{"absolute", "/etc/passwd"},            // absolute-looking value
		{"trailing_dotdot", "x/.."},            // collapses back up a level
	}

	out := t.TempDir()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			urls, err := expandRouteWith(ctx, pattern, map[string]string{"slug": tc.slug})
			if tc.name == "happy" {
				if err != nil {
					t.Fatalf("legitimate slug rejected: %v", err)
				}
				if len(urls) != 1 || urls[0] != "/products/alpha" {
					t.Fatalf("happy path: got %v", urls)
				}
				return
			}
			// Attack shapes: either the expansion errors out, or whatever URL
			// it produces must map to a file CONTAINED within OutDir.
			if err != nil {
				return // rejected — the desired fail-closed outcome
			}
			for _, u := range urls {
				dst := filepath.Join(out, pathToFile(u))
				rel, rerr := filepath.Rel(out, dst)
				if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
					t.Fatalf("slug %q escapes OutDir: dst=%q rel=%q", tc.slug, dst, rel)
				}
			}
		})
	}
}

// expandRouteWith drives expandRoute with a fixed StaticPaths map by invoking
// the param-substitution path directly through a test provider.
func expandRouteWith(ctx context.Context, pattern string, params map[string]string) ([]string, error) {
	return expandParams(ctx, pattern, []map[string]string{params})
}

// TestStaticExportShipsCSP pins that a static export carries the same
// Content-Security-Policy a served page gets.
//
// The whole browser-gadget set the audit found — the data-behavior
// script sink, the runtime's fetch URLs, signal-driven attributes — is
// mitigated in production by `default-src 'self'`, which
// core/middleware.SecurityHeaders sets as a RESPONSE HEADER. A static
// export is a directory of files: on S3, Netlify or GitHub Pages nobody
// sets that header, so every one of those gadgets loses its sole
// mitigation on exactly the deployment target that has no server to
// re-add it.
//
// Two carriers, because the hosts differ: an in-document
// <meta http-equiv> works everywhere, and a `_headers` file is what
// Netlify/Cloudflare Pages read to set the real header.
func TestStaticExportShipsCSP(t *testing.T) {
	if staticExportCSP == "" {
		t.Fatal("staticExportCSP is empty — a static export would ship no policy at all")
	}
	for _, want := range []string{"default-src 'self'", "object-src 'none'", "frame-ancestors 'none'"} {
		if !strings.Contains(staticExportCSP, want) {
			t.Errorf("SECURITY: [xss] the static-export CSP omits %q: %q", want, staticExportCSP)
		}
	}

	page := applyStaticCSP("<!doctype html>\n<html>\n<head>\n<title>x</title>\n</head>\n<body>hi</body>\n</html>")
	if !strings.Contains(page, `http-equiv="Content-Security-Policy"`) {
		t.Fatalf("SECURITY: [xss] exported pages carry no in-document CSP:\n%s", page)
	}
	// The meta has to precede anything that can trigger a fetch.
	metaIdx := strings.Index(page, "Content-Security-Policy")
	for _, tag := range []string{"<script", "<link", "<img", "<body"} {
		if i := strings.Index(page, tag); i >= 0 && i < metaIdx {
			t.Errorf("SECURITY: [xss] %s precedes the CSP meta — the policy installs too late", tag)
		}
	}

	// Netlify / Cloudflare Pages read _headers; writing it turns the
	// meta into a real header on those hosts.
	dir := t.TempDir()
	if err := writeHeadersFile(dir); err != nil {
		t.Fatalf("writeHeadersFile: %v", err)
	}
	buf, err := os.ReadFile(filepath.Join(dir, "_headers"))
	if err != nil {
		t.Fatalf("read _headers: %v", err)
	}
	if !strings.Contains(string(buf), "Content-Security-Policy") || !strings.Contains(string(buf), "/*") {
		t.Errorf("_headers does not apply a CSP to every path:\n%s", buf)
	}
}
