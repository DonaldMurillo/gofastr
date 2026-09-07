package ui_test

import (
	"strings"
	"testing"

	ui "github.com/DonaldMurillo/gofastr/framework/ui"
)

// Pins: the Menu form-action row allow-lists URL schemes
// (urlsafe.CleanAnchor, like every form-action sink) and honours
// MenuItem.Confirm as data-fui-confirm on the submit button.
//
// [menu-action-scheme]
// Property: every <form action> sink allow-lists URL schemes.
// TestFormActionSinksRejectUnsafeURL loops SearchInput/FilterToolbar/SignOut/StepWizard;
// MenuAction.Path is the missed surface, and the SignOut.Action comment documents
// javascript: surviving render.Escape as exactly this bug class.
// Surfaces: framework/ui/menu.go::writeMenuItem (Action branch).
// Finding: the Action branch writes `action="render.Escape(it.Action.Path)"` —
// HTML-escaping and scheme-blind — so javascript:/vbscript:/data:/protocol-relative
// paths render as a live form action with a type=submit button.
// Fix direction: run the path through urlsafe.CleanAnchor like the Href branch
// (menu.go:450) and every other form-action sink, degrading rejects.
//
// [menu-confirm-dropped]
// Property: MenuItem.Confirm maps to data-fui-confirm, which the runtime honors on
// RPC dispatch AND on any form submit (field doc comment, menu.go:44-48; runtime.js
// submit handler; pinned e2e TestConfirmNativeFormCancelBlocks).
// Surfaces: framework/ui/menu.go::writeMenuItem (Action branch).
// Finding: the Action branch never reads it.Confirm, so a destructive form row
// submits with no confirmation despite the caller asking for one.
// Fix direction: emit data-fui-confirm on the submit button (or refuse the combo
// like Radio+Children); the assert below accepts either, demanding presence.

func TestMenuFormActionScheme(t *testing.T) {
	unsafe := []string{
		"javascript:alert(1)",
		"vbscript:msgbox(1)",
		"data:text/html,<script>alert(1)</script>",
		"//evil.com/x",
	}
	for _, action := range unsafe {
		t.Run(action, func(t *testing.T) {
			h := ui.Menu(ui.MenuConfig{
				Label: "m",
				Items: []ui.MenuItem{{Label: "stop", Action: &ui.MenuAction{Path: action, Unsafe: true}}},
			})
			if strings.Contains(string(h), action) {
				t.Errorf("SECURITY: [menu-action-scheme] Menu rendered unsafe MenuAction.Path %q verbatim as a live form action:\n%s", action, h)
			}
			if scheme := schemeOf(action); scheme != "" && strings.Contains(string(h), scheme+":") {
				t.Errorf("SECURITY: [menu-action-scheme] Menu kept dangerous scheme %q: in the form action (render.Escape is scheme-blind):\n%s", scheme, h)
			}
		})
	}
	// Control: the guard is a scheme allow-list, not a blanket reject —
	// a valid relative action must round-trip unchanged.
	h := ui.Menu(ui.MenuConfig{
		Label: "m",
		Items: []ui.MenuItem{{Label: "go", Action: &ui.MenuAction{Path: "/go", Unsafe: true}}},
	})
	if !strings.Contains(string(h), `action="/go"`) {
		t.Errorf("Menu dropped a valid relative MenuAction.Path:\n%s", h)
	}
}

func TestMenuConfirmOnActionRow(t *testing.T) {
	h := ui.Menu(ui.MenuConfig{
		Label: "m",
		Items: []ui.MenuItem{{Label: "del", Confirm: "Really delete?", Action: &ui.MenuAction{Path: "/go", Unsafe: true}}},
	})
	if !strings.Contains(string(h), `data-fui-confirm="Really delete?"`) {
		t.Errorf("SECURITY: [menu-confirm-dropped] MenuItem.Confirm is silently dropped on an Action row: the destructive form submits unconfirmed, though the field doc promises data-fui-confirm is honored on any form submit:\n%s", h)
	}
}
