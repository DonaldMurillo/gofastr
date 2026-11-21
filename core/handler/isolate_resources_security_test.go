package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsCrossOriginSubresource(t *testing.T) {
	cases := []struct {
		name, method, site, mode, dest string
		want                           bool
	}{
		{"no fetch metadata (curl, old browser)", "GET", "", "", "", false},
		{"same-origin fetch", "GET", "same-origin", "cors", "empty", false},
		{"same-origin script", "GET", "same-origin", "no-cors", "script", false},
		{"typed address bar", "GET", "none", "navigate", "document", false},
		{"cross-site link click", "GET", "cross-site", "navigate", "document", false},
		{"cross-site HEAD navigation", "HEAD", "cross-site", "navigate", "document", false},
		{"cross-site iframe", "GET", "cross-site", "navigate", "iframe", false},
		{"cross-site script tag", "GET", "cross-site", "no-cors", "script", true},
		{"cross-site image", "GET", "cross-site", "no-cors", "image", true},
		{"cross-site fetch", "GET", "cross-site", "cors", "empty", true},
		{"cross-site form post", "POST", "cross-site", "navigate", "document", true},
		{"cross-site object navigation", "GET", "cross-site", "navigate", "object", true},
		{"cross-site embed navigation", "GET", "cross-site", "navigate", "embed", true},
		{"same-site sibling script", "GET", "same-site", "no-cors", "script", true},
		{"same-site sibling link click", "GET", "same-site", "navigate", "document", false},
		{"unknown site value", "GET", "weird", "no-cors", "script", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "/x", nil)
			for k, v := range map[string]string{"Sec-Fetch-Site": tc.site, "Sec-Fetch-Mode": tc.mode, "Sec-Fetch-Dest": tc.dest} {
				if v != "" {
					r.Header.Set(k, v)
				}
			}
			if got := IsCrossOriginSubresource(r); got != tc.want {
				t.Fatalf("IsCrossOriginSubresource = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsolateResources(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	allow := func(r *http.Request) bool { return r.URL.Path == "/public.js" }
	h := IsolateResources(allow)(ok)
	for _, tc := range []struct {
		name, path, site string
		want             int
	}{
		{"cross-site script refused", "/app.js", "cross-site", http.StatusForbidden},
		{"allowed path passes", "/public.js", "cross-site", http.StatusNoContent},
		{"same-origin passes", "/app.js", "same-origin", http.StatusNoContent},
		{"no metadata passes", "/app.js", "", http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", tc.path, nil)
			if tc.site != "" {
				r.Header.Set("Sec-Fetch-Site", tc.site)
				r.Header.Set("Sec-Fetch-Mode", "no-cors")
				r.Header.Set("Sec-Fetch-Dest", "script")
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d, want %d", w.Code, tc.want)
			}
		})
	}
	r := httptest.NewRequest("GET", "/app.js", nil)
	r.Header.Set("Sec-Fetch-Site", "cross-site")
	w := httptest.NewRecorder()
	IsolateResources(nil)(ok).ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("nil allow: status %d, want 403", w.Code)
	}
}

// The answer depends on the Fetch Metadata headers, so a shared cache must
// key on them: an allowed same-origin copy may not be served cross-site.
func TestIsolateResourcesVariesOnFetchMetadata(t *testing.T) {
	h := IsolateResources(nil)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Set("Cache-Control", "public, max-age=60")
	}))
	for _, site := range []string{"same-origin", "cross-site"} {
		r := httptest.NewRequest("GET", "/app.js", nil)
		r.Header.Set("Sec-Fetch-Site", site)
		r.Header.Set("Sec-Fetch-Mode", "no-cors")
		r.Header.Set("Sec-Fetch-Dest", "script")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		vary := strings.Join(w.Header().Values("Vary"), ",")
		for _, want := range []string{"Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest"} {
			if !strings.Contains(vary, want) {
				t.Errorf("%s answer (%d): Vary %q lacks %s", site, w.Code, vary, want)
			}
		}
		if site == "same-origin" && !strings.Contains(vary, "Accept-Encoding") {
			t.Errorf("the handler's own Vary was dropped: %q", vary)
		}
	}
}
