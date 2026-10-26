package gallery

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
)

func TestIconDemoShowsCopyableCatalog(t *testing.T) {
	entry, ok := Lookup("icon")
	if !ok {
		t.Fatal("icon demo missing from catalog")
	}
	got := string(entry.Demo())
	for _, name := range icons.Names() {
		if !strings.Contains(got, `data-icon-name="`+name+`"`) {
			t.Errorf("icon %q is absent from the preview", name)
		}
		if !strings.Contains(got, `aria-label="Copy `+name+` icon name"`) || !strings.Contains(got, `data-hui-copy-target="icon-name-`+name+`"`) {
			t.Errorf("icon %q is not a named copy control", name)
		}
	}
	if n := strings.Count(got, `width="24"`); n < len(icons.Names()) {
		t.Errorf("only %d full-size drawings", n)
	}
	if strings.Contains(got, `<style`) || strings.Contains(got, ` style=`) || strings.Contains(got, `<script`) {
		t.Fatal("icon demo requires inline styling or scripts")
	}
	if CodeSnippet("icon") == "" {
		t.Fatal("icon demo has no usage example")
	}
	if !strings.Contains(got, "Sources: ") || !strings.Contains(got, "Feather, commit 3dc050d") || !strings.Contains(got, "Lucide, commit 7faf876") || !strings.Contains(got, "Retrieved October 24 and 25, 2026") {
		t.Fatal("icon demo lost its upstream attribution or retrieval date")
	}
}

func TestIconCatalogSearchAndCategory(t *testing.T) {
	for _, tc := range []struct {
		name, query, category string
		want, absent          []string
	}{
		{"alias", "PENCIL", "", []string{"edit"}, []string{"edit-3", "search"}},
		{"words", "arrow_down", "arrows", []string{"arrow-down", "arrow-down-circle"}, []string{"arrow-up", "chevron-down"}},
		{"category", "", "security", []string{"shield", "key", "lock"}, []string{"search", "folder"}},
		{"combined", "folder", "security", nil, []string{"folder", "shield"}},
		{"unknown category", "folder", "not-a-category", []string{"folder"}, []string{"shield"}},
		{"category words", "security", "", []string{"key", "shield"}, []string{"search"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := string(RenderIconCatalog(IconCatalogConfig{Action: "/components/icon", Search: tc.query, Category: tc.category}))
			for _, name := range tc.want {
				if !strings.Contains(got, `data-icon-name="`+name+`"`) {
					t.Errorf("missing %q", name)
				}
			}
			for _, name := range tc.absent {
				if strings.Contains(got, `data-icon-name="`+name+`"`) {
					t.Errorf("unexpected %q", name)
				}
			}
			if !strings.Contains(got, `id="icon-catalog-header"`) || !strings.Contains(got, `method="GET"`) {
				t.Error("missing sticky header or GET form")
			}
			if tc.name == "combined" && !strings.Contains(got, "No icons found") {
				t.Error("missing empty state")
			}
		})
	}
}
