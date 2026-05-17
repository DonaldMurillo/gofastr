package ui

import (
	"strings"
	"testing"
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
