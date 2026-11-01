package siteheader

import (
	"strings"
	"testing"

	brandassets "github.com/DonaldMurillo/gofastr/assets/brand"
	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
)

func TestHeaderTextAndIconSources(t *testing.T) {
	got := string(Render(Config{Version: "dev", Links: []Link{{Label: "Docs", Href: "/docs"}}}))
	for _, label := range []string{"Find docs, components, examples…", "⌘"} {
		if !strings.Contains(got, label) {
			t.Errorf("header lost UTF-8 text %q", label)
		}
	}
	if !strings.Contains(got, brandassets.SVG()) || strings.Contains(got, "λ") {
		t.Error("header must use the approved logo, not the retired lambda mark")
	}
	for _, name := range []string{icons.Search, icons.Github, icons.Menu, icons.Close} {
		body, ok := icons.Lookup(name)
		if !ok || !strings.Contains(got, body) {
			t.Errorf("header does not render the vendored %s drawing", name)
		}
	}
}
