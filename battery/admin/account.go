package admin

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The account page is the signed-in user's own settings: who they are,
// the admin's theme in this browser, and their password. It reads only
// the caller's own record, so it is not elevated.

const passwordFormID = "admin-password-form"

// renderAccount draws <prefix>/account.
func (b *Battery) renderAccount(ctx context.Context, _ map[string]string) render.HTML {
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.T(ctx, i18nui.KeyAdminAccountSettings),
			Subtitle: i18nui.T(ctx, i18nui.KeyAdminAccountSub),
		}),
		b.profileCard(ctx),
		appearanceCard(ctx),
	}
	if b.cfg.Auth != nil {
		parts = append(parts, b.passwordCard(ctx))
	}
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerNarrow, Start: true},
		ui.Stack(ui.StackConfig{Gap: ui.GapLG}, parts...))
}

// profileCard is the signed-in user: name, email (with its verified
// state when the store records one) and roles.
func (b *Battery) profileCard(ctx context.Context) render.HTML {
	var items []ui.DetailItem
	if name := userName(ctx); name != "" {
		items = append(items, ui.DetailItem{Label: i18nui.T(ctx, i18nui.KeyAdminName), Value: render.Text(name)})
	}
	if email := userEmail(ctx); email != "" {
		value := render.Text(email)
		if verified, known := b.emailVerified(ctx); known {
			badge := ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.T(ctx, i18nui.KeyAdminUnverified), Variant: ui.StatusWarning})
			if verified {
				badge = ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.T(ctx, i18nui.KeyAdminVerified), Variant: ui.StatusSuccess})
			}
			value = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, value, badge)
		}
		items = append(items, ui.DetailItem{Label: i18nui.T(ctx, i18nui.KeyAdminEmail), Value: value})
	}
	roles := ui.Muted(render.Text(i18nui.T(ctx, i18nui.KeyAdminNoRoles)))
	if held := callerHeldRoles(ctx); len(held) > 0 {
		tags := make([]render.HTML, len(held))
		for i, r := range held {
			tags[i] = ui.Tag(ui.TagConfig{Label: r})
		}
		roles = ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS}, tags...)
	}
	items = append(items, ui.DetailItem{Label: i18nui.T(ctx, i18nui.KeyAdminRoles), Value: roles})
	return ui.Card(ui.CardConfig{
		Heading:      i18nui.T(ctx, i18nui.KeyAdminProfile),
		HeadingLevel: 2,
		Description:  i18nui.T(ctx, i18nui.KeyAdminProfileSub),
	}, ui.DetailList(ui.DetailListConfig{Items: items}))
}

// appearanceCard is the theme choice, the same control and storage the
// toolbar's toggle uses.
func appearanceCard(ctx context.Context) render.HTML {
	return ui.Card(ui.CardConfig{
		Heading:      i18nui.T(ctx, i18nui.KeyAdminAppearance),
		HeadingLevel: 2,
		Description:  i18nui.T(ctx, i18nui.KeyAdminAppearanceSub),
	}, ui.DetailList(ui.DetailListConfig{Items: []ui.DetailItem{{
		Label: i18nui.T(ctx, i18nui.KeyAdminThemeLabel),
		Value: ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeTogglePill, Ctx: ctx}),
	}}}))
}

// passwordCard changes the caller's password through the auth battery's
// POST <base>/password. The form posts JSON through the runtime, which
// carries the CSRF token as a header, so the form holds only the route's
// own fields (the route refuses unknown keys). A refusal comes back as
// per-field errors drawn beside each input. An account with no password
// (OAuth or magic-link only) is told how to set one instead.
func (b *Battery) passwordCard(ctx context.Context) render.HTML {
	card := func(body render.HTML) render.HTML {
		return ui.Card(ui.CardConfig{
			Heading:      i18nui.T(ctx, i18nui.KeyAdminSecurity),
			HeadingLevel: 2,
			Description:  i18nui.T(ctx, i18nui.KeyAdminSecuritySub),
		}, body)
	}
	if has, known := b.hasPassword(ctx); known && !has {
		return card(ui.Callout(ui.CalloutConfig{Variant: ui.StatusInfo}, render.Text(i18nui.T(ctx, i18nui.KeyAdminNoPassword))))
	}
	field := func(name, label, help, autocomplete string) render.HTML {
		id := passwordFormID + "-" + name
		return ui.FormField(ui.FormFieldConfig{
			Label: label, For: id, Help: help, Required: true,
			Input: func(c headless.FieldControl) render.HTML {
				return ui.PasswordInput(ui.PasswordInputConfig{
					Name: name, ID: id, Required: true, Autocomplete: autocomplete, Field: c, Ctx: ctx,
				})
			},
		})
	}
	action := b.cfg.Auth.Config().BasePath + "/password"
	return card(ui.Form(ui.FormConfig{
		Action:      action,
		ID:          passwordFormID,
		SubmitLabel: i18nui.T(ctx, i18nui.KeyAdminChangePassword),
		ExtraAttrs: interactive.Post(action).
			OnSuccessToast(i18nui.T(ctx, i18nui.KeyAdminPasswordChanged)).
			OnSuccess(interactive.ResetForm()).
			Attrs(),
	},
		field("current_password", i18nui.T(ctx, i18nui.KeyAdminCurrentPassword), "", "current-password"),
		field("password", i18nui.T(ctx, i18nui.KeyAdminNewPassword),
			i18nui.TVars(ctx, i18nui.KeyAdminNewPasswordHelp, map[string]string{"min": strconv.Itoa(auth.RecommendedMinPasswordBytes)}),
			"new-password"),
		field("confirm_password", i18nui.T(ctx, i18nui.KeyAdminConfirmPassword), "", "new-password"),
	))
}

// emailVerified asks the auth store whether the caller's address is
// verified. known is false without Auth, a store that does not record
// it, or a failed lookup.
func (b *Battery) emailVerified(ctx context.Context) (verified, known bool) {
	if b.cfg.Auth == nil {
		return false, false
	}
	checker, ok := b.cfg.Auth.UserStore().(auth.EmailVerifiedChecker)
	if !ok {
		return false, false
	}
	v, err := checker.IsEmailVerified(ctx, adminActorID(ctx))
	if err != nil {
		return false, false
	}
	return v, true
}

// hasPassword asks the auth store whether the caller has a password.
// known is false when the store cannot say or the lookup failed; the
// form is drawn then, and the route answers for itself.
func (b *Battery) hasPassword(ctx context.Context) (has, known bool) {
	checker, ok := b.cfg.Auth.UserStore().(auth.PasswordChecker)
	if !ok {
		return false, false
	}
	v, err := checker.HasPassword(ctx, adminActorID(ctx))
	if err != nil {
		return false, false
	}
	return v, true
}

// userName and userEmail read the signed-in user's name and email, or
// "" when the user type carries none.
func userName(ctx context.Context) string {
	u, _ := handlerUser(ctx).(interface{ GetName() string })
	if u == nil {
		return ""
	}
	return u.GetName()
}

func userEmail(ctx context.Context) string {
	u, _ := handlerUser(ctx).(interface{ GetEmail() string })
	if u == nil {
		return ""
	}
	return u.GetEmail()
}
