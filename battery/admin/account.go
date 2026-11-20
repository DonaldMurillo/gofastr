package admin

import (
	"context"
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const (
	passwordFormID = "admin-password-form"
	nameFormID     = "admin-name-form"
)

func (b *Battery) renderAccount(ctx context.Context, _ map[string]string) render.HTML {
	parts := []render.HTML{
		ui.PageHeader(ui.PageHeaderConfig{
			Title:    i18nui.AdminAccountSettings.Text(ctx),
			Subtitle: i18nui.AdminAccountSub.Text(ctx),
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

func (b *Battery) profileCard(ctx context.Context) render.HTML {
	var items []ui.DetailItem
	if name := b.userName(ctx); name != "" && b.nameStore() == nil {
		items = append(items, ui.DetailItem{Label: i18nui.AdminName.Text(ctx), Value: render.Text(name)})
	}
	if email := userEmail(ctx); email != "" {
		value := render.Text(email)
		if verified, known := b.emailVerified(ctx); known {
			badge := ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.AdminUnverified.Text(ctx), Variant: ui.StatusWarning})
			if verified {
				badge = ui.StatusBadge(ui.StatusBadgeConfig{Label: i18nui.AdminVerified.Text(ctx), Variant: ui.StatusSuccess})
			}
			value = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter}, value, badge)
		}
		items = append(items, ui.DetailItem{Label: i18nui.AdminEmail.Text(ctx), Value: value})
	}
	roles := ui.Muted(render.Text(i18nui.AdminNoRoles.Text(ctx)))
	if held := callerHeldRoles(ctx); len(held) > 0 {
		tags := make([]render.HTML, len(held))
		for i, r := range held {
			tags[i] = ui.Tag(ui.TagConfig{Ctx: ctx, Label: r})
		}
		roles = ui.Cluster(ui.ClusterConfig{Gap: ui.GapXS}, tags...)
	}
	items = append(items, ui.DetailItem{Label: i18nui.AdminRoles.Text(ctx), Value: roles})
	body := []render.HTML{ui.DetailList(ui.DetailListConfig{Items: items})}
	if b.nameStore() != nil {
		body = append(body, b.nameForm(ctx))
	}
	return ui.Card(ui.CardConfig{
		Heading:      i18nui.AdminProfile.Text(ctx),
		HeadingLevel: 2,
		Description:  i18nui.AdminProfileSub.Text(ctx),
	}, body...)
}

func (b *Battery) nameForm(ctx context.Context) render.HTML {
	action := b.cfg.PathPrefix + "/account/_name"
	return ui.Form(ui.FormConfig{
		Ctx:         ctx,
		Action:      action,
		ID:          nameFormID,
		SubmitLabel: i18nui.AdminSaveName.Text(ctx),
		ExtraAttrs:  interactive.Post(action).OnSuccess(interactive.Navigate(b.cfg.PathPrefix + "/account")).Attrs(),
	}, ui.TextField(ui.TextFieldConfig{Ctx: ctx,
		Name: "name", ID: nameFormID + "-name", Label: i18nui.AdminName.Text(ctx),
		Value: b.userName(ctx), Help: i18nui.AdminNameHelp.Text(ctx),
		AutoComplete: "name", MaxLength: auth.MaxNameRunes,
	}))
}

func (b *Battery) handleName(w http.ResponseWriter, r *http.Request) {
	page := b.cfg.PathPrefix + "/account"
	vals, ok := b.readOps(w, r, page)
	if !ok {
		return
	}
	store := b.nameStore()
	actor := callerUserID(r.Context())
	if store == nil || actor == "" {
		b.refuse(w, r, page, http.StatusNotImplemented, "failed")
		return
	}
	name := vals.Get("name")
	switch err := store.SetUserName(r.Context(), actor, name); {
	case errors.Is(err, auth.ErrInvalidName):
		b.refuse(w, r, page, http.StatusBadRequest, "name-refused")
		return
	case err != nil:
		b.logger().Error("admin: set name", "error", textsafe.ScrubControlBytes(err.Error()))
		b.refuse(w, r, page, http.StatusInternalServerError, "failed")
		return
	}
	stored, _ := auth.CleanName(name)
	b.appendAudit(r.Context(), "account", "set-name", actor, actor, map[string]any{"name": stored})
	b.done(w, r, page, "name-saved")
}

func (b *Battery) nameStore() auth.NameStore {
	if b.cfg.Auth == nil {
		return nil
	}
	s, _ := b.cfg.Auth.UserStore().(auth.NameStore)
	return s
}

func (b *Battery) appearanceCard(ctx context.Context) render.HTML {
	items := []ui.DetailItem{{
		Label: i18nui.AdminThemeLabel.Text(ctx),
		Value: ui.ThemeToggle(ui.ThemeToggleConfig{Variant: ui.ThemeTogglePill, Ctx: ctx}),
	}}
	if look := b.themePicker(ctx, false); look != "" {
		items = append(items, ui.DetailItem{Label: i18nui.AdminLook.Text(ctx), Value: look})
	}
	return ui.Card(ui.CardConfig{
		Heading:      i18nui.AdminAppearance.Text(ctx),
		HeadingLevel: 2,
		Description:  i18nui.AdminAppearanceSub.Text(ctx),
	}, ui.DetailList(ui.DetailListConfig{Items: items}))
}

func (b *Battery) passwordCard(ctx context.Context) render.HTML {
	card := func(body render.HTML) render.HTML {
		return ui.Card(ui.CardConfig{
			Heading:      i18nui.AdminSecurity.Text(ctx),
			HeadingLevel: 2,
			Description:  i18nui.AdminSecuritySub.Text(ctx),
		}, body)
	}
	if has, known := b.hasPassword(ctx); known && !has {
		return card(ui.Callout(ui.CalloutConfig{Variant: ui.StatusInfo}, render.Text(i18nui.AdminNoPassword.Text(ctx))))
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
		Ctx:         ctx,
		Action:      action,
		ID:          passwordFormID,
		SubmitLabel: i18nui.AdminChangePassword.Text(ctx),
		ExtraAttrs: interactive.Post(action).
			OnSuccessToast(i18nui.AdminPasswordChanged.Text(ctx)).
			OnSuccess(interactive.ResetForm()).
			Attrs(),
	},
		field("current_password", i18nui.AdminCurrentPassword.Text(ctx), "", "current-password"),
		field("password", i18nui.AdminNewPassword.Text(ctx),
			i18nui.AdminNewPasswordHelp.Text(ctx, map[string]any{"min": auth.RecommendedMinPasswordBytes}),
			"new-password"),
	))
}

func (b *Battery) emailVerified(ctx context.Context) (verified, known bool) {
	if b.cfg.Auth == nil {
		return false, false
	}
	checker, ok := b.cfg.Auth.UserStore().(auth.EmailVerifiedChecker)
	if !ok {
		return false, false
	}
	id := callerUserID(ctx)
	if id == "" {
		return false, false
	}
	v, err := checker.IsEmailVerified(ctx, id)
	if err != nil {
		return false, false
	}
	return v, true
}

func (b *Battery) hasPassword(ctx context.Context) (has, known bool) {
	checker, ok := b.cfg.Auth.UserStore().(auth.PasswordChecker)
	if !ok {
		return false, false
	}
	id := callerUserID(ctx)
	if id == "" {
		return false, false
	}
	v, err := checker.HasPassword(ctx, id)
	if err != nil {
		return false, false
	}
	return v, true
}

func (b *Battery) userName(ctx context.Context) string {
	if u, _ := handlerUser(ctx).(interface{ GetName() string }); u != nil {
		if name := u.GetName(); name != "" {
			return name
		}
	}
	store, actor := b.nameStore(), callerUserID(ctx)
	if store == nil || actor == "" {
		return ""
	}
	name, err := store.UserName(ctx, actor)
	if err != nil {
		return ""
	}
	return name
}

func userEmail(ctx context.Context) string {
	u, _ := handlerUser(ctx).(interface{ GetEmail() string })
	if u == nil {
		return ""
	}
	return u.GetEmail()
}
