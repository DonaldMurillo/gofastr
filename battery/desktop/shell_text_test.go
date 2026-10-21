package desktop

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
)

func frShellCtx() context.Context {
	cat := i18n.NewMapCatalog()
	for k, v := range map[string]string{
		"desktop.menu_about":           "À propos de {{app}}",
		"desktop.menu_settings":        "Réglages…",
		"desktop.menu_quit":            "Quitter {{app}}",
		"desktop.menu_show":            "Afficher {{app}}",
		"desktop.menu_edit":            "Édition",
		"desktop.menu_undo":            "Annuler",
		"desktop.menu_select_all":      "Tout sélectionner",
		"desktop.permission_asks":      "{{capability}}.{{method}} demande la permission {{permission}}",
		"desktop.permission_allow":     "Autoriser",
		"desktop.permission_deny":      "Refuser",
		"desktop.permission_once":      "Autoriser une fois",
		"desktop.permission_title":     "Permission requise",
		"desktop.permission_use":       "Autoriser cette application à utiliser {{capability}}.",
		"desktop.permission_choose":    "Choisissez.",
		"desktop.settings_title":       "Réglages",
		"desktop.settings_description": "Préférences de cette installation",
	} {
		cat.Set("fr", k, i18n.Message{Text: v})
	}
	ctx := i18n.WithTranslator(context.Background(), i18n.NewTranslator(cat, "en"))
	return i18n.WithContext(ctx, i18n.Locale{Tag: "fr"})
}

// The shell-owned menus speak the locale the shell resolved: the app
// menu's role items, the Edit menu and its rows.
func TestPlanMenuBarSpeaksLocale(t *testing.T) {
	m := &Menu{Items: []MenuItem{{Title: testPhrase("Fichier"), Children: []MenuItem{{Role: RoleQuit}, {Role: RoleShow}}}}}
	plans, _ := PlanMenuBar(frShellCtx(), "Notes", m, true)
	app := plans[0]
	if got := app.Items[0].Title; got != "À propos de Notes" {
		t.Errorf("About = %q", got)
	}
	if got := app.Items[1].Title; got != "Réglages…" {
		t.Errorf("Settings = %q", got)
	}
	if got := app.Items[3].Title; got != "Quitter Notes" {
		t.Errorf("Quit = %q", got)
	}
	file := plans[1]
	if file.Items[0].Title != "Quitter Notes" || file.Items[1].Title != "Afficher Notes" {
		t.Errorf("role rows = %q, %q", file.Items[0].Title, file.Items[1].Title)
	}
	edit := plans[len(plans)-1]
	if edit.Title != "Édition" || edit.Items[0].Title != "Annuler" || edit.Items[len(edit.Items)-1].Title != "Tout sélectionner" {
		t.Errorf("Edit menu = %q / %q / %q", edit.Title, edit.Items[0].Title, edit.Items[len(edit.Items)-1].Title)
	}
	// The shared row table is not rewritten by a localized plan.
	if editMenuRows[0].Title != "" {
		t.Errorf("editMenuRows carries a title %q; rows take their words per plan", editMenuRows[0].Title)
	}
}

// A French plan still names every row in English, the name the native
// drivers match, so a desktoptest suite passes on any OS language.
func TestPlanKeepsEnglishNames(t *testing.T) {
	file := testPhrase("File")
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "desktop.menu_quit", i18n.Message{Text: "Quitter {{app}}"})
	cat.Set("fr", file.Key(), i18n.Message{Text: "Fichier"})
	ctx := i18n.WithContext(i18n.WithTranslator(context.Background(), i18n.NewTranslator(cat, "en")), i18n.Locale{Tag: "fr"})

	m := &Menu{Items: []MenuItem{{Title: file, Children: []MenuItem{{Title: file, Navigate: "/f"}, {Role: RoleQuit}}}}}
	plans, ids := PlanMenuBar(ctx, "Notes", m, false)
	if got := plans[1]; got.Title != "Fichier" || got.English != "File" {
		t.Errorf("menu = %q / %q, want Fichier / File", got.Title, got.English)
	}
	rows := plans[1].Items
	if rows[0].Title != "Fichier" || rows[0].English != "File" || rows[1].Title != "Quitter Notes" || rows[1].English != "Quit Notes" {
		t.Errorf("rows = %+v", rows)
	}
	if plans[0].Items[len(plans[0].Items)-1].English != "Quit Notes" {
		t.Errorf("app menu Quit English = %q", plans[0].Items[len(plans[0].Items)-1].English)
	}

	tray := PlanTrayMenu(ctx, &Tray{Menu: &Menu{Items: []MenuItem{{Title: file, Navigate: "/t"}}}}, "Notes", &ids)
	if tray[0].Title != "Fichier" || tray[0].English != "File" {
		t.Errorf("tray row = %q / %q", tray[0].Title, tray[0].English)
	}
	// The English pass does not take tags of its own.
	if len(ids) != 2 || tray[0].Tag != 1 {
		t.Errorf("action ids = %v, tray tag %d", ids, tray[0].Tag)
	}
}

// A shell's preferred languages pick the catalog locale; none that
// matches falls back to the environment.
func TestShellContextPicksPreferredLanguage(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "k.k", i18n.Message{Text: "v"})
	cat.Set("de", "k.k", i18n.Message{Text: "v"})
	prev := i18n.Default()
	i18n.SetDefault(i18n.NewTranslator(cat, "en"))
	defer i18n.SetDefault(prev)
	t.Setenv("LANGUAGE", "")
	t.Setenv("LC_ALL", "de_DE.UTF-8")

	if got := i18n.FromContext(ShellContext(context.Background(), []string{"pt-BR", "fr-CA"})).Tag; got != "fr" {
		t.Errorf("preferred [pt-BR fr-CA] = %q; want fr", got)
	}
	if got := i18n.FromContext(ShellContext(context.Background(), []string{"pt-BR"})).Tag; got != "de" {
		t.Errorf("unmatched preferred = %q; want the environment's de", got)
	}
}

func TestPromptTextSpeaksLocale(t *testing.T) {
	w := PromptText(frShellCtx(), PermissionRequest{Capability: "fs", Method: "read", Permission: "fs.read"})
	if w.Message != "fs.read demande la permission fs.read" {
		t.Errorf("Message = %q", w.Message)
	}
	if w.Allow != "Autoriser" || w.AllowOnce != "Autoriser une fois" || w.Deny != "Refuser" || w.Title != "Permission requise" {
		t.Errorf("buttons = %+v", w)
	}
	if w.Use != "Autoriser cette application à utiliser fs." {
		t.Errorf("Use = %q", w.Use)
	}
}

// A settings window opened from a French shell is titled in French, and
// the preferences page names itself in the reader's language.
func TestSettingsTitleSpeaksLocale(t *testing.T) {
	_, shell := newTestBattery(t)
	b := New(Config{
		ID:       "settings.example.app",
		Title:    "Notes",
		Shell:    shell,
		Logger:   testLogger(t),
		Settings: &WindowSpec{Path: "/settings"},
	})
	b.grants = newMemGrantStore()
	markReadyForWindows(b, &fakeWindow{id: "main"})
	if _, err := b.OpenSettings(frShellCtx()); err != nil {
		t.Fatal(err)
	}
	if got := shell.openWindowCallsList()[0].spec.Title; got != "Réglages" {
		t.Errorf("settings window title = %q, want Réglages", got)
	}
	s := &preferencesScreen{b: b}
	if got := s.ScreenTitle(frShellCtx()); got != "Réglages" {
		t.Errorf("ScreenTitle = %q", got)
	}
	if got := s.ScreenDescription(frShellCtx()); got != "Préférences de cette installation" {
		t.Errorf("ScreenDescription = %q", got)
	}
}
