package ui_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Property: caller ExtraAttrs can never override the attributes a
// widget OWNS — not with the exact key, not with a case-variant key
// (HTML attribute names fold case-insensitively, and a folded duplicate
// would silently become the live attribute), and no on* handler can
// ride along. html.SafeExtraAttrs enforces this (EqualFold drops +
// data-fui-*/class/id prefixes + render.Attr's on* key rejection); this
// test pins that every widget in the family actually routes its extras
// through it and re-asserts its owned keys.
//
// Attack shapes per surface: exact owned key, case-variant owned key,
// and an on* handler.
func TestOwnedAttrsWinOverExtraAttrsFold(t *testing.T) {
	type surface struct {
		name     string
		render   func(extras map[string]string) string
		ownedKey string
		ownedVal string // exact owned rendering, key="value"
	}
	surfaces := []surface{
		{"tabs-data-active", func(ex map[string]string) string {
			return string(ui.Tabs(ui.TabsConfig{SignalName: "t", ExtraAttrs: ex,
				Tabs: []ui.TabItem{{Label: "A", Content: render.Text("a")}}}))
		}, `data-active`, `data-active="0"`},
		{"toc-target", func(ex map[string]string) string {
			return string(ui.TableOfContents(ui.TOCConfig{Target: "main", ExtraAttrs: ex}))
		}, `data-fui-toc`, `data-fui-toc="main"`},
		{"carousel-arialabel", func(ex map[string]string) string {
			return string(ui.Carousel(ui.CarouselConfig{Label: "Slideshow", ExtraAttrs: ex,
				Slides: []ui.CarouselSlide{{Content: render.Text("s")}}}))
		}, `aria-label`, `aria-label="Slideshow"`},
		{"polling-role", func(ex map[string]string) string {
			return string(ui.PollingIndicator(ui.PollingIndicatorConfig{ExtraAttrs: ex}))
		}, `role`, `role="status"`},
		{"formrepeater-arialive", func(ex map[string]string) string {
			return string(ui.FormRepeater(ui.FormRepeaterConfig{Name: "rows", ExtraAttrs: ex}))
		}, `aria-live`, `aria-live="polite"`},
		{"stepwizard-action", func(ex map[string]string) string {
			return string(ui.StepWizard(ui.StepWizardConfig{Action: "/wiz", ExtraAttrs: ex,
				Steps: []ui.StepWizardStep{{Heading: "H"}}}))
		}, `action`, `action="/wiz"`},
		{"select-name", func(ex map[string]string) string {
			return string(ui.Select(ui.SelectConfig{Name: "country", Label: "Country", ExtraAttrs: ex,
				Options: []ui.SelectOption{{Value: "se", Text: "SE"}}}))
		}, `name`, `name="country"`},
		{"numberinput-value", func(ex map[string]string) string {
			return string(ui.NumberInput(ui.NumberInputConfig{Name: "qty", Label: "Qty", Value: 7, ExtraAttrs: ex}))
		}, `value`, `value="7"`},
		{"taginput-wiring", func(ex map[string]string) string {
			return string(ui.TagInput(ui.TagInputConfig{Name: "tags", Label: "Tags", ExtraAttrs: ex}))
		}, `data-fui-tag-input`, `data-fui-tag-input="tags"`},
		{"panehost-marker", func(ex map[string]string) string {
			return string(ui.PaneHost(ui.PaneHostConfig{Primary: render.Text("p"), ExtraAttrs: ex}))
		}, `data-fui-pane-host`, `data-fui-pane-host=""`},
	}

	for _, s := range surfaces {
		t.Run(s.name, func(t *testing.T) {
			// Fold the attack into every casing of the owned key plus an
			// event handler; the smuggled value must never win.
			upper := strings.ToUpper(s.ownedKey)
			extras := map[string]string{
				s.ownedKey: "SMUGGLED",
				upper:      "SMUGGLED-UPPER",
				strings.ToUpper(s.ownedKey[:1]) + s.ownedKey[1:]: "SMUGGLED-CAP",
				"onclick": "alert(1)",
			}
			h := s.render(extras)

			if strings.Contains(h, "SMUGGLED") {
				t.Errorf("SECURITY: %s: ExtraAttrs overrode the owned %q attribute (check case-variant folding):\n%s", s.name, s.ownedKey, h)
			}
			if strings.Contains(h, `onclick=`) || strings.Contains(h, `ONCLICK=`) {
				t.Errorf("SECURITY: %s: an on* handler rode in through ExtraAttrs:\n%s", s.name, h)
			}
			if !strings.Contains(h, s.ownedVal) {
				t.Errorf("%s: owned attribute %q missing from output:\n%s", s.name, s.ownedVal, h)
			}
		})
	}
}
