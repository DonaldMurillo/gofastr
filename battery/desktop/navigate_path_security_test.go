package desktop

import (
	"io"
	"log/slog"
	"testing"
)

// PROPERTY
//
//	The navigate grammar refuses a dot segment in every spelling a URL
//	parser resolves as one: literal, percent-encoded in any case, and
//	before a query or fragment. A path segment that decodes to a slash,
//	a backslash or a control character is refused too: ServeMux decodes
//	a {name} wildcard, so "..%2f..%2fsecret" reaches the handler as
//	"../../secret". Every caller (menus, deep links, the window
//	capability, Settings, OpenWindow) shares it.
func TestNavigatePathRefusesDotSpellings(t *testing.T) {
	bad := []string{
		"/a/../b",
		"/a/./b",
		"/a/%2e%2e/b",
		"/a/%2E%2e/b",
		"/a/.%2e/b",
		"/a/%2e./b",
		"/a/%2e/b",
		"/a/..?x=1",
		"/a/%2e%2e?x=1",
		"/a/%2e%2e#top",
		"/a/..",
		// A dot segment after the query is not path, but the grammar has
		// always refused it, and it keeps doing so.
		"/a?next=/../b",
		// Encoded separators and control bytes inside one segment.
		"/files/..%2f..%2fsecret",
		"/files/..%2F..%2Fsecret",
		"/files/%2e%2e%2fsecret",
		"/files/%2E%2E%2Fsecret",
		"/files/..%5c..%5csecret",
		"/files/..%5C..%5Csecret",
		"/a/%2e%2e%2fb",
		"/files/a%2fb",
		"/files/a%5cb",
		"/files/name%00.txt",
		"/files/name%0a",
		"/files/name%1b",
		"/files/name%7f",
		"/files/name%c2%85", // U+0085, a C1 control
		"/files/%2e%2e%2fsecret?x=1",
	}
	for _, p := range bad {
		if validNavigatePath(p) {
			t.Errorf("validNavigatePath(%q) = true, want false", p)
		}
	}
	ok := []string{
		"/",
		"/a/b",
		"/a/%2e%2e%2e", // three dots are a name, not a dot segment
		"/a/b%20c",
		"/a?source=%2f", // the query is the page's to decode
		"/a/...",
		"/a/b?next=x.y",
		"/a/%3Fb?x=1",
		"/a/%zz", // not a valid escape, so not a dot either
	}
	for _, p := range ok {
		if !validNavigatePath(p) {
			t.Errorf("validNavigatePath(%q) = false, want true", p)
		}
	}
}

// TestDeepLinkRefusesEncodedSeparators: the default deep-link mapping
// keeps the path escaped, so an encoded separator or NUL reaches the
// grammar intact and the link is dropped instead of navigating.
func TestDeepLinkRefusesEncodedSeparators(t *testing.T) {
	b := &Battery{
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		cfg:    Config{DeepLink: &DeepLinkConfig{Scheme: "gofastr-notes"}},
	}
	for _, raw := range []string{
		"gofastr-notes://files/..%2f..%2fsecret",
		"gofastr-notes://files/%2e%2e%2fsecret",
		"gofastr-notes://files/..%5c..%5csecret",
		"gofastr-notes://files/name%00",
	} {
		if path, ok := b.mapDeepLink(raw); ok {
			t.Errorf("mapDeepLink(%q) = %q, want the link dropped", raw, path)
		}
	}
	if path, ok := b.mapDeepLink("gofastr-notes://files/report.txt"); !ok || path != "/files/report.txt" {
		t.Fatalf("mapDeepLink(plain file) = %q, %v", path, ok)
	}
}
