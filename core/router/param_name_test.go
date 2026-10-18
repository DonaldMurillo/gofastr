package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serveParam routes path through a router holding pattern and returns
// what read(r) produced, or the panic it raised.
func serveParam(t *testing.T, pattern, path string, read func(r *http.Request) string) (got string, panicked any) {
	t.Helper()
	r := New()
	r.Get(pattern, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer func() { panicked = recover() }()
		got = read(req)
	}))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	return got, panicked
}

// A misspelt name used to return "", the same as an empty value, so a
// handler looked up record "" instead of failing.
func TestParamPanicsOnUndeclaredName(t *testing.T) {
	_, p := serveParam(t, "/posts/{id}", "/posts/7", func(r *http.Request) string { return Param(r, "idd") })
	if p == nil {
		t.Fatal("Param(r, \"idd\") on /posts/{id} returned instead of panicking")
	}
	if msg, _ := p.(string); !strings.Contains(msg, "{idd}") || !strings.Contains(msg, "/posts/{id}") {
		t.Errorf("panic = %v, want it to name the wildcard and the route", p)
	}
}

func TestParamReadsDeclaredNames(t *testing.T) {
	for _, tc := range []struct{ pattern, path, name, want string }{
		{"/posts/{id}", "/posts/7", "id", "7"},
		{"/files/{rest...}", "/files/a/b", "rest", "a/b"},
		{"/orgs/{org}/posts/{id}", "/orgs/x/posts/7", "org", "x"},
	} {
		got, p := serveParam(t, tc.pattern, tc.path, func(r *http.Request) string { return Param(r, tc.name) })
		if p != nil || got != tc.want {
			t.Errorf("%s %s: Param(%q) = %q, panic %v; want %q", tc.pattern, tc.path, tc.name, got, p, tc.want)
		}
	}
}

// A request built by hand has no pattern to check against: tests that
// call a handler directly set path values with SetPathValue.
func TestParamWithoutPatternReadsValue(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/posts/7", nil)
	req.SetPathValue("id", "7")
	if got := Param(req, "id"); got != "7" {
		t.Fatalf("Param = %q, want 7", got)
	}
}
