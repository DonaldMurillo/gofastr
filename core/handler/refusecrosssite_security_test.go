package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRefuseCrossSite(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := RefuseCrossSite(ok)
	cases := []struct {
		name        string
		method, ct  string
		hdr         map[string]string
		wantRefused bool
	}{
		{"cross-site form POST", "POST", "application/x-www-form-urlencoded", map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		{"cross-site multipart via Origin", "POST", "multipart/form-data; boundary=x", map[string]string{"Origin": "https://evil.example"}, true},
		{"cross-site text/plain DELETE", "DELETE", "text/plain", map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		{"cross-site bodyless PUT", "PUT", "", map[string]string{"Sec-Fetch-Site": "cross-site"}, true},
		{"same-origin form POST", "POST", "application/x-www-form-urlencoded", map[string]string{"Sec-Fetch-Site": "same-origin"}, false},
		{"curl POST (no browser headers)", "POST", "application/x-www-form-urlencoded", nil, false},
		{"cross-site JSON POST (preflighted)", "POST", "application/json", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"cross-site GET", "GET", "", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"cross-site CORS preflight", "OPTIONS", "", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://spa.example", "Access-Control-Request-Method": "POST"}, false},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(tc.method, "http://app.example/form", strings.NewReader("a=1"))
		r.Host = "app.example"
		if tc.ct != "" {
			r.Header.Set("Content-Type", tc.ct)
		}
		for k, v := range tc.hdr {
			r.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if refused := rec.Code == http.StatusForbidden; refused != tc.wantRefused {
			t.Errorf("%s: status %d; refused=%v, want %v", tc.name, rec.Code, refused, tc.wantRefused)
		}
	}
}
