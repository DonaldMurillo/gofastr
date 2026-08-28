package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestDividerPlainEmitsHR(t *testing.T) {
	h := Divider(DividerConfig{})
	mustContain(t, h, "<hr")
	mustContain(t, h, `data-fui-comp="ui-divider"`)
	if strings.Contains(string(h), `role="separator"`) {
		t.Fatalf("plain horizontal divider should not need role=separator (native <hr>):\n%s", h)
	}
}

func TestDividerLabelledRendersDivWithRole(t *testing.T) {
	h := Divider(DividerConfig{Label: "OR"})
	mustContain(t, h, `role="separator"`)
	mustContain(t, h, "OR")
	mustContain(t, h, "ui-divider--labelled")
	mustContain(t, h, "ui-divider__label")
}

func TestDividerVerticalAlwaysUsesRole(t *testing.T) {
	h := Divider(DividerConfig{Orientation: DividerVertical})
	mustContain(t, h, `role="separator"`)
	mustContain(t, h, `aria-orientation="vertical"`)
	mustContain(t, h, "ui-divider--vertical")
}

func TestDividerExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, h := range map[string]render.HTML{
		"hr":       Divider(DividerConfig{ExtraAttrs: extra}),
		"vertical": Divider(DividerConfig{Orientation: DividerVertical, ExtraAttrs: extra}),
		"labelled": Divider(DividerConfig{Label: "OR", ExtraAttrs: extra}),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
	}
}
