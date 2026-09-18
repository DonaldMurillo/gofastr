package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func requiredSelect() string {
	return string(Select(SelectConfig{
		Name:     "policy",
		Label:    "Policy",
		Required: true,
		Options:  []SelectOption{{Value: "a", Text: "A"}},
	}))
}

// The required state rides the label (data-required) and the control
// (required), and the stylesheet draws the mark from the state — a
// sibling span would become its own grid row under the label text.
func TestSelectRequiredMarksLabelAndControl(t *testing.T) {
	h := requiredSelect()
	marker := strings.Index(h, `data-required`)
	if marker == -1 {
		t.Fatalf("Required: true marked no label:\n%s", h)
	}
	if marker > strings.Index(h, "</label>") {
		t.Fatalf("the required state is not on the label:\n%s", h)
	}
	// The control's own opening tag carries required: a whole-field
	// search is satisfied by the label's data-required="".
	i := strings.Index(h, "<select")
	if i < 0 {
		t.Fatalf("no <select> in the field:\n%s", h)
	}
	open := h[i:]
	if j := strings.IndexByte(open, '>'); j >= 0 {
		open = open[:j+1]
	}
	if !strings.Contains(open, `required=""`) {
		t.Fatalf("the control itself is not marked required:\n%s", open)
	}
}

// A Select renders BOTH markers it needs wherever it renders: the
// field's (ui-form-field, on the root) and its own (ui-select, on the
// control) — so a Select outside any Form still loads both sheets.
func TestSelectCarriesItsOwnMarkerBesideTheFields(t *testing.T) {
	h := requiredSelect()
	root := h[:strings.Index(h, ">")+1]
	if !strings.Contains(root, `data-fui-comp="ui-form-field"`) {
		t.Fatalf("the field marker is missing from the root:\n%s", root)
	}
	if !strings.Contains(h, `<select class="fui-select"`) {
		t.Fatalf("the control does not carry its classes:\n%s", h)
	}
	sel := h[strings.Index(h, "<select"):strings.Index(h, "</select>")]
	if !strings.Contains(sel, `data-fui-comp="ui-select"`) {
		t.Fatalf("the select's own marker is missing — its sheet would never load outside a FormField:\n%s", sel)
	}
}

// The select's own sheet styles the classes it emits, so the marker is
// never styled by another component's sheet happening to be present.
func TestSelectCSSStylesItsOwnClasses(t *testing.T) {
	css := selectCSS(style.Theme{})
	for _, want := range []string{".fui-select", "var(--fui-field-radius)"} {
		if !strings.Contains(css, want) {
			t.Fatalf("selectCSS missing %q:\n%s", want, css)
		}
	}
}

// ExtraAttrs land on the <select> but never override what the component
// owns (#262): name and required keep their framework values.
func TestSelectExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(Select(SelectConfig{
		Name: "country", Label: "Country", Class: "mine", Required: true,
		Options: []SelectOption{{Value: "fr", Text: "France"}},
		ExtraAttrs: map[string]string{
			"data-test": "hook", "name": "evil", "Class": "evil", "data-fui-comp": "spoof",
		},
	}))
	sel := h[strings.Index(h, "<select"):strings.Index(h, "</select>")]
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(sel, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, sel)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `name="country"`, `class="fui-select"`, `required=""`,
	} {
		if !strings.Contains(sel, want) {
			t.Errorf("select missing %q:\n%s", want, sel)
		}
	}
	// The relation attribute survives: the generator's mount hook
	// populates the select by it.
	rel := string(Select(SelectConfig{
		Name: "owner", Label: "Owner",
		Options:    []SelectOption{{Value: "1", Text: "One"}},
		ExtraAttrs: map[string]string{"data-rel-entity": "users"},
	}))
	if !strings.Contains(rel, `data-rel-entity="users"`) {
		t.Errorf("data-rel-entity dropped from the select:\n%s", rel)
	}
}

// Help and error are both visible when both are set, the error first,
// and both ids ride the control's described-by — the field family's
// contract, which Select inherits from headless.Field.
func TestSelectHelpAndErrorBothVisible(t *testing.T) {
	h := string(Select(SelectConfig{
		Name: "policy", Label: "Policy",
		Options: []SelectOption{{Value: "a", Text: "A"}},
		Help:    "Pick the strictest that fits.",
		Error:   "Pick one.",
	}))
	if !strings.Contains(h, `aria-describedby="policy-error policy-hint"`) {
		t.Errorf("the control must carry both ids, error first:\n%s", h)
	}
	errAt := strings.Index(h, `id="policy-error"`)
	hintAt := strings.Index(h, `id="policy-hint"`)
	// A missing node indexes at -1 and -1 compares as "in order", so
	// absence fails first, before the order comparison runs.
	if errAt == -1 || hintAt == -1 {
		t.Fatalf("the error node or the hint node is missing (error at %d, hint at %d):\n%s", errAt, hintAt, h)
	}
	if errAt > hintAt {
		t.Errorf("the error must be drawn before the hint:\n%s", h)
	}
}
