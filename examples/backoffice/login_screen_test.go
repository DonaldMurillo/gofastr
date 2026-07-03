package main

import (
	"context"
	"strings"
	"testing"
)

// Pins the sign-in form's input contract (ids, names, types,
// autocomplete, prefill, required) so the markup can move from
// hand-rolled render.Raw to the design system's html.Input without
// behavior drift.
func TestLoginScreenInputs(t *testing.T) {
	out := string(loginScreen{}.RenderCtx(context.Background()))
	for _, want := range []string{
		`id="f-email"`, `name="email"`, `type="email"`,
		`autocomplete="email"`, `value="admin@example.com"`, `required`,
		`id="f-password"`, `name="password"`, `type="password"`,
		`autocomplete="current-password"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("login form missing %s in:\n%s", want, out)
		}
	}
}
