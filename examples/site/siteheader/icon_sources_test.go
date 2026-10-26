package siteheader

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
)

func TestHeaderTextAndIconSources(t *testing.T) {
	got := string(Render(Config{Version: "dev", Links: []Link{{Label: "Docs", Href: "/docs"}}}))
	for _, label := range []string{"λ", "Find docs, components, examples…", "⌘"} {
		if !strings.Contains(got, label) {
			t.Errorf("header lost UTF-8 text %q", label)
		}
	}
	for _, name := range []string{icons.Search, icons.Github, icons.Menu, icons.Close} {
		body, ok := icons.Lookup(name)
		if !ok || !strings.Contains(got, body) {
			t.Errorf("header does not render the vendored %s drawing", name)
		}
	}
}
