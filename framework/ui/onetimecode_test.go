package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// The code field is what the OS and the phone fill a one-time code into:
// a text input asking for one-time-code on the digit keyboard, six
// digits by pattern and length, never spell-checked, named by its label.
func TestOneTimeCodeIsAFillableCodeField(t *testing.T) {
	out := string(FormField(FormFieldConfig{Label: "Authentication code", For: "otp", Required: true,
		Input: func(c headless.FieldControl) render.HTML { return OneTimeCode(OneTimeCodeConfig{Field: c}) }}))
	for _, want := range []string{
		`type="text"`, `name="code"`, `autocomplete="one-time-code"`, `inputmode="numeric"`,
		`pattern="[0-9]{6}"`, `minlength="6"`, `maxlength="6"`, `spellcheck="false"`,
		`autocapitalize="off"`, `enterkeyhint="done"`, `id="otp"`, `for="otp"`, `required`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the code field lacks %s:\n%s", want, out)
		}
	}
	if eight := string(OneTimeCode(OneTimeCodeConfig{Field: headless.FieldControl{ID: "x"}, Digits: 8})); !strings.Contains(eight, `pattern="[0-9]{8}"`) || !strings.Contains(eight, `maxlength="8"`) {
		t.Errorf("Digits does not set the pattern and length:\n%s", eight)
	}
}

// A revealed password is a text input: spell check and capitalisation
// stay off on it, and the sign-in hints reach the input that submits.
func TestPasswordInputSignInHints(t *testing.T) {
	out := string(PasswordInput(PasswordInputConfig{Name: "password", ID: "pw", Autocomplete: "new-password", MinLength: 8, EnterKeyHint: "go"}))
	input := out[strings.Index(out, "<input"):]
	input = input[:strings.Index(input, ">")]
	for _, want := range []string{`spellcheck="false"`, `autocapitalize="off"`, `minlength="8"`, `enterkeyhint="go"`, `autocomplete="new-password"`} {
		if !strings.Contains(input, want) {
			t.Errorf("the password input lacks %s: %s", want, input)
		}
	}
}

// The identifier field's hints land on the input.
func TestControlSignInHints(t *testing.T) {
	out := string(Control(ControlConfig{Field: headless.FieldControl{ID: "e"}, Type: "email", Name: "email", AutoComplete: "username", EnterKeyHint: "next", InputMode: "email", NoSpellCheck: true}))
	for _, want := range []string{`autocomplete="username"`, `enterkeyhint="next"`, `inputmode="email"`, `spellcheck="false"`, `autocapitalize="off"`} {
		if !strings.Contains(out, want) {
			t.Errorf("the control lacks %s: %s", want, out)
		}
	}
}

// AutoCapitalize lands on the input, wins over NoSpellCheck's "off",
// and a value outside the attribute's vocabulary is refused.
func TestControlAutoCapitalize(t *testing.T) {
	out := string(Control(ControlConfig{Field: headless.FieldControl{ID: "n"}, Name: "name", AutoCapitalize: AutoCapitalizeWords, NoSpellCheck: true}))
	if !strings.Contains(out, `autocapitalize="words"`) || strings.Contains(out, `autocapitalize="off"`) {
		t.Errorf("the control should carry autocapitalize=words only: %s", out)
	}
	if out := string(Control(ControlConfig{Field: headless.FieldControl{ID: "n"}, Name: "name"})); strings.Contains(out, "autocapitalize") {
		t.Errorf("an unset AutoCapitalize should leave the browser default: %s", out)
	}
	defer func() {
		if recover() == nil {
			t.Error("an AutoCapitalize outside off, sentences, words and characters was not refused")
		}
	}()
	Control(ControlConfig{Field: headless.FieldControl{ID: "n"}, Name: "name", AutoCapitalize: "shout"})
}
