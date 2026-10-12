package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// An ActionList is a list of rows, each a link or a button carrying an
// RPC, in a menu row's look but with no menu role: it sits in a panel
// that also holds other controls.
func TestActionList(t *testing.T) {
	out := string(ActionList(ActionListConfig{Label: "Account", Items: []ActionListItem{
		{Label: "Account settings", Href: "/admin/account", Icon: "user"},
		{Label: "Sign out", Do: new(interactive.Post("/auth/logout"))},
	}}))
	for _, want := range []string{
		`<ul aria-label="Account" class="fui-action-list"`,
		`<a class="fui-action-list__item" href="/admin/account"`,
		`<button class="fui-action-list__item" data-cui-rpc="/auth/logout" data-cui-rpc-method="POST" type="button"`,
		`data-cui-rpc="/auth/logout"`,
		`>Sign out<`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
	if strings.Contains(out, `role="menu`) {
		t.Error("an action list is not a menu")
	}
	css := actionListCSS(style.Theme{})
	if !strings.Contains(css, `min-height: var(--spacing-touch-target, 44px)`) {
		t.Error("a row is not a touch target")
	}
	for _, bad := range []ActionListItem{{Label: "x"}, {Label: "x", Href: "/a", Do: new(interactive.Post("/b"))}, {Href: "/a"}, {Label: "x", Href: "/a", Icon: "nope"}} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("ActionList took %+v", bad)
				}
			}()
			ActionList(ActionListConfig{Items: []ActionListItem{bad}})
		}()
	}
}

// A Dropdown's Avatar trigger is the avatar alone, with the label kept
// as its accessible name.
func TestDropdownAvatarTrigger(t *testing.T) {
	out := string(Dropdown(DropdownConfig{Label: "Account", Avatar: &AvatarConfig{Name: "Ada Lovelace"}, Content: "<p>x</p>", Align: DropdownEnd}))
	for _, want := range []string{"fui-dropmenu__trigger--avatar", ">AL<", `<span class="fui-visually-hidden" data-cui-internal="">Account</span></summary>`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in\n%s", want, out)
		}
	}
	if strings.Contains(out, "fui-dropmenu__label") {
		t.Error("an avatar trigger still draws the visible label")
	}
	defer func() {
		if recover() == nil {
			t.Error("Avatar beside Icon did not panic")
		}
	}()
	Dropdown(DropdownConfig{Label: "Account", Avatar: &AvatarConfig{Name: "A"}, Icon: "user", Content: "<p>x</p>"})
}

// Fill stretches a theme pill (the toggle's and the picker's) across
// its container, the options sharing the width.
func TestThemePillFill(t *testing.T) {
	if out := string(ThemeToggle(ThemeToggleConfig{Variant: ThemeTogglePill, Fill: true})); !strings.Contains(out, "fui-theme-toggle--fill") {
		t.Errorf("the toggle has no fill modifier:\n%s", out)
	}
	if out := string(ThemeToggle(ThemeToggleConfig{Variant: ThemeTogglePill})); strings.Contains(out, "fui-theme-toggle--fill") {
		t.Error("the fill modifier rides a toggle that did not ask for it")
	}
	th := style.DefaultTheme()
	th.Colors.Primary = style.Color{Name: "primary", Value: "#654321"}
	if out := string(ThemePicker(ThemePickerConfig{Themes: []ThemeChoice{{Label: "Brutal", Theme: style.RegisterThemeOverride(th)}}, Fill: true})); !strings.Contains(out, "fui-theme-toggle--fill") {
		t.Errorf("the picker has no fill modifier:\n%s", out)
	}
	if css := themeToggleCSS(style.Theme{}); !strings.Contains(css, `.fui-theme-toggle--fill .fui-theme-toggle__option { flex: 1 1 0; }`) {
		t.Error("no fill rule")
	}
}
