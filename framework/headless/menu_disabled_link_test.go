package headless

import (
	"strings"
	"testing"
)

// A Disabled link row carries no href: aria-disabled and pointer-events
// only stop a mouse, and a keyboard Enter or an assistive-tech activation
// followed the live link. An enabled link row keeps its href.
func TestMenuDisabledHrefRowDoesNotNavigate(t *testing.T) {
	h := string(Menu(MenuProps{Label: "M", Items: []MenuItem{
		{Label: "Billing", Href: "/billing", Disabled: true},
		{Label: "Profile", Href: "/profile"},
	}}, nil))
	if strings.Contains(h, `href="/billing"`) {
		t.Fatalf("a Disabled menu row is still a live link: %s", h)
	}
	if !strings.Contains(h, `aria-disabled="true"`) {
		t.Fatalf("the disabled row lost aria-disabled: %s", h)
	}
	if !strings.Contains(h, `href="/profile"`) {
		t.Fatalf("an enabled link row lost its href: %s", h)
	}
}
