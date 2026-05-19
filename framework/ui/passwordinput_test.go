package ui

import (
	"strings"
	"testing"
)

func TestPasswordInputRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("PasswordInput without Name should panic")
		}
	}()
	PasswordInput(PasswordInputConfig{ID: "pw"})
}

func TestPasswordInputRequiresID(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("PasswordInput without ID should panic")
		}
	}()
	PasswordInput(PasswordInputConfig{Name: "pw"})
}

func TestPasswordInputEmitsTypePassword(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{Name: "secret", ID: "secret"}))
	if !strings.Contains(h, `type="password"`) {
		t.Errorf("expected type=password:\n%s", h)
	}
	if !strings.Contains(h, `name="secret"`) {
		t.Errorf("expected name=secret:\n%s", h)
	}
	if !strings.Contains(h, `id="secret"`) {
		t.Errorf("expected id=secret:\n%s", h)
	}
}

func TestPasswordInputEmitsToggleButton(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{Name: "pw", ID: "pw"}))
	if !strings.Contains(h, `type="button"`) {
		t.Errorf("expected toggle button type=button:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Show password"`) {
		t.Errorf("expected aria-label Show password:\n%s", h)
	}
	if !strings.Contains(h, `aria-pressed="false"`) {
		t.Errorf("expected aria-pressed=false:\n%s", h)
	}
}

func TestPasswordInputPlaceholder(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw", Placeholder: "Enter password",
	}))
	if !strings.Contains(h, `placeholder="Enter password"`) {
		t.Errorf("expected placeholder:\n%s", h)
	}
}

func TestPasswordInputAutocomplete(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw", Autocomplete: "new-password",
	}))
	if !strings.Contains(h, `autocomplete="new-password"`) {
		t.Errorf("expected autocomplete=new-password:\n%s", h)
	}
}

func TestPasswordInputErrorState(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw", Error: "Too short",
	}))
	if !strings.Contains(h, "is-error") {
		t.Errorf("Error state should add .is-error class:\n%s", h)
	}
	if !strings.Contains(h, `aria-invalid="true"`) {
		t.Errorf("Error state should mark input aria-invalid:\n%s", h)
	}
	if !strings.Contains(h, "Too short") {
		t.Errorf("Error message should render:\n%s", h)
	}
}

func TestPasswordInputErrorInsideComponentScope(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw", Error: "Too short",
	}))
	// The error <p> must be INSIDE the [data-fui-comp] wrapper, not a sibling.
	// Otherwise scoped CSS [data-fui-comp="ui-password-input"] .ui-password-input__error won't match.
	idx := strings.Index(h, `data-fui-comp="ui-password-input"`)
	if idx == -1 {
		t.Fatal("missing data-fui-comp")
	}
	// Find the closing tag of the component wrapper
	closeIdx := strings.LastIndex(h, "</div>")
	if closeIdx == -1 {
		t.Fatal("missing closing div")
	}
	scope := h[idx:closeIdx]
	if !strings.Contains(scope, "ui-password-input__error") {
		t.Errorf("error paragraph must be inside component scope, got HTML:\n%s", h)
	}
	if !strings.Contains(scope, "Too short") {
		t.Errorf("error text must be inside component scope, got HTML:\n%s", h)
	}
}

func TestPasswordInputAttrsCannotOverrideType(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw",
		Attrs: map[string]string{"type": "text"},
	}))
	if !strings.Contains(h, `type="password"`) {
		t.Errorf("type should remain password despite Attrs override, got:\n%s", h)
	}
	// Should NOT have a duplicate type="text"
	if strings.Contains(h, `type="text"`) {
		t.Errorf("type should not be overridden to text, got:\n%s", h)
	}
}

func TestPasswordInputAttrsCannotOverrideName(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw",
		Attrs: map[string]string{"name": "evil"},
	}))
	if !strings.Contains(h, `name="pw"`) {
		t.Errorf("name should remain pw despite Attrs override, got:\n%s", h)
	}
}

func TestPasswordInputAttrsCannotOverrideID(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw",
		Attrs: map[string]string{"id": "evil"},
	}))
	if !strings.Contains(h, `id="pw"`) {
		t.Errorf("id should remain pw despite Attrs override, got:\n%s", h)
	}
}

func TestPasswordInputRequired(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw", Required: true,
	}))
	if !strings.Contains(h, `required`) {
		t.Errorf("expected required attribute:\n%s", h)
	}
}
