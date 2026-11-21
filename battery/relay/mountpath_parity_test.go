package relay

import (
	"fmt"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/mountpath"
)

// relay keeps its own path validator so it stays unmountable (a copy
// may not import gofastr internal/ packages); this pins it to
// internal/mountpath, the validator rtc uses.
func TestPathRulesMatchMountpath(t *testing.T) {
	paths := []string{
		"rel", "/", "/tail/", "/a\x00b", "/a b", "/live/{room}", "/a$b",
		"/p%20th", "/a#b", "/a?b", "/a\\b", "/a//b", "/a/b/..", "/__gofastr",
		"/__gofastr/sse", "/__gofastr/runtime.js/x", "/__gofastr/t", "/firstparty", "/a/b",
	}
	for _, p := range paths {
		want := ""
		if err := mountpath.Validate("Config.Path", p); err != nil {
			want = "relay: " + err.Error()
		}
		if got := panicText(func() { validatePath(p) }); got != want {
			t.Errorf("validatePath(%q) = %q, mountpath says %q", p, got, want)
		}
	}
	for _, prefix := range []string{"a{b", "../x", "a//b", "a%20", "a b", "e"} {
		want := ""
		if err := mountpath.ValidateSegments("route prefix", "/"+prefix); err != nil {
			want = "relay: " + err.Error()
		}
		got := ""
		if err := validateSegments("route prefix", "/"+prefix); err != nil {
			got = err.Error()
		}
		if got != want {
			t.Errorf("validateSegments(%q) = %q, mountpath says %q", prefix, got, want)
		}
	}
}

func panicText(fn func()) (msg string) {
	defer func() {
		if r := recover(); r != nil {
			msg = fmt.Sprint(r)
		}
	}()
	fn()
	return ""
}
