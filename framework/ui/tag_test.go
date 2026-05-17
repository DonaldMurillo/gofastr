package ui

import (
	"strings"
	"testing"
)

func TestTagRequiresLabel(t *testing.T) {
	defer func() { recover() }()
	Tag(TagConfig{})
	t.Fatal("expected panic with empty Label")
}

func TestTagDefaultVariantIsNeutral(t *testing.T) {
	h := Tag(TagConfig{Label: "design"})
	mustContain(t, h, `data-fui-comp="ui-tag"`)
	mustContain(t, h, "ui-tag--neutral")
	mustContain(t, h, "design")
}

func TestTagVariantsEmitClasses(t *testing.T) {
	for _, v := range []StatusVariant{StatusSuccess, StatusWarning, StatusDanger, StatusInfo} {
		h := Tag(TagConfig{Label: "x", Variant: v})
		mustContain(t, h, "ui-tag--"+string(v))
	}
}

func TestTagUnknownVariantPanics(t *testing.T) {
	defer func() { recover() }()
	Tag(TagConfig{Label: "x", Variant: "purple"})
	t.Fatal("expected panic on unknown variant")
}

func TestTagInteractiveBecomesAnchor(t *testing.T) {
	h := Tag(TagConfig{Label: "design", Href: "/?tag=design"})
	mustContain(t, h, "ui-tag--interactive")
	mustContain(t, h, `href="/?tag=design"`)
}

func TestTagDismissAddsRPCButton(t *testing.T) {
	h := Tag(TagConfig{Label: "design", Dismiss: "/filters/remove"})
	mustContain(t, h, `data-fui-rpc="/filters/remove"`)
	mustContain(t, h, `aria-label="Remove design"`)
	mustContain(t, h, "ui-tag__dismiss")
}

func TestTagCustomDismissLabel(t *testing.T) {
	h := Tag(TagConfig{Label: "x", Dismiss: "/x", DismissLabel: "Clear filter"})
	mustContain(t, h, `aria-label="Clear filter"`)
}

func TestTagNoDismissOmitsButton(t *testing.T) {
	h := Tag(TagConfig{Label: "x"})
	if strings.Contains(string(h), "ui-tag__dismiss") {
		t.Fatalf("Tag without Dismiss should not render × button:\n%s", h)
	}
}
