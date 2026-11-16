package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
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
	// ExtraAttrs land on the form; class, data-cui-*, method and action are dropped.
	ExtraAttrs html.Attrs
	// Ctx is the label's language; nil is English.
	Ctx context.Context
}

// SignOut renders a form whose ui.Button POSTs to the logout endpoint.
func SignOut(cfg SignOutConfig) render.HTML {
	action := urlsafe.CleanAnchor(cfg.Action)
	if action == "" {
		action = "/auth/logout"
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = i18nui.SignOut.Text(ctx)
	}
	variant := cfg.Variant
	if variant == "" {
		variant = ButtonGhost
	}
	cls := "fui-sign-out"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	var b strings.Builder
	// csrf-exempt: battery/auth's logout handler enforces same-origin; auth.WithBFFPosture exempts only this route.
	b.WriteString(`<form data-cui-comp="ui-sign-out" class="` + render.Escape(cls) + `" method="post" action="` + render.Escape(action) + `"`)
	b.WriteString(serializeExtraAttrs(html.SafeExtraAttrs(cfg.ExtraAttrs, "method", "action")))
	b.WriteString(`>`)
	if cfg.Next != "" {
		b.WriteString(`<input type="hidden" name="next" value="` + render.Escape(cfg.Next) + `" data-cui-internal>`)
	}
	b.WriteString(string(headless.Own(Button(ButtonConfig{Ctx: ctx, Label: label, Variant: variant, Type: "submit", Size: ButtonSizeSmall}))))
	b.WriteString(`</form>`)
	return signOutStyle.WrapHTML(render.HTML(b.String()))
}

var signOutStyle = registry.RegisterStyle("ui-sign-out", func(_ style.Theme) string {
	return `[data-cui-comp="ui-sign-out"] { display: inline-flex; margin: 0; }`
})

var _ = signOutStyle
