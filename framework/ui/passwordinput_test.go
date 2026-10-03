package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/headless"
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
			t.Fatal("PasswordInput without ID (and without a Field carrying one) should panic")
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

// The reveal button is the headless module's contract: data-hui-reveal
// on the button, data-hui-affix on the shell, data-hui-affix-input on
// the input, and the four label/text attributes the module swaps
// between. The retired passwordinput.js module bound none of these.
func TestPasswordInputEmitsTheHeadlessRevealHooks(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{Name: "pw", ID: "pw"}))
	for _, want := range []string{
		`type="button"`,
		`data-hui-reveal`,
		`data-hui-affix`,
		`data-hui-affix-input`,
		`aria-label="Show password"`,
		`data-hui-show-label="Show password"`,
		`data-hui-hide-label="Hide password"`,
		`data-hui-show-text="Show"`,
		`data-hui-hide-text="Hide"`,
		`aria-pressed="false"`,
		`>Show<`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("reveal contract missing %q:\n%s", want, h)
		}
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

// The invalid state arrives as data-invalid on the SHELL (the input
// inside has no border of its own to colour) and as aria-invalid on
// the input — driven by the field's wiring, never by a config Error:
// the affix shell is a control and renders no message of its own.
func TestPasswordInputInvalidMarksTheShell(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw",
		Field: headless.FieldControl{ID: "pw", Invalid: true},
	}))
	if !strings.Contains(h, `data-invalid`) {
		t.Errorf("the shell must carry data-invalid:\n%s", h)
	}
	if !strings.Contains(h, `aria-invalid="true"`) {
		t.Errorf("the input must carry aria-invalid:\n%s", h)
	}
}

func TestPasswordInputAttrsCannotOverrideType(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw",
		ExtraAttrs: map[string]string{"type": "text"},
	}))
	if !strings.Contains(h, `type="password"`) {
		t.Errorf("type should remain password despite Attrs override, got:\n%s", h)
	}
	if strings.Contains(h, `type="text"`) {
		t.Errorf("type should not be overridden to text, got:\n%s", h)
	}
}

func TestPasswordInputAttrsCannotOverrideName(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw",
		ExtraAttrs: map[string]string{"name": "evil"},
	}))
	if strings.Contains(h, `name="evil"`) {
		t.Errorf("name should not be overridable, got:\n%s", h)
	}
}

func TestPasswordInputAttrsCannotOverrideID(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw",
		ExtraAttrs: map[string]string{"id": "evil"},
	}))
	if strings.Contains(h, `id="evil"`) {
		t.Errorf("id should not be overridable, got:\n%s", h)
	}
}

// Extras land on the shell's ROOT — the contract every component's
// ExtraAttrs carries — never on the inner input that submits, and
// never override what the component owns (#262); the reveal hooks
// cannot be forged: a data-hui-* or data-fui-* key is dropped on the
// way in.
func TestPasswordInputExtraAttrsCannotOverrideOwned(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "pw",
		ExtraAttrs: map[string]string{
			"data-testid":     "pw-field",
			"data-hui-reveal": "forged",
			"data-fui-rpc":    "/evil",
		},
	}))
	root := h[:strings.Index(h, ">")+1]
	if !strings.Contains(root, `data-testid="pw-field"`) {
		t.Errorf("benign extra must reach the shell's root:\n%s", h)
	}
	i := strings.Index(h, "<input")
	input := h[i : i+strings.Index(h[i:], ">")+1]
	if strings.Contains(input, `data-testid`) {
		t.Errorf("extras must not land on the inner input that submits:\n%s", input)
	}
	if strings.Contains(h, `data-hui-reveal="forged"`) {
		t.Errorf("a forged data-hui hook must not ship:\n%s", h)
	}
	if strings.Contains(h, `data-fui-rpc`) {
		t.Errorf("a data-fui-* key must not ship:\n%s", h)
	}
}

func TestPasswordInputCarriesTheFieldsWiring(t *testing.T) {
	h := string(PasswordInput(PasswordInputConfig{
		Name: "pw", ID: "ignored",
		Field: headless.FieldControl{
			ID:          "f1",
			DescribedBy: "f1-error f1-hint",
			Invalid:     true,
			Required:    true,
		},
	}))
	s := string(h)
	i := strings.Index(s, "<input")
	input := s[i : i+strings.Index(s[i:], ">")+1]
	for _, want := range []string{`id="f1"`, `aria-describedby="f1-error f1-hint"`, `aria-invalid="true"`, "required"} {
		if !strings.Contains(input, want) {
			t.Errorf("inner input missing %q:\n%s", want, input)
		}
	}
	if strings.Contains(h, `id="ignored"`) {
		t.Errorf("the field's id must win over the config's own:\n%s", h)
	}
}
