package main

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/docs"
	"github.com/DonaldMurillo/gofastr/framework/gallery"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func TestSidebarDemoLinksResolve(t *testing.T) {
	var checkItems func([]ui.SidebarItem)
	checkItems = func(items []ui.SidebarItem) {
		for _, item := range items {
			if len(item.Children) > 0 {
				checkItems(item.Children)
				continue
			}
			t.Run(item.Label, func(t *testing.T) {
				u, err := url.Parse(item.Href)
				if err != nil || u.IsAbs() || !strings.HasPrefix(u.Path, "/docs/") {
					t.Fatalf("invalid local docs link %q: %v", item.Href, err)
				}
				slug := strings.TrimPrefix(u.Path, "/docs/")
				if _, _, ok := findDocEntry(slug); !ok {
					t.Fatalf("sidebar link %q does not resolve in the docs page registry", item.Href)
				}
				body, err := docs.Get(slug)
				if err != nil {
					t.Fatalf("sidebar link %q has no embedded doc: %v", item.Href, err)
				}
				if u.Fragment != "" && !strings.Contains(string(ui.Markdown(ui.MarkdownConfig{Source: string(body)})), `id="`+u.Fragment+`"`) {
					t.Errorf("sidebar link %q has no rendered heading anchor", item.Href)
				}
			})
		}
	}
	checkItems(gallery.SidebarShowcaseConfig.Items)
}
