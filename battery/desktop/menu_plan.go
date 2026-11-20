package desktop

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/asciicase"
)

// MenuPlan is one top-level menu resolved for native rendering.
type MenuPlan struct {
	// Title is the menu-bar title (the app name, "File", "Edit").
	Title string
	// English is Title in English, the name a native test driver matches.
	English string
	// Items are the rows, in order.
	Items []MenuItemPlan
}

// MenuItemPlan is one row of one menu.
type MenuItemPlan struct {
	// Title is the displayed label; empty for separators.
	Title string
	// English is Title in English, the name a native test driver matches.
	English string
	// Key is the key-equivalent glyph (the last +-token of the accelerator); "" when the item has none.
	Key string
	// Mask is the modifier mask for Key (darwin NSEventModifierFlags).
	Mask uintptr
	// Sep marks a separator row (no title, no action).
	Sep bool
	// Role is the role the shell implements itself; "" for action items and labels.
	Role string
	// Action marks a Navigate or Handler row; Tag then indexes the plan's action-ID table.
	Action bool
	Tag    int
	// FirstResponder is a selector sent to the responder chain with no target.
	FirstResponder string
	// Submenu holds nested rows; nil for leaf rows.
	Submenu []MenuItemPlan
}

const (
	nsModifierCommand = 1 << 20
	nsModifierShift   = 1 << 17
	nsModifierOption  = 1 << 19
	nsModifierControl = 1 << 18
)

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

func splitMenuKey(key string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(key); i++ {
		if i == len(key) || key[i] == '+' {
			parts = append(parts, asciicase.Lower(key[start:i]))
			start = i + 1
		}
	}
	return parts
}

var editMenuRows = []MenuItemPlan{
	{Key: "z", Mask: nsModifierCommand, FirstResponder: "undo:"},
	{Key: "z", Mask: nsModifierCommand | nsModifierShift, FirstResponder: "redo:"},
	{Sep: true},
	{Key: "x", Mask: nsModifierCommand, FirstResponder: "cut:"},
	{Key: "c", Mask: nsModifierCommand, FirstResponder: "copy:"},
	{Key: "v", Mask: nsModifierCommand, FirstResponder: "paste:"},
	{Key: "a", Mask: nsModifierCommand, FirstResponder: "selectAll:"},
}

var editMenuTitles = map[string]i18n.Phrase{
	"undo:":      menuUndo,
	"redo:":      menuRedo,
	"cut:":       menuCut,
	"copy:":      menuCopy,
	"paste:":     menuPaste,
	"selectAll:": menuSelectAll,
}

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

// PlanMenuBar plans the app menu, m's menus and the Edit menu, and returns the action-ID table their Tags index.
func PlanMenuBar(ctx context.Context, appTitle string, m *Menu, hasSettings bool) (plans []MenuPlan, actionIDs []string) {
	plans, actionIDs = planMenuBar(ctx, appTitle, m, hasSettings)
	en, _ := planMenuBar(englishContext, appTitle, m, hasSettings)
	for i := range plans {
		plans[i].English = en[i].Title
		withEnglish(plans[i].Items, en[i].Items)
	}
	return plans, actionIDs
}

var englishContext = i18n.WithTranslator(context.Background(), i18n.NewTranslator(i18n.NewMapCatalog(), "en"))

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
				continue
			}
			mp := MenuPlan{Title: defaultRoleTitle(ctx, it, appTitle)}
			if len(it.Children) > 0 {
				mp.Items = planRows(ctx, it.Children, appTitle, &actionIDs)
			} else {
				mp.Items = planRows(ctx, []MenuItem{it}, appTitle, &actionIDs)
			}
			plans = append(plans, mp)
		}
	}

	plans = append(plans, editMenu(ctx))
	return plans, actionIDs
}

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

func itemTitle(ctx context.Context, it MenuItem) string {
	if it.Title.IsZero() {
		return ""
	}
	return it.Title.Text(ctx)
}

// PlanTrayMenu plans the tray menu's rows for a platform shell.
func PlanTrayMenu(ctx context.Context, t *Tray, appTitle string, actionIDs *[]string) []MenuItemPlan {
	if t == nil {
		return nil
	}
	var items []MenuItem
	if t.Menu != nil {
		items = t.Menu.Items
	}
	rows := planRows(ctx, items, appTitle, actionIDs)
	scratch := append([]string(nil), (*actionIDs)...)
	withEnglish(rows, planRows(englishContext, items, appTitle, &scratch))
	if t.CloseHidesWindow && !trayHasShow(rows) {
		show := MenuItem{Role: RoleShow}
		rows = append([]MenuItemPlan{{Role: RoleShow, Title: defaultRoleTitle(ctx, show, appTitle), English: defaultRoleTitle(englishContext, show, appTitle)}}, rows...)
	}
	return rows
}

func trayHasShow(rows []MenuItemPlan) bool {
	for _, row := range rows {
		if row.Role == RoleShow || trayHasShow(row.Submenu) {
			return true
		}
	}
	return false
}
