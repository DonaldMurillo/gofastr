package auth

import (
	"os"
	"strings"
	"testing"
)

//gofastr:test-reads /framework/docs/content/auth.md
func TestBFFDefaultPrefixDocs(t *testing.T) {
	source, err := os.ReadFile("../../framework/docs/content/auth.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, found := strings.Cut(string(source), "`AllowedOrigins` requires exact")
	section, _, _ = strings.Cut(section, "`framework/ui.SignOut`")
	if !found || strings.Contains(section, "(default `/api`)") || !strings.Contains(section, "whole app") || !strings.Contains(section, "own origin") {
		t.Fatalf("BFF guide must describe the bare mount's origin guard: %s", section)
	}
}
