package gatepath

import (
	"net/http/httptest"
	"testing"
)

// Every path the router could serve as something other than what the
// gate matched must stay gated. Each case below is one way to dress a
// gated route up as an open one.
func TestOpenRefusesPathsTheRouterCouldResolveElsewhere(t *testing.T) {
	prefixes := []string{"/api"}
	gated := []string{
		"/apix",             // not on a segment boundary
		"/apix/things",      // same
		"/api/../admin",     // dot-dot climbs out after a router cleans it
		"/api/./../admin",   // same, with a dot segment first
		"/api//things",      // a doubled slash a router cleans away
		"/api%2F..%2Fadmin", // escaped slashes: RawPath differs
		"/api%2Fthings",     // an escaped slash a router may keep as one segment
		"/%61pi/things",     // an escaped letter: RawPath differs
		"/",                 // root
		"/admin/api",        // the prefix later in the path
		"/API/things",       // routes are case-sensitive; the match is too
	}
	for _, target := range gated {
		r := httptest.NewRequest("GET", target, nil)
		if Open(r, prefixes) {
			t.Errorf("Open(%q) = true; the path must stay behind the gate", target)
		}
	}
	open := []string{"/api", "/api/", "/api/things", "/api/things/7", "/api/caf%C3%A9"}
	for _, target := range open {
		r := httptest.NewRequest("GET", target, nil)
		if !Open(r, prefixes) {
			t.Errorf("Open(%q) = false; want the open prefix to match", target)
		}
	}
}

func TestOpenWithNoPrefixesIsAlwaysGated(t *testing.T) {
	if Open(httptest.NewRequest("GET", "/api", nil), nil) {
		t.Fatal("Open with no prefixes must be false")
	}
}

func TestValidateRefusesUnsafePrefixes(t *testing.T) {
	bad := []string{"", "/", "api", "/api/", "/api//x", "/api/../x", "/a/./b", "/__gofastr", "/__gofastr/sse", "/a b", "/ápi", "/api?x", "/api%2F"}
	for _, p := range bad {
		if err := Validate("test: OpenPrefixes", p); err == nil {
			t.Errorf("Validate(%q) = nil; want a refusal", p)
		}
	}
	for _, p := range []string{"/api", "/api/v1", "/hooks", "/v1.2", "/__gofastr_app"} {
		if err := Validate("test: OpenPrefixes", p); err != nil {
			t.Errorf("Validate(%q) = %v; want nil", p, err)
		}
	}
}
