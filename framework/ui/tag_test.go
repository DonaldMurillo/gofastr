package ui

import (
	"github.com/DonaldMurillo/gofastr/framework/headless"

	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestTagRequiresLabel(t *testing.T) {
	defer func() { recover() }()
	Tag(TagConfig{})
	t.Fatal("expected panic with empty Label")
}

func TestTagDefaultVariantIsNeutral(t *testing.T) {
	h := Tag(TagConfig{Label: "design"})
	mustContain(t, h, `data-fui-comp="ui-tag"`)
	mustContain(t, h, "fui-tag--neutral")
	mustContain(t, h, "design")
}

func TestTagVariantsEmitClasses(t *testing.T) {
	for _, v := range []StatusVariant{StatusSuccess, StatusWarning, StatusDanger, StatusInfo} {
		h := Tag(TagConfig{Label: "x", Variant: v})
		mustContain(t, h, "fui-tag--"+string(v))
	}
}

func TestTagUnknownVariantPanics(t *testing.T) {
	defer func() { recover() }()
	Tag(TagConfig{Label: "x", Variant: "purple"})
	t.Fatal("expected panic on unknown variant")
}

func TestTagInteractiveBecomesAnchor(t *testing.T) {
	h := Tag(TagConfig{Label: "design", Href: "/?tag=design"})
	mustContain(t, h, "fui-tag--interactive")
	mustContain(t, h, `href="/?tag=design"`)
}

func TestTagDismissAddsRPCButton(t *testing.T) {
	// A dismissal is an in-page state change: the Island is required
	// with the dismiss href, and the same anchor is the no-script
	// destination.
	h := Tag(TagConfig{Label: "design", Dismiss: "/filters/remove",
		Island: headless.Island{Endpoint: "/filters/remove", Signal: "filters"}})
	mustContain(t, h, `href="/filters/remove"`)
	mustContain(t, h, `data-fui-rpc="/filters/remove"`)
	mustContain(t, h, `aria-label="Remove design"`)
	mustContain(t, h, "fui-tag__dismiss")
}

func TestTagCustomDismissLabel(t *testing.T) {
	h := Tag(TagConfig{Label: "x", Dismiss: "/x", DismissLabel: "Clear filter",
		Island: headless.Island{Endpoint: "/x", Signal: "tags"}})
	mustContain(t, h, `aria-label="Clear filter"`)
}

func TestTagNoDismissOmitsButton(t *testing.T) {
	h := Tag(TagConfig{Label: "x"})
	if strings.Contains(string(h), "fui-tag__dismiss") {
		t.Fatalf("Tag without Dismiss should not render × button:\n%s", h)
	}
}

func TestTagExtraAttrsOnEveryRootShape(t *testing.T) {
	extra := map[string]string{"data-test": "hook"}
	for name, h := range map[string]render.HTML{
		"span": Tag(TagConfig{Label: "x", ExtraAttrs: extra}),
		"link": Tag(TagConfig{Label: "x", Href: "/f", ExtraAttrs: extra}),
	} {
		root := string(h)[:strings.Index(string(h), ">")+1]
		if !strings.Contains(root, `data-test="hook"`) {
			t.Errorf("%s root missing data-test:\n%s", name, root)
		}
	}
}

// A chip says its label exactly once, whatever shape it renders: the
// adapter once rendered its own label span INSIDE a whole headless
// Tag, and a reader heard the label twice.
func TestTagLabelOccursExactlyOnce(t *testing.T) {
	count := func(h string, label string) int {
		return strings.Count(h, ">"+label+"<") + strings.Count(h, label+"×")
	}
	dismissable := string(Tag(TagConfig{Label: "beta", Dismiss: "/d",
		Island: headless.Island{Endpoint: "/d", Signal: "s"}}))
	if n := count(dismissable, "beta"); n != 1 {
		t.Errorf("the dismissable render says the label %d times, want exactly 1:\n%s", n, dismissable)
	}
	linked := string(Tag(TagConfig{Label: "filter", Href: "/f"}))
	if n := count(linked, "filter"); n != 1 {
		t.Errorf("the linked render says the label %d times, want exactly 1:\n%s", n, linked)
	}
}
