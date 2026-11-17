package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// Forced colours (Windows high contrast) drop background images and paint
// backgrounds Canvas, so a component that shows state or an icon only
// through a background vanishes (#702). Each such component carries a
// forced-colors block that redraws it in system colours.
func TestForcedColorsFallbacks(t *testing.T) {
	th := theme.Default()
	for _, c := range []struct {
		name  string
		style *registry.Style
		want  []string // each must appear inside the forced-colors block
	}{
		{"checkbox tick", toggleStyle, []string{".fui-choice--checkbox .fui-choice__input:checked", "HighlightText calc(50% - 1px)", "forced-color-adjust: none"}},
		{"radio dot", toggleStyle, []string{".fui-choice--radio .fui-choice__input:checked", "radial-gradient(circle at center, Highlight"}},
		{"switch thumb", toggleStyle, []string{".fui-switch__input:checked", "radial-gradient(circle, HighlightText"}},
		{"spinner", spinnerStyle, []string{".fui-spinner__ring", "border-top-color: Highlight", ".fui-spinner__dot"}},
		{"divider", dividerStyle, []string{".fui-divider__line", "background: CanvasText"}},
		{"selected tab", tabsStyle, []string{`.fui-tab[aria-selected="true"]`, "border-color:Highlight"}},
		{"tab nav current", tabNavStyle, []string{".fui-tab-nav__link { border-block-end-color: Canvas; }", `[aria-current="page"] { border-block-end-color: Highlight; }`}},
		{"segmented links current", segmentedLinksStyle, []string{`.fui-seglinks__item[aria-current="true"]`, "solid Highlight"}},
		{"segmented pill", segmentedStyle, []string{".fui-segmented__input:checked", "solid Highlight"}},
		{"menu caret", menuStyle, []string{".fui-menu__caret", "forced-color-adjust: none; background: currentColor"}},
		{"multiselect chevron", multiselectStyle, []string{".fui-multiselect__summary::after", "forced-color-adjust: none"}},
		{"sidebar chevron", sidebarStyle, []string{".fui-sidebar__group-toggle::after", "forced-color-adjust: none"}},
		{"sort icon", dataTableStyle, []string{".fui-data-table__sort::after", "forced-color-adjust: none"}},
		{"collapsible chevron", collapsibleStyle, []string{".fui-collapsible__summary::after", "forced-color-adjust:none"}},
	} {
		css := c.style.Entry().CSSFor(th)
		i := strings.Index(css, "@media (forced-colors: active)")
		if i < 0 {
			t.Errorf("%s: no forced-colors block", c.name)
			continue
		}
		block := css[i:]
		for _, w := range c.want {
			if !strings.Contains(block, w) {
				t.Errorf("%s: forced-colors block lacks %q", c.name, w)
			}
		}
	}
}
