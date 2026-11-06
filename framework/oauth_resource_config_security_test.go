package framework

import (
	"strings"
	"testing"
)

func TestOAuthMetadataRefusesInvalidURLs(t *testing.T) {
	for _, resource := range []string{"", "not-a-url", "/api", "http://api.example", "https:///api", "https://api.example/#fragment", "https://api.example/#", "https://user@api.example"} {
		t.Run("resource/"+resource, func(t *testing.T) {
			app := NewApp(WithOAuthProtectedResource(OAuthProtectedResourceConfig{Resource: resource}))
			if err := app.Start("not-an-address"); err == nil || !strings.Contains(err.Error(), "OAuth protected resource") || !strings.Contains(err.Error(), "Resource") {
				t.Fatalf("invalid Resource %q reached boot: %v", resource, err)
			}
		})
	}
	for _, issuer := range []string{"", "issuer", "http://auth.example", "https:///issuer", "https://auth.example/#fragment", "https://auth.example/?query", "https://auth.example/?", "https://user@auth.example"} {
		t.Run("issuer/"+issuer, func(t *testing.T) {
			app := NewApp(WithOAuthProtectedResource(OAuthProtectedResourceConfig{Resource: "https://api.example/v1?audience=app", AuthorizationServers: []string{issuer}}))
			if err := app.Start("not-an-address"); err == nil || !strings.Contains(err.Error(), "AuthorizationServers[0]") {
				t.Fatalf("invalid issuer %q reached boot: %v", issuer, err)
			}
		})
	}
}

func TestOAuthMetadataAllowsResourceQuery(t *testing.T) {
	app := NewApp(WithOAuthProtectedResource(OAuthProtectedResourceConfig{
		Resource: "https://api.example/v1?audience=app", AuthorizationServers: []string{"https://auth.example/issuer"},
	}))
	if err := app.Start("not-an-address"); err == nil || strings.Contains(err.Error(), "OAuth protected resource") {
		t.Fatalf("valid metadata did not reach address validation: %v", err)
	}
}
