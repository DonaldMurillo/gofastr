package appspec

import (
	"testing"
)

func TestAppSpecControlBlocksNeedLabels(t *testing.T) {
	cases := []AppSpecBlock{
		{Kind: "action_button"},
		{Kind: "text_field", Props: map[string]any{"label": "Email"}},
		{Kind: "checkbox", Props: map[string]any{"name": "x"}},
		{Kind: "select_field", Props: map[string]any{"label": "A", "name": "a"}},
		{Kind: "radio_group", Props: map[string]any{"name": "a", "options": []any{"x"}}},
		{Kind: "custom_form"},
		{Kind: "custom_form", Props: map[string]any{"action": "javascript:alert(1)"}},
		{Kind: "data_table", Props: map[string]any{"caption": "T"}},
		{Kind: "data_table", Props: map[string]any{"columns": []any{"A"}, "caption_hidden": true}},
		{Kind: "data_table", Props: map[string]any{"columns": []any{"A"}, "rows": []any{[]any{"1", "2"}}}},
		{Kind: "action_button", Props: map[string]any{"label": "x", "variant": "loud"}},
		{Kind: "action_button", Props: map[string]any{"label": "x", "type": "image"}},
		{Kind: "custom_form", Props: map[string]any{"action": "/s", "method": "put"}},
		{Kind: "select_field", Props: map[string]any{"label": "A", "name": "a", "options": []any{map[string]any{"label": "A", "value": 1}}}},
		{Kind: "select_field", Props: map[string]any{"label": "A", "name": "a", "options": []any{map[string]any{"label": "A", "selected": "yes"}}}},
		{Kind: "nav_links"},
		{Kind: "custom_form", Props: map[string]any{"action": "/a"}, Children: []AppSpecBlock{{Kind: "custom_form", Props: map[string]any{"action": "/b"}}}},
		{Type: "div", Children: []AppSpecBlock{{Kind: "custom_form", Props: map[string]any{"action": "/b"}}}},
		{Type: "div", Children: []AppSpecBlock{{Kind: "section", Children: []AppSpecBlock{{Kind: "card"}}}}},
		{Type: "link", Href: "javascript:alert(1)", Text: "x"},
		{Type: "link", Href: "//evil.example", Text: "x"},
	}
	for _, b := range cases {
		if err := validateAppSpecBlock("s", nil, b); err == nil {
			t.Errorf("%s %v validated", b.Kind, b.Props)
		}
	}
	ok := AppSpecBlock{Kind: "custom_form", Props: map[string]any{"action": "/s"}, Children: []AppSpecBlock{
		{Kind: "text_field", Props: map[string]any{"label": "Email", "name": "email"}},
		{Kind: "select_field", Props: map[string]any{"label": "A", "name": "a", "options": []any{"x", map[string]any{"label": "Y", "value": "y"}}}},
	}}
	if err := validateAppSpecBlock("s", nil, ok); err != nil {
		t.Errorf("valid form rejected: %v", err)
	}
	// section is a node kind too: the node renderer draws it.
	nodeSection := AppSpecBlock{Type: "div", Children: []AppSpecBlock{{Kind: "section", Children: []AppSpecBlock{{Type: "text", Text: "x"}}}}}
	if err := validateAppSpecBlock("s", nil, nodeSection); err != nil {
		t.Errorf("section under a div rejected: %v", err)
	}
}
