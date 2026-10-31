package desktop

import "testing"

// PROPERTY
//
//	The navigate grammar refuses a dot segment in every spelling a URL
//	parser resolves as one: literal, percent-encoded in any case, and
//	before a query or fragment. Every caller (menus, deep links, the
//	window capability, Settings, OpenWindow) shares it.
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
	}
	for _, p := range bad {
		if validNavigatePath(p) {
			t.Errorf("validNavigatePath(%q) = true, want false", p)
		}
	}
	ok := []string{
		"/",
		"/a/b",
		"/a/%2e%2e%2fb", // an encoded slash keeps it one segment
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
