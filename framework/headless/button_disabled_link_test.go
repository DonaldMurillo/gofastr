package headless

import (
	"strings"
	"testing"
)

// Button with Href + Disabled dropped the href ("rather than leaving a
// live link that looks dead") but kept the Action's wiring keys. The
// runtime's data-cui-open delegate reads no aria-disabled, so the
// dead-looking link still opened its widget on click. A rejected href
// takes the same posture.
func TestDisabledLinkButtonCarriesNoWiring(t *testing.T) {
	for _, p := range []ButtonProps{
		{Label: "Delete", Href: "/x/delete", Disabled: true},
		{Label: "Delete", Href: "javascript:alert(1)"},
	} {
		p.Action = map[string]string{"data-cui-open": "confirm-delete"}
		h := string(Button(p, nil))
		if strings.Contains(h, "data-cui-open") || !strings.Contains(h, `aria-disabled="true"`) {
			t.Fatalf("a dead link button still carries its wiring: %s", h)
		}
	}
	live := string(Button(ButtonProps{Label: "Delete", Href: "/x/delete",
		Action: map[string]string{"data-cui-open": "confirm-delete"}}, nil))
	if !strings.Contains(live, `data-cui-open="confirm-delete"`) {
		t.Fatalf("a live link button lost its wiring: %s", live)
	}
}
