package desktop

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
	// Items are the rows, in order.
	Items []MenuItemPlan
}

// MenuItemPlan is one row of one menu.
type MenuItemPlan struct {
	// Title is the displayed label; empty for separators.
	Title string
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
var editMenuRows = []MenuItemPlan{
	{Title: "Undo", Key: "z", Mask: nsModifierCommand, FirstResponder: "undo:"},
	{Title: "Redo", Key: "z", Mask: nsModifierCommand | nsModifierShift, FirstResponder: "redo:"},
	{Sep: true},
	{Title: "Cut", Key: "x", Mask: nsModifierCommand, FirstResponder: "cut:"},
	{Title: "Copy", Key: "c", Mask: nsModifierCommand, FirstResponder: "copy:"},
	{Title: "Paste", Key: "v", Mask: nsModifierCommand, FirstResponder: "paste:"},
	{Title: "Select All", Key: "a", Mask: nsModifierCommand, FirstResponder: "selectAll:"},
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
// quit.
func PlanMenuBar(appTitle string, m *Menu, hasSettings bool) (plans []MenuPlan, actionIDs []string) {
	appItems := []MenuItemPlan{{Title: "About " + appTitle, Role: RoleAbout}}
	if hasSettings {
		appItems = append(appItems, MenuItemPlan{Title: "Settings…", Role: RoleSettings, Key: ",", Mask: nsModifierCommand})
	}
	appItems = append(appItems,
		MenuItemPlan{Sep: true},
		MenuItemPlan{Title: "Quit " + appTitle, Role: RoleQuit, Key: "q", Mask: nsModifierCommand},
	)
	plans = append(plans, MenuPlan{Title: appTitle, Items: appItems})

	if m != nil {
		for _, it := range m.Items {
			if it.Role == RoleSeparator {
				// A separator between menu-bar titles is not a thing;
				// drop it rather than render an empty untitled menu.
				continue
			}
			mp := MenuPlan{Title: defaultRoleTitle(it, appTitle)}
			if len(it.Children) > 0 {
				mp.Items = planRows(it.Children, appTitle, &actionIDs)
			} else {
				// A childless top-level item becomes its own
				// single-row menu so its action stays reachable.
				mp.Items = planRows([]MenuItem{it}, appTitle, &actionIDs)
			}
			plans = append(plans, mp)
		}
	}

	plans = append(plans, MenuPlan{Title: "Edit", Items: editMenuRows})
	return plans, actionIDs
}

// planRows maps menu items to planned rows, assigning action tags.
func planRows(items []MenuItem, appTitle string, actionIDs *[]string) []MenuItemPlan {
	rows := make([]MenuItemPlan, 0, len(items))
	for _, it := range items {
		if it.Role == RoleSeparator {
			rows = append(rows, MenuItemPlan{Sep: true})
			continue
		}
		eq, mask := keyEquivalent(it.Key)
		row := MenuItemPlan{Title: it.Title, Key: eq, Mask: mask, Role: it.Role}
		switch {
		case it.Role == RoleQuit || it.Role == RoleAbout || it.Role == RoleSettings || it.Role == RoleShow:
			row.Title = defaultRoleTitle(it, appTitle)
		case it.Navigate != "" || it.Handler != nil:
			row.Action = true
			row.Tag = len(*actionIDs)
			*actionIDs = append(*actionIDs, it.ID)
		}
		if len(it.Children) > 0 {
			row.Submenu = planRows(it.Children, appTitle, actionIDs)
		}
		rows = append(rows, row)
	}
	return rows
}

// defaultRoleTitle fills the OS-standard label for a role item whose
// Title is empty.
func defaultRoleTitle(it MenuItem, appTitle string) string {
	if it.Title != "" {
		return it.Title
	}
	switch it.Role {
	case RoleQuit:
		return "Quit " + appTitle
	case RoleAbout:
		return "About " + appTitle
	case RoleSettings:
		return "Settings…"
	case RoleShow:
		return "Show " + appTitle
	}
	return it.Title
}

// PlanTrayMenu plans the tray menu's rows for a platform shell. It
// shares the caller's action-ID table with the menu bar plan, so a
// tray Navigate/Handler row's Tag resolves through the same table
// OnMenu dispatches on.
func PlanTrayMenu(t *Tray, appTitle string, actionIDs *[]string) []MenuItemPlan {
	if t == nil || t.Menu == nil {
		return nil
	}
	return planRows(t.Menu.Items, appTitle, actionIDs)
}
