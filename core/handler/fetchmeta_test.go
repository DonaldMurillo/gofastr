package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIsCrossSiteRequest(t *testing.T) {
	cases := []struct {
		name   string
		sfs    string
		origin string
		want   bool
	}{
		{"same-origin fetch metadata", "same-origin", "https://evil.example", false},
		{"none is a navigation", "none", "", false},
		{"cross-site refused", "cross-site", "https://app.example", true},
		{"same-site sibling subdomain falls through to Origin", "same-site", "https://evil.app.example", true},
		{"same-site sibling port falls through to Origin", "same-site", "http://localhost:3000", true},
		{"same-site with matching Origin", "same-site", "https://app.example", false},
		{"unknown value falls through", "weird", "https://evil.example", true},
		{"no metadata, foreign Origin", "", "https://evil.example", true},
		{"no metadata, matching Origin case-folded", "", "https://APP.example", false},
		{"no metadata, null Origin", "", "null", false},
		{"no metadata, no Origin (curl)", "", "", false},
		{"no metadata, unparseable Origin", "", "://", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/", nil)
			r.Host = "app.example"
			if tc.sfs != "" {
				r.Header.Set("Sec-Fetch-Site", tc.sfs)
			}
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if got := IsCrossSiteRequest(r); got != tc.want {
				t.Fatalf("IsCrossSiteRequest = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestIsForgeableRequest(t *testing.T) {
	cases := map[string]bool{
		"":                                  true,
		"application/x-www-form-urlencoded": true,
		"multipart/form-data; boundary=x":   true,
		"Text/Plain; charset=utf-8":         true,
		"application/json":                  false,
		"application/xml":                   false,
	}
	for ct, want := range cases {
		r := httptest.NewRequest("POST", "/", nil)
		if ct != "" {
			r.Header.Set("Content-Type", ct)
		}
		if got := IsForgeableRequest(r); got != want {
			t.Fatalf("IsForgeableRequest(%q) = %v, want %v", ct, got, want)
		}
	}
}

func TestIsCrossSiteRequestStrictRefusesBareNullOrigin(t *testing.T) {
	mk := func(sfs, origin string) *http.Request {
		r := httptest.NewRequest("POST", "/", nil)
		r.Host = "app.example"
		if sfs != "" {
			r.Header.Set("Sec-Fetch-Site", sfs)
		}
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		return r
	}
	if !IsCrossSiteRequestStrict(mk("", "null")) {
		t.Fatal("bare Origin: null with no Fetch Metadata must be cross-site on a fetch-only surface")
	}
	if IsCrossSiteRequestStrict(mk("same-origin", "null")) {
		t.Fatal("Sec-Fetch-Site: same-origin vouches for a null Origin")
	}
	if IsCrossSiteRequestStrict(mk("", "")) {
		t.Fatal("no Origin at all (curl) is not a browser request")
	}
	if !IsCrossSiteRequestStrict(mk("", "https://evil.example")) {
		t.Fatal("foreign Origin stays cross-site")
	}
}
