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

func TestTextAreaErrorState(t *testing.T) {
	h := string(TextArea(TextAreaConfig{Name: "x", Label: "x", Error: "Too short"}))
	if !strings.Contains(h, "is-error") {
		t.Errorf("Error state should add .is-error class:\n%s", h)
	}
	if !strings.Contains(h, `aria-invalid="true"`) {
		t.Errorf("Error should mark textarea aria-invalid:\n%s", h)
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
