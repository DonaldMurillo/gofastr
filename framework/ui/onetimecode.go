package ui

import (
	"strconv"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// OneTimeCodeConfig configures a OneTimeCode control.
type OneTimeCodeConfig struct {
	// Field is the wiring the enclosing FormField handed its builder:
	// the id its label points at, the description and the invalid
	// state. Required, as for Control.
	Field headless.FieldControl
	// Name is the form-field name. Default "code", the field
	// battery/auth's /2fa/challenge reads.
	Name string
	// Digits is how many digits the code has. Default 6, an
	// authenticator app's TOTP code.
	Digits int
	// EnterKeyHint labels the on-screen keyboard's enter key.
	// Default "done".
	EnterKeyHint string
	// ExtraAttrs forwards additional attributes (data-* test hooks) to
	// the input, under Control's rules; the code attributes above are
	// the component's own and win.
	ExtraAttrs html.Attrs
}

// OneTimeCode renders the input for a one-time code (an authenticator
// app's TOTP code, a code sent by SMS or email): a text input the
// browser and the phone treat as a one-time code. It carries
// autocomplete="one-time-code" (so the OS offers the code it just
// received), inputmode="numeric" (the digit keyboard), a digits-only
// pattern with matching minlength and maxlength, and no spell check or
// auto-capitalisation. Build it inside a FormField, whose label names
// it:
//
//	ui.FormField(ui.FormFieldConfig{Label: i18nui.AuthOneTimeCode.Text(ctx), For: "otp", Required: true,
//	    Input: func(c headless.FieldControl) render.HTML {
//	        return ui.OneTimeCode(ui.OneTimeCodeConfig{Field: c})
//	    }})
//
// The type stays text, not number: a code keeps its leading zeros and
// gets no spinner.
func OneTimeCode(cfg OneTimeCodeConfig) render.HTML {
	name := cfg.Name
	if name == "" {
		name = "code"
	}
	digits := cfg.Digits
	if digits <= 0 {
		digits = 6
	}
	enter := cfg.EnterKeyHint
	if enter == "" {
		enter = "done"
	}
	extra := html.SafeExtraAttrs(cfg.ExtraAttrs, "pattern")
	if extra == nil {
		extra = html.Attrs{}
	}
	extra["pattern"] = "[0-9]{" + strconv.Itoa(digits) + "}"
	return Control(ControlConfig{
		Field:        cfg.Field,
		Type:         "text",
		Name:         name,
		AutoComplete: "one-time-code",
		InputMode:    "numeric",
		EnterKeyHint: enter,
		NoSpellCheck: true,
		MinLength:    digits,
		MaxLength:    digits,
		ExtraAttrs:   extra,
	})
}
