package main

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The shell's own text: menu rows, the tray tooltip and the settings
// labels. The shell renders them in the OS language, so a catalog
// entry for a key translates the native menu with no code change.
var (
	menuFile         = i18n.Define("notes.menu.file", "File")
	menuShow         = i18n.Define("notes.menu.show", "Show Notes")
	menuNewNote      = i18n.Define("notes.menu.new_note", "New note")
	menuQuickNote    = i18n.Define("notes.menu.quick_note", "Quick note…")
	menuSettings     = i18n.Define("notes.menu.settings", "Settings…")
	menuExportAll    = i18n.Define("notes.menu.export_all", "Export all…")
	menuCheckUpdates = i18n.Define("notes.menu.check_updates", "Check for updates…")
	trayTooltip      = i18n.Define("notes.tray.tooltip", "Notes")
	prefNotifyOnSave = i18n.Define("notes.pref.notify_on_save", "Notify on save")
	prefExportFolder = i18n.Define("notes.pref.export_folder", "Export folder")
)
