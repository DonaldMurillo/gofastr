package desktop

import (
	"context"
	"reflect"
	"testing"
)

// Group: the pure menu planning pass (menu_plan.go). These tests pin
// the mapping from the validated menu model to the platform-neutral
// render plan, key equivalents, modifier masks, role resolution, tag
// assignment, and the synthesized app/Edit menus, with no native
// toolkit in the process. Runs on every GOOS.

func TestKeyEquivalentSplitting(t *testing.T) {
	cases := []struct {
		key  string
		eq   string
		mask uintptr
	}{
		{"", "", 0},
		{"n", "n", 0},
		{"cmd+n", "n", nsModifierCommand},
		{"cmd+shift+t", "t", nsModifierCommand | nsModifierShift},
		{"ctrl+shift+t", "t", nsModifierControl | nsModifierShift},
		{"alt+option+o", "o", nsModifierOption}, // aliases OR to the same bit
		{"opt+q", "q", nsModifierOption},
		{"CMD+N", "n", nsModifierCommand}, // case-insensitive tokens
		{"cmd+ctrl+alt+shift+x", "x", nsModifierCommand | nsModifierControl | nsModifierOption | nsModifierShift},
	}
	for _, tc := range cases {
		eq, mask := keyEquivalent(tc.key)
		if eq != tc.eq || mask != tc.mask {
			t.Errorf("keyEquivalent(%q) = (%q, %#x), want (%q, %#x)", tc.key, eq, mask, tc.eq, tc.mask)
		}
	}
}

func TestPlanMenuBarSynthesizesAppAndEditMenus(t *testing.T) {
	plans, actions := PlanMenuBar("Notes", nil, false)
	if len(actions) != 0 {
		t.Fatalf("nil menu must produce no actions, got %v", actions)
	}
	if len(plans) != 2 {
		t.Fatalf("plans = %d menus, want 2 (app + Edit)", len(plans))
	}
	app := plans[0]
	if app.Title != "Notes" {
		t.Fatalf("app menu title = %q, want Notes", app.Title)
	}
	want := []MenuItemPlan{
		{Title: "About Notes", Role: RoleAbout},
		{Sep: true},
		{Title: "Quit Notes", Role: RoleQuit, Key: "q", Mask: nsModifierCommand},
	}
	if !reflect.DeepEqual(app.Items, want) {
		t.Fatalf("app menu items = %+v, want %+v", app.Items, want)
	}
	edit := plans[1]
	if edit.Title != "Edit" {
		t.Fatalf("edit menu title = %q", edit.Title)
	}
	// The Edit rows are first-responder targets: no roles, no actions,
	// standard equivalents.
	for _, r := range edit.Items {
		if r.Role != "" || r.Action {
			t.Fatalf("edit row %+v must be a plain first-responder row", r)
		}
	}
	var labels []string
	for _, r := range edit.Items {
		if !r.Sep {
			labels = append(labels, r.Title)
		}
	}
	if !reflect.DeepEqual(labels, []string{"Undo", "Redo", "Cut", "Copy", "Paste", "Select All"}) {
		t.Fatalf("edit labels = %v", labels)
	}
}

func TestPlanMenuBarUserMenusAndTags(t *testing.T) {
	menu := &Menu{Items: []MenuItem{
		{Title: "File", Children: []MenuItem{
			{Title: "New", Navigate: "/new", Key: "cmd+n"},
			{ID: "open", Title: "Open", Navigate: "/open"},
			{Role: RoleSeparator},
			{Title: "Bye", Role: RoleQuit},
		}},
		{Title: "Go", Children: []MenuItem{
			{Title: "Two", Navigate: "/two"},
			{Title: "Ping", Handler: func(context.Context) error { return nil }},
		}},
	}}
	validated, err := validateMenu(menu)
	if err != nil {
		t.Fatal(err)
	}
	plans, actions := PlanMenuBar("Notes", validated, false)

	if len(plans) != 4 {
		t.Fatalf("plans = %d menus, want 4 (app, File, Go, Edit)", len(plans))
	}
	// validateMenu assigns m1..m8 in declaration order (separators
	// included); the planner passes those IDs through as tags.
	if !reflect.DeepEqual(actions, []string{"m2", "open", "m7", "m8"}) {
		t.Fatalf("action ids = %v, want [m2 open m7 m8] (explicit ids pass through)", actions)
	}

	file := plans[1]
	if file.Title != "File" {
		t.Fatalf("second menu title = %q", file.Title)
	}
	// New (action, cmd+n), Open (action, no key), separator, Bye (quit role).
	if !reflect.DeepEqual(file.Items, []MenuItemPlan{
		{Title: "New", Key: "n", Mask: nsModifierCommand, Action: true, Tag: 0},
		{Title: "Open", Action: true, Tag: 1},
		{Sep: true},
		{Title: "Bye", Role: RoleQuit},
	}) {
		t.Fatalf("file rows = %+v", file.Items)
	}

	goMenu := plans[2]
	if len(goMenu.Items) != 2 || !goMenu.Items[0].Action || goMenu.Items[0].Tag != 2 || !goMenu.Items[1].Action || goMenu.Items[1].Tag != 3 {
		t.Fatalf("go menu rows = %+v", goMenu.Items)
	}
}

func TestPlanMenuBarNestedSubmenus(t *testing.T) {
	menu := &Menu{Items: []MenuItem{
		{Title: "File", Children: []MenuItem{
			{Title: "Export", Children: []MenuItem{
				{Title: "PDF", Navigate: "/export/pdf"},
				{Title: "Markdown", Navigate: "/export/md"},
			}},
		}},
	}}
	validated, err := validateMenu(menu)
	if err != nil {
		t.Fatal(err)
	}
	plans, actions := PlanMenuBar("Notes", validated, false)
	file := plans[1]
	if len(file.Items) != 1 || file.Items[0].Title != "Export" || file.Items[0].Action {
		t.Fatalf("export row = %+v", file.Items)
	}
	sub := file.Items[0].Submenu
	if len(sub) != 2 || !sub[0].Action || sub[0].Tag != 0 || !sub[1].Action || sub[1].Tag != 1 {
		t.Fatalf("submenu rows = %+v", sub)
	}
	if !reflect.DeepEqual(actions, []string{"m3", "m4"}) {
		t.Fatalf("action ids = %v, want [m3 m4] (File m1, Export m2, PDF m3, Markdown m4)", actions)
	}
}

func TestPlanMenuBarChildlessTopLevelActionStaysReachable(t *testing.T) {
	menu := &Menu{Items: []MenuItem{
		{Title: "Standalone", Navigate: "/solo"},
	}}
	validated, err := validateMenu(menu)
	if err != nil {
		t.Fatal(err)
	}
	plans, actions := PlanMenuBar("Notes", validated, false)
	solo := plans[1]
	if solo.Title != "Standalone" || len(solo.Items) != 1 {
		t.Fatalf("solo menu = %+v", solo)
	}
	row := solo.Items[0]
	if !row.Action || row.Tag != 0 || row.Title != "Standalone" {
		t.Fatalf("solo row = %+v", row)
	}
	if !reflect.DeepEqual(actions, []string{"m1"}) {
		t.Fatalf("actions = %v, want [m1]", actions)
	}
}

func TestPlanMenuBarRoleTitleDefaults(t *testing.T) {
	menu := &Menu{Items: []MenuItem{
		{Title: "File", Children: []MenuItem{{Role: RoleQuit}, {Role: RoleAbout}}},
	}}
	plans, _ := PlanMenuBar("Notes", menu, false)
	rows := plans[1].Items
	if rows[0].Title != "Quit Notes" || rows[0].Role != RoleQuit {
		t.Fatalf("quit default title = %+v", rows[0])
	}
	if rows[1].Title != "About Notes" || rows[1].Role != RoleAbout {
		t.Fatalf("about default title = %+v", rows[1])
	}
}

func TestPlanMenuBarSettingsRowOnlyWhenConfigured(t *testing.T) {
	// Without Settings: About, separator, Quit.
	plans, _ := PlanMenuBar("Notes", nil, false)
	app := plans[0]
	for _, r := range app.Items {
		if r.Role == RoleSettings {
			t.Fatalf("settings row without WindowConfig.Settings: %+v", app.Items)
		}
	}
	// With Settings: About, Settings… (cmd+,), separator, Quit.
	plans, _ = PlanMenuBar("Notes", nil, true)
	app = plans[0]
	want := []MenuItemPlan{
		{Title: "About Notes", Role: RoleAbout},
		{Title: "Settings…", Role: RoleSettings, Key: ",", Mask: nsModifierCommand},
		{Sep: true},
		{Title: "Quit Notes", Role: RoleQuit, Key: "q", Mask: nsModifierCommand},
	}
	if !reflect.DeepEqual(app.Items, want) {
		t.Fatalf("app menu items = %+v, want %+v", app.Items, want)
	}
}

func TestPlanTrayMenuSharesActionIDTable(t *testing.T) {
	menu := &Menu{Items: []MenuItem{
		{Title: "File", Children: []MenuItem{
			{Title: "New", Navigate: "/new", Key: "cmd+n"},
		}},
	}}
	tray := &Menu{Items: []MenuItem{
		{Title: "Show Notes", Role: RoleShow},
		{Title: "New", Navigate: "/new"},
		{Title: "Settings…", Role: RoleSettings},
		{Role: RoleSeparator},
		{Role: RoleQuit},
	}}
	// New validates BOTH trees in one pass, so ids are unique across
	// the menu bar and the tray (the shell dispatches both through one
	// action-id table).
	b := New(Config{ID: "tray.example.app", Menu: menu, Tray: &Tray{Menu: tray}})
	plans, actionIDs := PlanMenuBar("Notes", b.runMenu, true)
	trayRows := PlanTrayMenu(b.tray, "Notes", &actionIDs)

	if len(plans[1].Items) != 1 || !plans[1].Items[0].Action || plans[1].Items[0].Tag != 0 {
		t.Fatalf("File menu row lost its tag: %+v", plans[1].Items)
	}
	// The tray rows: show (role), new (action, tag 1: the NEXT index in
	// the shared table), settings (role), separator, quit (role).
	wantTray := []MenuItemPlan{
		{Title: "Show Notes", Role: RoleShow},
		{Title: "New", Action: true, Tag: 1},
		{Title: "Settings…", Role: RoleSettings},
		{Sep: true},
		{Title: "Quit Notes", Role: RoleQuit},
	}
	if !reflect.DeepEqual(trayRows, wantTray) {
		t.Fatalf("tray rows = %+v, want %+v", trayRows, wantTray)
	}
	// File=New is m2 (File m1, New m2), the tray's New is m4 (Show m3,
	// New m4): one table, unique ids, menu bar first.
	if !reflect.DeepEqual(actionIDs, []string{"m2", "m4"}) {
		t.Fatalf("shared action table = %v, want [m2 m4]", actionIDs)
	}
	// The battery's dispatch menu keeps both trees; the shell's menu is
	// the main rows only.
	if _, ok := b.menu.find("m4"); !ok {
		t.Fatal("tray item id missing from the battery's dispatch menu")
	}
	for _, it := range b.runMenu.Items {
		if it.ID == "m4" {
			t.Fatal("tray item leaked into the menu-bar menu")
		}
	}
}

func TestPlanRowsRoleDefaultsForSettingsAndShow(t *testing.T) {
	menu := &Menu{Items: []MenuItem{
		{Title: "Tray", Children: []MenuItem{
			{Role: RoleShow},
			{Role: RoleSettings},
		}},
	}}
	validated, err := validateMenu(menu)
	if err != nil {
		t.Fatal(err)
	}
	rows := planRows(validated.Items[0].Children, "Notes", new([]string))
	if rows[0].Title != "Show Notes" || rows[0].Role != RoleShow || rows[0].Action {
		t.Fatalf("show row = %+v", rows[0])
	}
	if rows[1].Title != "Settings…" || rows[1].Role != RoleSettings || rows[1].Action {
		t.Fatalf("settings row = %+v", rows[1])
	}
}
