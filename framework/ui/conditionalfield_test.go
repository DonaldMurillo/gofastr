package ui

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestConditionalFieldRequiresWhenName(t *testing.T) {
	defer func() { recover() }()
	ConditionalField(ConditionalFieldConfig{WhenValue: "pro", Children: []render.HTML{render.Text("x")}})
	t.Fatal("expected panic without WhenName")
}

func TestConditionalFieldRequiresWhenValue(t *testing.T) {
	defer func() { recover() }()
	ConditionalField(ConditionalFieldConfig{WhenName: "plan", Children: []render.HTML{render.Text("x")}})
	t.Fatal("expected panic without WhenValue")
}

// The reader-without-script case is the point of the posture: the
// region ships VISIBLE, because a field only a script can reveal is a
// field a scriptless reader never reaches. The module hides it until
// the watched field matches — never the other way round.
func TestConditionalFieldRendersVisible(t *testing.T) {
	h := string(ConditionalField(ConditionalFieldConfig{
		WhenName:  "plan",
		WhenValue: "pro",
		Children:  []render.HTML{render.Text("Pro content")},
	}))
	for _, want := range []string{
		`data-cui-comp="ui-conditional-field"`,
		`data-hui-when="plan"`,
		`data-hui-when-value="pro"`,
		"Pro content",
		"fui-when",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
	if strings.Contains(h, "hidden") {
		t.Errorf("the region must not ship hidden — without script the field inside is unreachable:\n%s", h)
	}
	if strings.Contains(h, "aria-hidden") {
		t.Errorf("the region must not ship aria-hidden:\n%s", h)
	}
}

func TestConditionalFieldCustomClass(t *testing.T) {
	h := string(ConditionalField(ConditionalFieldConfig{
		WhenName:  "plan",
		WhenValue: "pro",
		Class:     "extra",
		Children:  []render.HTML{render.Text("x")},
	}))
	if !strings.Contains(h, `"fui-when extra"`) {
		t.Errorf("expected custom class appended to the region, got: %s", h)
	}
}

// The hooks and the visibility are the runtime's contract: a caller
// cannot forge the hooks the module binds to, and cannot pre-hide or
// pre-aria-hide the region the module owns the visibility of.
func TestConditionalFieldExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(ConditionalField(ConditionalFieldConfig{
		WhenName:  "plan",
		WhenValue: "pro",
		ExtraAttrs: map[string]string{
			"data-test": "hook", "hidden": "", "aria-hidden": "true",
			"data-hui-when": "evil", "data-hui-when-value": "evil",
		},
		Children: []render.HTML{render.Text("x")},
	}))
	for _, want := range []string{`data-test="hook"`, `data-hui-when="plan"`, `data-hui-when-value="pro"`} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
	for _, banned := range []string{"evil", "hidden"} {
		if strings.Contains(h, banned) {
			t.Errorf("owned attribute smuggled through ExtraAttrs (%q):\n%s", banned, h)
		}
	}
}

// The in condition: shown while the watched field holds any listed
// value. The kit forwards the list to the headless region, whose JSON
// encoding keeps a member containing a comma, a quote or a bracket one
// member.
func TestConditionalFieldInRendersJSONList(t *testing.T) {
	h := string(ConditionalField(ConditionalFieldConfig{
		WhenName:   "notify",
		WhenValues: []string{"email", `webhook, "both"]`},
		Children:   []render.HTML{render.Text("x")},
	}))
	for _, want := range []string{
		`data-cui-comp="ui-conditional-field"`,
		`data-hui-when="notify"`,
		`data-hui-when-in="`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
	if strings.Contains(h, "data-hui-when-value") {
		t.Errorf("an in condition must not carry the single-value hook:\n%s", h)
	}
	// The exact spelling: the attribute is the JSON encoding of the
	// list, HTML-escaped for the attribute slot.
	raw, _ := json.Marshal([]string{"email", `webhook, "both"]`})
	if want := `data-hui-when-in="` + render.Escape(string(raw)) + `"`; !strings.Contains(h, want) {
		t.Errorf("missing %q in: %s", want, h)
	}
	if strings.Contains(h, "hidden") {
		t.Errorf("the region must not ship hidden — without script the field inside is unreachable:\n%s", h)
	}
}

// Exactly one condition per region: both spellings is two conditions
// wearing one region, and neither is a div.
func TestConditionalFieldInRefusesAmbiguous(t *testing.T) {
	defer func() { recover() }()
	ConditionalField(ConditionalFieldConfig{
		WhenName:   "plan",
		WhenValue:  "pro",
		WhenValues: []string{"pro"},
		Children:   []render.HTML{render.Text("x")},
	})
	t.Fatal("expected panic with both WhenValue and WhenValues")
}

func TestConditionalFieldInRequiresValues(t *testing.T) {
	defer func() { recover() }()
	ConditionalField(ConditionalFieldConfig{
		WhenName: "plan",
		Children: []render.HTML{render.Text("x")},
	})
	t.Fatal("expected panic without WhenValue or WhenValues")
}

// The list hook is the runtime's contract like the others: a caller
// cannot forge the list the module will obey.
func TestConditionalFieldInExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(ConditionalField(ConditionalFieldConfig{
		WhenName:   "plan",
		WhenValues: []string{"pro"},
		ExtraAttrs: map[string]string{
			"data-hui-when-in": `["evil"]`,
		},
		Children: []render.HTML{render.Text("x")},
	}))
	if !strings.Contains(h, `data-hui-when-in="[&quot;pro&quot;]"`) {
		t.Errorf("the owned list hook was lost or mis-encoded:\n%s", h)
	}
	if strings.Contains(h, "evil") {
		t.Errorf("a caller forged the hook the runtime binds to:\n%s", h)
	}
}
