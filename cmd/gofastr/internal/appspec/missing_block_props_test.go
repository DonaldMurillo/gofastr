package appspec

import "testing"

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
