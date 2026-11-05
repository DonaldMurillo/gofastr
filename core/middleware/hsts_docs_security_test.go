package middleware

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSecureDocumentsHSTSOverride(t *testing.T) {
	source, err := os.ReadFile("security.go")
	if err != nil {
		t.Fatal(err)
	}
	_, comment, found := strings.Cut(string(source), "// Secure ")
	comment, _, _ = strings.Cut(comment, "Secure bool")
	if !found || !strings.Contains(comment, "auto-detect") || !strings.Contains(comment, "HSTSMaxAge: -1") {
		t.Fatalf("Secure must document auto-detection and the real off switch: %s", comment)
	}
	for _, test := range []struct {
		name      string
		config    SecurityHeadersConfig
		tls       bool
		forwarded string
		want      bool
	}{
		{name: "plain"},
		{name: "tls", tls: true, want: true},
		{name: "proxy", forwarded: "https", want: true},
		{name: "forced", config: SecurityHeadersConfig{Secure: true}, want: true},
		{name: "disabled tls", config: SecurityHeadersConfig{HSTSMaxAge: -1}, tls: true},
		{name: "disabled forced", config: SecurityHeadersConfig{Secure: true, HSTSMaxAge: -1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://app.test/", nil)
			if test.tls {
				request.TLS = &tls.ConnectionState{}
			}
			request.Header.Set("X-Forwarded-Proto", test.forwarded)
			recorder := httptest.NewRecorder()
			SecurityHeaders(test.config)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(recorder, request)
			if got := recorder.Header().Get("Strict-Transport-Security"); (got != "") != test.want {
				t.Errorf("HSTS = %q, want present=%v", got, test.want)
			}
		})
	}
}
