package main

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

//gofastr:test-reads examples/real-estate/gofastr.yml
func TestStaffScreensRequireLogin(t *testing.T) {
	if testing.Short() {
		t.Skip("generates, builds, and boots an app")
	}
	clitest.VariantBuild(t, "examples/real-estate/gofastr.yml")
	binary, directory := exampleTree(t, "../../examples/real-estate/gofastr.yml", "real-estate")
	base, _, _ := bootGeneratedApp(t, "real-estate", binary, directory)
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	for _, path := range []string{"/agents", "/tours", "/inquiries", "/properties"} {
		response, err := client.Get(base + path)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if path == "/properties" {
			if response.StatusCode != http.StatusOK {
				t.Errorf("public catalog = %d; want 200", response.StatusCode)
			}
			continue
		}
		location, err := response.Location()
		if err != nil || location.Path != "/login" || response.StatusCode != http.StatusFound && response.StatusCode != http.StatusSeeOther {
			t.Errorf("anonymous %s = %d, Location=%q, %v; want a login redirect", path, response.StatusCode, response.Header.Get("Location"), err)
		}
		assertScreenServesNoRows(t, "real-estate", base, path[1:])
	}
}
