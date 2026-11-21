package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func cspOf(cfg SecurityHeadersConfig) string {
	w := httptest.NewRecorder()
	SecurityHeaders(cfg)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	return w.Header().Get("Content-Security-Policy")
}

func TestDefaultCSPEnforcesTrustedTypes(t *testing.T) {
	if csp := cspOf(SecurityHeadersConfig{}); !strings.HasSuffix(csp, "; require-trusted-types-for 'script'; trusted-types "+TrustedTypesPolicy) {
		t.Fatalf("default CSP does not end in the Trusted Types directives: %q", csp)
	}
	if csp := cspOf(SecurityHeadersConfig{TrustedTypesPolicies: []string{"charts", "default", "charts", TrustedTypesPolicy}}); !strings.HasSuffix(csp, "trusted-types gofastr charts default") {
		t.Fatalf("extra policy names not listed once each after gofastr: %q", csp)
	}
	if csp := cspOf(SecurityHeadersConfig{NoTrustedTypes: true}); strings.Contains(csp, "trusted-types") {
		t.Fatalf("NoTrustedTypes kept a Trusted Types directive: %q", csp)
	}
	own := "default-src 'self'"
	if csp := cspOf(SecurityHeadersConfig{ContentSecurityPolicy: own, TrustedTypesPolicies: []string{"x"}}); csp != own {
		t.Fatalf("a host policy was rewritten: %q", csp)
	}
}

func TestTrustedTypesPolicyNameIsChecked(t *testing.T) {
	for _, bad := range []string{"", "a b", "x;script-src *", "'none'", "*", "a,b"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("policy name %q was accepted", bad)
				}
			}()
			SecurityHeaders(SecurityHeadersConfig{TrustedTypesPolicies: []string{bad}})
		}()
	}
	if got := TrustedTypesDirectives("a-b_c/d@e.f%g#h=i"); !strings.HasSuffix(got, "trusted-types gofastr a-b_c/d@e.f%g#h=i") {
		t.Fatalf("a name the grammar allows was not listed: %q", got)
	}
}
