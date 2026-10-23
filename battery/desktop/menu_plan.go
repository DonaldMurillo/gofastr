package desktop

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// The pure menu planning pass: WindowConfig.Menu (plus the standard
// app and Edit menus) resolved into a platform-neutral description a
// native shell renders with its own toolkit calls. Keeping the mapping
// here, key-equivalent splitting, modifier masks, role resolution,
// action-tag assignment, means it is unit-testable on every GOOS
// with no AppKit, GTK, or Win32 in the process (the same fake-shell
// posture as the rest of the battery's tests).
//
// The macos package (battery/desktop/macos) renders a MenuPlan with
// NSMenu/NSMenuItem. The masks carry darwin's NSEventModifierFlag
// values, the one platform-specific datum, spelled out per constant so
// a future windows/linux renderer reuses everything but the mask bits.
// MenuPlan, MenuItemPlan, PlanMenuBar, and PlanTrayMenu are exported
// for exactly those platform packages; everything else here stays
// internal to the planning pass.

// MenuPlan is one top-level menu resolved for native rendering.
type MenuPlan struct {
	// Title is the menu-bar title (the app name, "File", "Edit").
	Title string
	// English is Title in English: the name a native test driver
	// matches (desktoptest's ClickMenu), whatever language the shell
	// renders.
	English string
	// Items are the rows, in order.
	Items []MenuItemPlan
}

// MenuItemPlan is one row of one menu.
type MenuItemPlan struct {
	// Title is the displayed label; empty for separators.
	Title string
	// English is Title in English, the name a native test driver
	// matches.
	English string
	// Key is the key-equivalent glyph (the last +-token of the
	// accelerator); "" when the item has none.
	Key string
	// Mask is the modifier mask for Key (darwin
	// NSEventModifierFlags). Zero with a non-empty Key is a bare
	// key equivalent; zero with an empty Key means no equivalent.
	Mask uintptr
	// Sep marks a separator row (no title, no action).
	Sep bool
	// Role is the shell-native role (RoleQuit, RoleAbout) the shell
	// implements itself; "" for action items and plain labels.
	Role string
	// Action is true for Navigate/Handler items the shell wires to
	// its action selector; Tag is then the index into the plan's
	// action-ID table (setTag: on darwin).
	Action bool
	Tag    int
	// FirstResponder is a selector routed to the responder chain with
	// no target (the Edit menu's cut:/paste:/…: the WebView's editor
	// answers). Mutually exclusive with Role and Action.
	FirstResponder string
	// Submenu holds nested rows; nil for leaf rows.
	Submenu []MenuItemPlan
}

// NSEventModifierFlags (AppKit), used as MenuItemPlan.Mask values.
const (
	nsModifierCommand = 1 << 20
	nsModifierShift   = 1 << 17
	nsModifierOption  = 1 << 19
	nsModifierControl = 1 << 18
)

// keyEquivalent splits a validated accelerator ("cmd+shift+t") into
// the key-equivalent glyph (last token) and the darwin modifier mask
// (the remaining tokens). The menu validator already enforced the
// grammar; unknown modifier tokens contribute nothing (defensive: the
// planner is also reached by hand-built test menus).
func keyEquivalent(key string) (string, uintptr) {
	if key == "" {
		return "", 0
	}
	parts := splitMenuKey(key)
	eq := parts[len(parts)-1]
	var mask uintptr
	for _, p := range parts[:len(parts)-1] {
		switch p {
		case "cmd", "command", "meta":
			mask |= nsModifierCommand
		case "shift":
			mask |= nsModifierShift
		case "alt", "option", "opt":
			mask |= nsModifierOption
		case "ctrl", "control":
			mask |= nsModifierControl
		}
	}
	return eq, mask
}

// splitMenuKey lower-cases and splits a validated accelerator on '+'.
func splitMenuKey(key string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(key); i++ {
		if i == len(key) || key[i] == '+' {
			parts = append(parts, lowerASCII(key[start:i]))
			start = i + 1
		}
	}
	return parts
}

// lowerASCII lower-cases A-Z only (accelerators are validated to ASCII
// letters and digits).
func lowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// editMenuRows are the standard text-editing commands every WebView
// app needs inside the page. They target the first responder (the
// WKWebView's internal editor), not the bridge.
// Their titles come from editMenuTitles, in the plan's locale.
var editMenuRows = []MenuItemPlan{
	{Key: "z", Mask: nsModifierCommand, FirstResponder: "undo:"},
	{Key: "z", Mask: nsModifierCommand | nsModifierShift, FirstResponder: "redo:"},
	{Sep: true},
	{Key: "x", Mask: nsModifierCommand, FirstResponder: "cut:"},
	{Key: "c", Mask: nsModifierCommand, FirstResponder: "copy:"},
	{Key: "v", Mask: nsModifierCommand, FirstResponder: "paste:"},
	{Key: "a", Mask: nsModifierCommand, FirstResponder: "selectAll:"},
}

// editMenuTitles names each editMenuRows row by its FirstResponder.
var editMenuTitles = map[string]i18n.Phrase{
	"undo:":      menuUndo,
	"redo:":      menuRedo,
	"cut:":       menuCut,
	"copy:":      menuCopy,
	"paste:":     menuPaste,
	"selectAll:": menuSelectAll,
}

// editMenu is the Edit menu in ctx's locale.
func editMenu(ctx context.Context) MenuPlan {
	rows := make([]MenuItemPlan, len(editMenuRows))
	for i, row := range editMenuRows {
		if !row.Sep {
			row.Title = editMenuTitles[row.FirstResponder].Text(ctx)
		}
		rows[i] = row
	}
	return MenuPlan{Title: menuEdit.Text(ctx), Items: rows}
}

// PlanMenuBar resolves a validated Menu plus the shell-owned standard
// menus into the render plan a platform shell renders natively: the
// app menu (first, holding the standard Quit item and, when
// hasSettings, the Settings item), the caller's menus, and the Edit
// menu (last). actionIDs is the tag
// table: an action row's Tag indexes into it and yields the
// MenuItem.ID OnMenu receives.
//
// appTitle names the app menu and its Quit item ("Quit <Title>"). A
// nil Menu still yields a working bar (app + Edit): a macOS app
// without a menu bar has no keyboard escape and no standard way to
// quit. The shell-owned words (the app menu's items, role items'
// default titles, the Edit menu) are in ctx's locale: see
// [ShellContext]. Every menu and row also carries its English, the
// name a native test driver matches.
func PlanMenuBar(ctx context.Context, appTitle string, m *Menu, hasSettings bool) (plans []MenuPlan, actionIDs []string) {
	plans, actionIDs = planMenuBar(ctx, appTitle, m, hasSettings)
	// planning twice keeps the two trees the same shape by construction.
	en, _ := planMenuBar(englishContext, appTitle, m, hasSettings)
	for i := range plans {
		plans[i].English = en[i].Title
		withEnglish(plans[i].Items, en[i].Items)
	}
	return plans, actionIDs
}

// englishContext renders every phrase in its own English. A bare
// context is not that: a phrase then asks the process's default
// translator, whose fallback locale need not be English.
var englishContext = i18n.WithTranslator(context.Background(), i18n.NewTranslator(i18n.NewMapCatalog(), "en"))

// withEnglish copies en's titles into rows' English fields, row for
// row; both come from planRows over the same items.
func withEnglish(rows, en []MenuItemPlan) {
	for i := range rows {
		rows[i].English = en[i].Title
		withEnglish(rows[i].Submenu, en[i].Submenu)
	}
}

func planMenuBar(ctx context.Context, appTitle string, m *Menu, hasSettings bool) (plans []MenuPlan, actionIDs []string) {
	app := map[string]any{"app": appTitle}
	appItems := []MenuItemPlan{{Title: menuAbout.Text(ctx, app), Role: RoleAbout}}
	if hasSettings {
		appItems = append(appItems, MenuItemPlan{Title: menuSettings.Text(ctx), Role: RoleSettings, Key: ",", Mask: nsModifierCommand})
	}
	appItems = append(appItems,
		MenuItemPlan{Sep: true},
		MenuItemPlan{Title: menuQuit.Text(ctx, app), Role: RoleQuit, Key: "q", Mask: nsModifierCommand},
	)
	plans = append(plans, MenuPlan{Title: appTitle, Items: appItems})

	if m != nil {
		for _, it := range m.Items {
			if it.Role == RoleSeparator {
				// A separator between menu-bar titles is not a thing;
				// drop it rather than render an empty untitled menu.
				continue
			}
			mp := MenuPlan{Title: defaultRoleTitle(ctx, it, appTitle)}
			if len(it.Children) > 0 {
				mp.Items = planRows(ctx, it.Children, appTitle, &actionIDs)
			} else {
				// A childless top-level item becomes its own
				// single-row menu so its action stays reachable.
				mp.Items = planRows(ctx, []MenuItem{it}, appTitle, &actionIDs)
			}
			plans = append(plans, mp)
		}
	}

	plans = append(plans, editMenu(ctx))
	return plans, actionIDs
}

// planRows maps menu items to planned rows, assigning action tags.
func planRows(ctx context.Context, items []MenuItem, appTitle string, actionIDs *[]string) []MenuItemPlan {
	rows := make([]MenuItemPlan, 0, len(items))
	for _, it := range items {
		if it.Role == RoleSeparator {
			rows = append(rows, MenuItemPlan{Sep: true})
			continue
		}
		eq, mask := keyEquivalent(it.Key)
		row := MenuItemPlan{Title: itemTitle(ctx, it), Key: eq, Mask: mask, Role: it.Role}
		switch {
		case it.Role == RoleQuit || it.Role == RoleAbout || it.Role == RoleSettings || it.Role == RoleShow:
			row.Title = defaultRoleTitle(ctx, it, appTitle)
		case it.Navigate != "" || it.Handler != nil:
			row.Action = true
			row.Tag = len(*actionIDs)
			*actionIDs = append(*actionIDs, it.ID)
		}
		if len(it.Children) > 0 {
			row.Submenu = planRows(ctx, it.Children, appTitle, actionIDs)
		}
		rows = append(rows, row)
	}
	return rows
}

// defaultRoleTitle fills the OS-standard label, in ctx's locale, for a
// role item whose Title is empty.
func defaultRoleTitle(ctx context.Context, it MenuItem, appTitle string) string {
	if !it.Title.IsZero() {
		return it.Title.Text(ctx)
	}
	app := map[string]any{"app": appTitle}
	switch it.Role {
	case RoleQuit:
		return menuQuit.Text(ctx, app)
	case RoleAbout:
		return menuAbout.Text(ctx, app)
	case RoleSettings:
		return menuSettings.Text(ctx)
	case RoleShow:
		return menuShow.Text(ctx, app)
	}
	return ""
}

// itemTitle is an item's label in ctx's locale, "" when it has none.
func itemTitle(ctx context.Context, it MenuItem) string {
	if it.Title.IsZero() {
		return ""
	}
	return it.Title.Text(ctx)
}

// PlanTrayMenu plans the tray menu's rows for a platform shell. It
// shares the caller's action-ID table with the menu bar plan, so a
// tray Navigate/Handler row's Tag resolves through the same table
// OnMenu dispatches on.
func PlanTrayMenu(ctx context.Context, t *Tray, appTitle string, actionIDs *[]string) []MenuItemPlan {
	if t == nil || t.Menu == nil {
		return nil
	}
	rows := planRows(ctx, t.Menu.Items, appTitle, actionIDs)
	scratch := append([]string(nil), (*actionIDs)...)
	withEnglish(rows, planRows(englishContext, t.Menu.Items, appTitle, &scratch))
	return rows
}
