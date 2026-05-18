package ui

import (
	"strings"
	"testing"
)

func TestTagInputRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TagInput without Name should panic")
		}
	}()
	TagInput(TagInputConfig{Label: "x"})
}

func TestTagInputRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("TagInput without Label should panic")
		}
	}()
	TagInput(TagInputConfig{Name: "x"})
}

func TestTagInputEmitsTextInputWithMarker(t *testing.T) {
	h := string(TagInput(TagInputConfig{Name: "tags", Label: "Tags"}))
	if !strings.Contains(h, `type="text"`) {
		t.Errorf("expected <input type=text>:\n%s", h)
	}
	if !strings.Contains(h, `data-fui-tag-input="tags"`) {
		t.Errorf("expected runtime marker:\n%s", h)
	}
	if !strings.Contains(h, `autocomplete="off"`) {
		t.Errorf("text input should disable autocomplete:\n%s", h)
	}
}

func TestTagInputInitialValuesRenderAsHiddenInputs(t *testing.T) {
	h := string(TagInput(TagInputConfig{
		Name: "tags", Label: "Tags",
		Values: []string{"go", "rust", "ts"},
	}))
	if c := strings.Count(h, `type="hidden"`); c != 3 {
		t.Errorf("expected 3 hidden inputs for 3 initial tags, got %d:\n%s", c, h)
	}
	if c := strings.Count(h, ` name="tags"`); c < 3 {
		t.Errorf("each hidden input should use the shared Name, got %d:\n%s", c, h)
	}
	if !strings.Contains(h, `value="go"`) {
		t.Errorf("expected initial Value=go:\n%s", h)
	}
}

func TestTagInputAriaLabel(t *testing.T) {
	h := string(TagInput(TagInputConfig{Name: "tags", Label: "Tags"}))
	if !strings.Contains(h, `aria-label="Tags"`) {
		t.Errorf("text input should carry aria-label=Label:\n%s", h)
	}
}

func TestTagInputMaxLength(t *testing.T) {
	h := string(TagInput(TagInputConfig{
		Name: "tags", Label: "Tags", MaxLength: 32,
	}))
	if !strings.Contains(h, `maxlength="32"`) {
		t.Errorf("expected maxlength attr:\n%s", h)
	}
}
