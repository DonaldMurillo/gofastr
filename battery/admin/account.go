package admin

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The account page is the signed-in user's own settings: who they are,
// the admin's theme in this browser, and their password. It reads only
// the caller's own record, so it is not elevated.

const (
	passwordFormID = "admin-password-form"
	nameFormID     = "admin-name-form"
)

// renderAccount draws <prefix>/account.
func (b *Battery) renderAccount(ctx context.Context, _ map[string]string) render.HTML {
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.T(ctx, i18nui.KeyAdminAccountSettings),
			Subtitle: i18nui.T(ctx, i18nui.KeyAdminAccountSub),
		}),
		b.profileCard(ctx),
		b.appearanceCard(ctx),
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
	// With a name form the form shows the name; a row would repeat it.
	if name := b.userName(ctx); name != "" && b.nameStore() == nil {
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
	body := []render.HTML{ui.DetailList(ui.DetailListConfig{Items: items})}
	if b.nameStore() != nil {
		body = append(body, b.nameForm(ctx))
	}
	return ui.Card(ui.CardConfig{
		Heading:      i18nui.T(ctx, i18nui.KeyAdminProfile),
		HeadingLevel: 2,
		Description:  i18nui.T(ctx, i18nui.KeyAdminProfileSub),
	}, body...)
}

// nameForm sets the caller's display name, the name the account menu
// and its avatar show. Saving returns to the page, so the shell redraws
// with the new name.
func (b *Battery) nameForm(ctx context.Context) render.HTML {
	action := b.cfg.PathPrefix + "/account/_name"
	return ui.Form(ui.FormConfig{
		Action:      action,
		ID:          nameFormID,
		SubmitLabel: i18nui.T(ctx, i18nui.KeyAdminSaveName),
		ExtraAttrs:  interactive.Post(action).OnSuccess(interactive.Navigate(b.cfg.PathPrefix + "/account")).Attrs(),
	}, ui.TextField(ui.TextFieldConfig{
		Name: "name", ID: nameFormID + "-name", Label: i18nui.T(ctx, i18nui.KeyAdminName),
		Value: b.userName(ctx), Help: i18nui.T(ctx, i18nui.KeyAdminNameHelp),
		AutoComplete: "name", MaxLength: auth.MaxNameRunes,
	}))
}

// handleName stores the caller's display name. It names the caller
// only: a user_id in the body is not read.
func (b *Battery) handleName(w http.ResponseWriter, r *http.Request) {
	page := b.cfg.PathPrefix + "/account"
	vals, ok := b.readOps(w, r, page)
	if !ok {
		return
	}
	store := b.nameStore()
	actor := adminActorID(r.Context())
	if store == nil || actor == "" {
		b.refuse(w, r, page, http.StatusNotImplemented, "failed")
		return
	}
	switch err := store.SetUserName(r.Context(), actor, vals.Get("name")); {
	case errors.Is(err, auth.ErrInvalidName):
		b.refuse(w, r, page, http.StatusBadRequest, "name-refused")
		return
	case err != nil:
		b.logger().Error("admin: set name", "error", textsafe.ScrubControlBytes(err.Error()))
		b.refuse(w, r, page, http.StatusInternalServerError, "failed")
		return
	}
	b.done(w, r, page, "name-saved")
}

// nameStore is the auth store's NameStore, or nil without Auth or a
// store that keeps no name.
func (b *Battery) nameStore() auth.NameStore {
	if b.cfg.Auth == nil {
		return nil
	}
	s, _ := b.cfg.Auth.UserStore().(auth.NameStore)
	return s
}

// appearanceCard is the colour scheme, the same control and storage
// the toolbar's toggle uses, and, with Config.Themes, the page's look.
func (b *Battery) appearanceCard(ctx context.Context) render.HTML {
	items := []ui.DetailItem{{
		Label: i18nui.T(ctx, i18nui.KeyAdminThemeLabel),
		Value: ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeTogglePill, Ctx: ctx}),
	}}
	if look := b.themePicker(ctx); look != "" {
		items = append(items, ui.DetailItem{Label: i18nui.T(ctx, i18nui.KeyAdminLook), Value: look})
	}
	return ui.Card(ui.CardConfig{
		Heading:      i18nui.T(ctx, i18nui.KeyAdminAppearance),
		HeadingLevel: 2,
		Description:  i18nui.T(ctx, i18nui.KeyAdminAppearanceSub),
	}, ui.DetailList(ui.DetailListConfig{Items: items}))
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

// userName reads the signed-in user's display name: the user type's own
// GetName, else the auth store's NameStore, else "".
func (b *Battery) userName(ctx context.Context) string {
	if u, _ := handlerUser(ctx).(interface{ GetName() string }); u != nil {
		if name := u.GetName(); name != "" {
			return name
		}
	}
	store, actor := b.nameStore(), adminActorID(ctx)
	if store == nil || actor == "" {
		return ""
	}
	name, err := store.UserName(ctx, actor)
	if err != nil {
		return ""
	}
	return name
}

// userEmail reads the signed-in user's email, or "" when the user type
// carries none.

func userEmail(ctx context.Context) string {
	u, _ := handlerUser(ctx).(interface{ GetEmail() string })
	if u == nil {
		return ""
	}
	return u.GetEmail()
}
