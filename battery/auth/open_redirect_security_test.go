package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// TestOpenRedirect_SafeRelativePath verifies that handler.IsSafeRelativePath
// rejects various open-redirect payloads.
func TestOpenRedirect_SafeRelativePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		safe  bool
		desc  string
	}{
		{"absolute_url", "https://evil.com", false, "absolute URL redirect"},
		{"protocol_relative", "//evil.com", false, "protocol-relative redirect"},
		{"javascript_uri", "javascript:alert(1)", false, "javascript: URI redirect"},
		{"backslash_bypass", "/\\evil.com", false, "backslash bypass for protocol-relative"},
		{"null_byte", "/\x00evil.com", false, "null byte injection"},
		{"crlf", "/\r\nLocation: evil.com", false, "CRLF header injection"},
		{"encoded_backslash", "/%5Cevil.com", false, "percent-encoded backslash"},
		{"encoded_slash", "/%2F%2Fevil.com", false, "percent-encoded double slash"},
		{"control_char", "/\x01admin", false, "control character in path"},
		{"valid_relative", "/dashboard", true, "legitimate relative path"},
		{"valid_with_query", "/dashboard?tab=settings", true, "relative path with query"},
		{"empty", "", false, "empty string"},
		{"no_leading_slash", "dashboard", false, "missing leading slash"},
		{"data_uri", "data:text/html,<script>alert(1)</script>", false, "data URI redirect"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := handler.IsSafeRelativePath(tc.input)
			if got != tc.safe {
				if tc.safe && !got {
					t.Errorf("handler.IsSafeRelativePath(%q) = false (want true). Legitimate path rejected.", tc.input)
				} else {
					t.Errorf("SECURITY: [open_redirect] handler.IsSafeRelativePath(%q) = true (want false). Attack: %s.", tc.input, tc.desc)
				}
			}
		})
	}
}

// TestOpenRedirect_SuccessRedirectValidatesNext verifies that
// successRedirect only honors safe ?next= values.
func TestOpenRedirect_SuccessRedirectValidatesNext(t *testing.T) {
	tests := []struct {
		name     string
		nextURL  string
		fallback string
		wantSafe bool
		desc     string
	}{
		{"evil_absolute", "https://evil.com", "/home", false, "absolute URL in next param"},
		{"protocol_relative", "//evil.com", "/home", false, "protocol-relative in next param"},
		{"valid_relative", "/dashboard", "/home", true, "legitimate relative redirect"},
		{"missing_next", "", "/home", true, "no next param uses fallback"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Build a request with the next param
			url := "/auth/login?next=" + tc.nextURL
			req := httptest.NewRequest(http.MethodPost, url, nil)

			got := successRedirect(httptest.NewRecorder(), req, tc.fallback)

			if tc.wantSafe {
				if got == tc.nextURL && !tc.wantSafe {
					t.Errorf("SECURITY: [open_redirect] successRedirect honored unsafe ?next=%q. Attack: %s.", tc.nextURL, tc.desc)
				}
			}
			// For unsafe inputs, result should be the fallback or a safe path
			if !tc.wantSafe && got == tc.nextURL {
				if !handler.IsSafeRelativePath(tc.nextURL) {
					t.Errorf("SECURITY: [open_redirect] successRedirect returned %q for unsafe input. Attack: %s.", got, tc.desc)
				}
			}
		})
	}
}

// TestOpenRedirect_SanitiseErr verifies that error messages embedded in
// redirect URLs are sanitised. Attack: CRLF injection via error message.
func TestOpenRedirect_SanitiseErr(t *testing.T) {
	tests := []struct {
		name  string
		input string
		desc  string
	}{
		{"crlf", "ok\r\nSet-Cookie: evil=true", "CRLF injection via error message"},
		{"html", "<script>alert(1)</script>", "HTML injection in error message"},
		{"url_special", "error&next=/evil#fragment", "URL special characters"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitiseErr(tc.input)
			if strings.Contains(got, "\r") || strings.Contains(got, "\n") {
				t.Errorf("SECURITY: [open_redirect] sanitiseErr(%q) = %q contains newline. Attack: %s.", tc.input, got, tc.desc)
			}
			if strings.Contains(got, "<script>") {
				t.Errorf("SECURITY: [open_redirect] sanitiseErr(%q) = %q contains HTML. Attack: %s.", tc.input, got, tc.desc)
			}
		})
	}
}

// The Referer's query is dropped, and an escaped ? or # in its path
// stays escaped: the decoded path turned /login%3Fnext=/admin into
// /login?next=/admin, a query the error redirect then extended.
func TestSafeRefererKeepsPathEscapes(t *testing.T) {
	for _, ref := range []string{
		"https://app.example.com/login%3Fnext=/admin?drop=1",
		"/login%3Fnext=/admin?drop=1",
	} {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.Header.Set("Referer", ref)
		req.Host = "app.example.com"
		if got := safeReferer(req); got != "/login%3Fnext=/admin" {
			t.Errorf("safeReferer(%q) = %q, want /login%%3Fnext=/admin", ref, got)
		}
	}
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("Referer", "https://app.example.com/a%23b")
	req.Host = "app.example.com"
	if got := safeReferer(req); got != "/a%23b" {
		t.Errorf("safeReferer kept %q, want /a%%23b", got)
	}
	// Escaped is not a way around the protocol-relative refusal: the
	// check reads the decoded path as well.
	for _, ref := range []string{"https://app.example.com/%2Fevil.com", "/%2Fevil.com", "/%5Cevil.com"} {
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.Header.Set("Referer", ref)
		req.Host = "app.example.com"
		if got := safeReferer(req); got != "" {
			t.Errorf("SECURITY: safeReferer(%q) = %q, want refused", ref, got)
		}
	}
}

// TestOpenRedirect_SafeRefererRejectsCrossOrigin verifies that
// safeReferer rejects cross-origin Referer headers.
func TestOpenRedirect_SafeRefererRejectsCrossOrigin(t *testing.T) {
	tests := []struct {
		name     string
		referer  string
		host     string
		wantSafe bool
		desc     string
	}{
		{"same_origin", "https://app.example.com/login", "app.example.com", true, "same-origin referer is kept"},
		{"cross_origin", "https://evil.com/login", "app.example.com", false, "cross-origin referer is rejected"},
		{"empty", "", "app.example.com", false, "empty referer returns empty"},
		{"different_port", "https://app.example.com:8080/login", "app.example.com", false, "different port is cross-origin"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
			req.Header.Set("Referer", tc.referer)
			req.Host = tc.host

			got := safeReferer(req)
			if tc.wantSafe && got == "" {
				t.Errorf("safeReferer rejected safe referer %q for host %q", tc.referer, tc.host)
			}
			if !tc.wantSafe && got != "" {
				t.Errorf("SECURITY: [open_redirect] safeReferer accepted cross-origin referer %q for host %q → %q. Attack: %s.", tc.referer, tc.host, got, tc.desc)
			}
		})
	}
}
