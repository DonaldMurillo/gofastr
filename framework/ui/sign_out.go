package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// SignOutConfig configures a SignOut control.
type SignOutConfig struct {
	// Action is the POST target; defaults to "/auth/logout".
	Action string
	// Label is the button text; defaults to "Sign out".
	Label string
	// Next is an optional post-logout redirect, sent as a hidden field.
	Next string
	// Variant styles the button; defaults to ButtonGhost.
	Variant ButtonVariant
	Class   string
	// Ctx carries the per-request context used to resolve the sign-out label.
	// When nil, English fallbacks apply.
	Ctx context.Context
}

// SignOut renders a logout control: a minimal form that POSTs to the auth
// battery's logout endpoint. It is a POST (not a link) on purpose — a GET
// logout is trivially triggerable by a stray <img> or prefetch. The button is
// a real ui.Button, so it inherits the design system's styling.
func SignOut(cfg SignOutConfig) render.HTML {
	action := cfg.Action
	if action == "" {
		action = "/auth/logout"
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = i18nui.T(ctx, i18nui.KeySignOut)
	}
	variant := cfg.Variant
	if variant == "" {
		variant = ButtonGhost
	}
	cls := "ui-sign-out"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	var b strings.Builder
	b.WriteString(`<form data-fui-comp="ui-sign-out" class="` + escAttr(cls) + `" method="post" action="` + escAttr(action) + `">`)
	if cfg.Next != "" {
		b.WriteString(`<input type="hidden" name="next" value="` + escAttr(cfg.Next) + `">`)
	}
	b.WriteString(string(Button(ButtonConfig{Label: label, Variant: variant, Type: "submit", Size: ButtonSizeSmall})))
	b.WriteString(`</form>`)
	return signOutStyle.WrapHTML(render.HTML(b.String()))
}

var signOutStyle = registry.RegisterStyle("ui-sign-out", func(_ style.Theme) string {
	return `[data-fui-comp="ui-sign-out"] { display: inline-flex; margin: 0; }`
})

var _ = signOutStyle
