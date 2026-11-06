package appspec

import (
	"strings"
	"testing"
)

func TestLinkContentMatchesRenderer(t *testing.T) {
	for _, test := range []struct {
		name  string
		block AppSpecBlock
		valid bool
	}{
		{"href label", AppSpecBlock{Type: "link", Href: "/docs"}, true},
		{"styled href label", AppSpecBlock{Type: "link", Href: "/docs", Class: "docs"}, true},
		{"kind", AppSpecBlock{Kind: "link", Href: "/docs"}, false},
		{"anchor", AppSpecBlock{Kind: "a", Href: "/docs"}, false},
		{"props", AppSpecBlock{Type: "link", Props: map[string]any{"href": "/docs"}}, false},
		{"actions", AppSpecBlock{Type: "link", Href: "/docs", Actions: []AppSpecAction{{Name: "docs"}}}, false},
		{"island", AppSpecBlock{Type: "link", Href: "/docs", Island: "docs"}, false},
		{"widget", AppSpecBlock{Type: "link", Href: "/docs", Widget: "docs"}, false},
		{"node text", AppSpecBlock{Kind: "link", Href: "/docs", Text: "Docs"}, true},
		{"node children", AppSpecBlock{Kind: "a", Href: "/docs", Children: []AppSpecBlock{{Kind: "span", Text: "Docs"}}}, true},
		{"aria label", AppSpecBlock{Kind: "link", Href: "/docs", Props: map[string]any{"aria-label": "Open docs"}}, true},
		{"aria labelledby", AppSpecBlock{Kind: "a", Href: "/docs", Props: map[string]any{"aria-labelledby": "docs-title"}}, true},
		{"blank label with labelledby", AppSpecBlock{Kind: "a", Href: "/docs", Props: map[string]any{"aria-label": " \t", "aria-labelledby": "docs-title"}}, true},
		{"blank aria label", AppSpecBlock{Kind: "link", Href: "/docs", Props: map[string]any{"aria-label": " \t\n"}}, false},
		{"blank aria labelledby", AppSpecBlock{Kind: "a", Href: "/docs", Props: map[string]any{"aria-labelledby": " \t\n"}}, false},
		{"nonstring aria label", AppSpecBlock{Kind: "a", Href: "/docs", Props: map[string]any{"aria-label": false}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateAppSpecBlock("home", nil, test.block)
			if test.valid && err != nil {
				t.Fatalf("valid link rejected: %v", err)
			}
			if !test.valid && (err == nil || !strings.Contains(err.Error(), "text or children are required")) {
				t.Fatalf("empty node link error = %v", err)
			}
		})
	}
}

func TestBlocksNeedRequiredProps(t *testing.T) {
	for _, block := range []AppSpecBlock{
		{Kind: "label", Text: "Email"},
		{Kind: "fieldset"},
		{Kind: "img", Props: map[string]any{"alt": "chart"}},
		{Kind: "link", Href: "/target"},
	} {
		if err := validateAppSpecBlock("home", nil, block); err == nil {
			t.Errorf("%s without a required prop was accepted", block.Kind)
		}
	}
}
