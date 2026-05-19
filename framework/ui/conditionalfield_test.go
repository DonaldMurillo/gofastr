package ui

import (
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

func TestConditionalFieldRendersHidden(t *testing.T) {
	h := string(ConditionalField(ConditionalFieldConfig{
		WhenName:  "plan",
		WhenValue: "pro",
		Children:  []render.HTML{render.Text("Pro content")},
	}))
	for _, want := range []string{
		`data-fui-comp="ui-conditional-field"`,
		`data-when-name="plan"`,
		`data-when-value="pro"`,
		`hidden`,
		`aria-hidden="true"`,
		"Pro content",
		"ui-conditional-field",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestConditionalFieldVisibleRendersWithoutHidden(t *testing.T) {
	h := string(ConditionalFieldVisible(ConditionalFieldConfig{
		WhenName:  "plan",
		WhenValue: "pro",
		Children:  []render.HTML{render.Text("Pro content")},
	}))
	// Must NOT have hidden attribute.
	if strings.Contains(h, ` hidden`) || strings.Contains(h, `hidden=""`) {
		t.Errorf("ConditionalFieldVisible should NOT have hidden attr: %s", h)
	}
	// A-3: Visible conditional field should NOT have aria-hidden at all.
	// Best practice: omit aria-hidden entirely when visible.
	if strings.Contains(h, "aria-hidden") {
		t.Errorf("ConditionalFieldVisible should NOT have aria-hidden attr: %s", h)
	}
	for _, want := range []string{
		`data-when-name="plan"`,
		`data-when-value="pro"`,
		"Pro content",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestConditionalFieldCustomClass(t *testing.T) {
	h := string(ConditionalField(ConditionalFieldConfig{
		WhenName:  "plan",
		WhenValue: "pro",
		Class:     "extra",
	}))
	if !strings.Contains(h, "ui-conditional-field extra") {
		t.Errorf("expected custom class, got: %s", h)
	}
}

func TestConditionalFieldEvaluateInitialState(t *testing.T) {
	cfg := ConditionalFieldConfig{WhenName: "plan", WhenValue: "pro"}
	if !cfg.EvaluateInitialState("pro") {
		t.Error("should match when value equals WhenValue")
	}
	if cfg.EvaluateInitialState("free") {
		t.Error("should not match when value differs from WhenValue")
	}
}
