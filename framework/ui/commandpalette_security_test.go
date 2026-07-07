package ui

import (
	"strings"
	"testing"
)

// A palette command's Href flows into the combobox option's
// data-fui-push-state, which the combobox runtime hands to the SPA
// navigator (falling back to location.href). An unsanitized
// javascript: URL there is DOM XSS the moment the option is picked.
func TestPaletteHrefDropsUnsafeSchemes(t *testing.T) {
	slot := &commandPaletteSlot{
		widgetName: "cmdk",
		options: paletteCommandsToOptions([]PaletteCommand{
			{Label: "Evil", Href: "javascript:alert(1)"},
			{Label: "Docs", Href: "/docs"},
		}),
	}
	h := strings.ToLower(string(slot.Render()))
	if strings.Contains(h, "javascript:") {
		t.Errorf("javascript: Href leaked into the palette markup:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-push-state="/docs"`) {
		t.Errorf("safe Href must survive:\n%s", h)
	}
}
