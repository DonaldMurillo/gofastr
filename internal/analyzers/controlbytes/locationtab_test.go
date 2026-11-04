package controlbytes_test

import (
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	fixture "github.com/DonaldMurillo/gofastr/internal/analyzers/controlbytes/testdata/src/locationtab"
)

func TestRefuseNewlineRedirectStripsControls(t *testing.T) {
	var controls strings.Builder
	for value := byte(0); value < 0x20; value++ {
		if value != '\n' {
			controls.WriteByte(value)
		}
	}
	controls.WriteByte(0x7f)
	request := httptest.NewRequest("GET", "http://example.test/", nil)
	request.URL = &url.URL{Path: "/safe" + controls.String() + "/path"}
	response := httptest.NewRecorder()
	fixture.RefuseNewlineRedirect(response, request)
	if target := response.Header().Get("Location"); target != "/safe/path" {
		t.Fatalf("Location retains controls: %q", target)
	}
}
