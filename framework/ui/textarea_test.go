package ui

import (
	"strings"
	"testing"
)

func TestTextAreaRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TextArea without Name should panic")
		}
	}()
	TextArea(TextAreaConfig{Label: "x"})
}

func TestTextAreaRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TextArea without Label should panic")
		}
	}()
	TextArea(TextAreaConfig{Name: "x"})
}

func TestTextAreaEmitsTextareaWithName(t *testing.T) {
	h := string(TextArea(TextAreaConfig{Name: "bio", Label: "Bio", Value: "hello"}))
	if !strings.Contains(h, `<textarea`) {
		t.Errorf("expected <textarea> tag:\n%s", h)
	}
	if !strings.Contains(h, `name="bio"`) {
		t.Errorf("expected name=bio:\n%s", h)
	}
	if !strings.Contains(h, ">hello<") {
		t.Errorf("expected initial Value in element body:\n%s", h)
	}
}

// The control carries the component's own marker, so its sheet loads
// wherever a TextArea renders — inside a Form or alone — beside the
// field marker that fetches the field sheet.
func TestTextAreaControlCarriesItsOwnMarker(t *testing.T) {
	h := string(TextArea(TextAreaConfig{Name: "bio", Label: "Bio"}))
	ta := extraAttrsOpeningTag(t, h, "textarea")
	if !strings.Contains(ta, `data-fui-comp="ui-textarea"`) {
		t.Errorf("the textarea should carry the ui-textarea marker:\n%s", ta)
	}
	if !strings.Contains(ta, `class="fui-textarea"`) {
		t.Errorf("the textarea should wear the control class:\n%s", ta)
	}
}

func TestTextAreaAutogrowAddsMarker(t *testing.T) {
	on := string(TextArea(TextAreaConfig{Name: "x", Label: "x", Autogrow: true}))
	if !strings.Contains(on, "data-fui-autogrow") {
		t.Errorf("Autogrow=true should emit data-fui-autogrow:\n%s", on)
	}
	off := string(TextArea(TextAreaConfig{Name: "x", Label: "x"}))
	if strings.Contains(off, "data-fui-autogrow") {
		t.Errorf("default Autogrow=false should NOT emit marker:\n%s", off)
	}
}

// The field owns the message: the error renders as a role="alert"
// paragraph wired into the control's aria-describedby, and the
// control carries the invalid state.
func TestTextAreaErrorState(t *testing.T) {
	h := string(TextArea(TextAreaConfig{Name: "x", Label: "x", Error: "Too short"}))
	if !strings.Contains(h, `aria-invalid="true"`) {
		t.Errorf("Error should mark textarea aria-invalid:\n%s", h)
	}
	if !strings.Contains(h, `role="alert"`) {
		t.Errorf("Error should render an alert paragraph:\n%s", h)
	}
	if !strings.Contains(h, `aria-describedby="x-error"`) {
		t.Errorf("Error should be wired into the control's described-by:\n%s", h)
	}
}

func TestTextAreaHelpWiresDescribedBy(t *testing.T) {
	h := string(TextArea(TextAreaConfig{Name: "x", Label: "x", Help: "Plain text"}))
	if !strings.Contains(h, `aria-describedby="x-hint"`) {
		t.Errorf("Help should be wired into the control's described-by:\n%s", h)
	}
	if !strings.Contains(h, "Plain text") {
		t.Errorf("Help text missing:\n%s", h)
	}
}

func TestTextAreaLabelForMatchesID(t *testing.T) {
	h := string(TextArea(TextAreaConfig{Name: "feedback", Label: "Feedback"}))
	if !strings.Contains(h, `for="feedback"`) {
		t.Errorf("label[for] should default to Name:\n%s", h)
	}
	if !strings.Contains(h, `id="feedback"`) {
		t.Errorf("textarea[id] should default to Name:\n%s", h)
	}
}

// ExtraAttrs land on the <textarea> but never override what the
// component owns (#262): rows keeps its framework value; the
// data-fui-autogrow wiring cannot be spoofed.
func TestTextAreaExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(TextArea(TextAreaConfig{
		Name: "bio", Label: "Bio", Class: "mine", Placeholder: "Tell us",
		ExtraAttrs: map[string]string{
			"data-test": "hook", "rows": "evil", "Class": "evil", "data-fui-autogrow": "spoof",
		},
	}))
	ta := extraAttrsOpeningTag(t, h, "textarea")
	for _, banned := range []string{"evil", "spoof"} {
		if strings.Contains(ta, banned) {
			t.Errorf("owned attr overridden by ExtraAttrs (%q):\n%s", banned, ta)
		}
	}
	for _, want := range []string{
		`data-test="hook"`, `name="bio"`, `rows="3"`, `placeholder="Tell us"`,
	} {
		if !strings.Contains(ta, want) {
			t.Errorf("textarea missing %q:\n%s", want, ta)
		}
	}
}
