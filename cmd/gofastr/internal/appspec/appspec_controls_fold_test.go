package appspec

import "testing"

// Control enum values fold A–Z only. Unicode folding (strings.EqualFold,
// strings.ToLower) maps U+017F LONG S onto s, so "ſecondary" validated as
// secondary and then rendered as whatever the generator's exact switch
// fell back to.
func TestControlEnumsFoldASCIIOnly(t *testing.T) {
	refused := []AppSpecBlock{
		{Kind: "action_button", Props: map[string]any{"label": "x", "variant": "ſecondary"}},
		{Kind: "action_button", Props: map[string]any{"label": "x", "type": "ſubmit"}},
	}
	for _, b := range refused {
		if err := validateAppSpecBlock("s", nil, b); err == nil {
			t.Errorf("%v validated", b.Props)
		}
	}
	accepted := AppSpecBlock{Kind: "action_button", Props: normalizeControlProps("action_button", map[string]any{"label": "x", "variant": "GHOST", "type": "Submit"})}
	if err := validateAppSpecBlock("s", nil, accepted); err != nil {
		t.Errorf("ASCII-cased enums refused: %v", err)
	}
	if got := accepted.Props["variant"]; got != "ghost" {
		t.Errorf("variant normalized to %q, want ghost", got)
	}
	if got := accepted.Props["type"]; got != "submit" {
		t.Errorf("type normalized to %q, want submit", got)
	}
}
