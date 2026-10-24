package desktop

import "github.com/DonaldMurillo/gofastr/core/i18n"

// The refusals the preferences screen writes, under their catalog keys.
var (
	reasonPrefNull        = i18n.Define("desktop.pref_null", "must not be null")
	reasonPrefNotBool     = i18n.Define("desktop.pref_not_bool", "must be true or false")
	reasonPrefNotInt      = i18n.Define("desktop.pref_not_int", "must be a whole number")
	reasonPrefNotString   = i18n.Define("desktop.pref_not_string", "must be a string")
	reasonPrefNotChoice   = i18n.Define("desktop.pref_not_choice", "must be one of: {{choices}}")
	reasonPrefUnknownKind = i18n.Define("desktop.pref_unknown_kind", "unknown kind {{kind}}")
	reasonPrefTooSmall    = i18n.Define("desktop.pref_too_small", "must be at least {{min}}")
	reasonPrefTooLarge    = i18n.Define("desktop.pref_too_large", "must be at most {{max}}")
	reasonPrefUndeclared  = i18n.Define("desktop.pref_undeclared", "not a declared preference")
	reasonPrefBadShape    = i18n.Define("desktop.pref_bad_shape", "must be a bool, a whole number, or a string")
	reasonPrefsNeedHost   = i18n.Define("desktop.prefs_need_host", "preferences need the desktop host: the app state store is not open")
	reasonPrefsNotObject  = i18n.Define("desktop.prefs_not_object", "request body must be a JSON object")
	reasonPrefsInvalid    = i18n.Define("desktop.prefs_invalid", "invalid preferences: {{fields}}")
	reasonPrefsSaveFailed = i18n.Define("desktop.prefs_save_failed", "saving preferences failed")
)

// The preferences screen's heading, description and save button.
var (
	pageSettingsTitle       = i18n.Define("desktop.settings_title", "Settings")
	pageSettingsDescription = i18n.Define("desktop.settings_description", "Preferences for this installation")
	pageSettingsSave        = i18n.Define("desktop.settings_save", "Save")
)

// A number preference's range hint on the preferences screen.
var (
	hintPrefBetween = i18n.Define("desktop.pref_between", "Between {{min}} and {{max}}.")
	hintPrefAtLeast = i18n.Define("desktop.pref_at_least", "At least {{min}}.")
	hintPrefAtMost  = i18n.Define("desktop.pref_at_most", "At most {{max}}.")
)

// The shell's own words: the menus it adds, the role items' default
// titles, the permission prompt and the Windows About box.
var (
	menuAbout     = i18n.Define("desktop.menu_about", "About {{app}}")
	menuSettings  = i18n.Define("desktop.menu_settings", "Settings…")
	menuQuit      = i18n.Define("desktop.menu_quit", "Quit {{app}}")
	menuShow      = i18n.Define("desktop.menu_show", "Show {{app}}")
	menuEdit      = i18n.Define("desktop.menu_edit", "Edit")
	menuUndo      = i18n.Define("desktop.menu_undo", "Undo")
	menuRedo      = i18n.Define("desktop.menu_redo", "Redo")
	menuCut       = i18n.Define("desktop.menu_cut", "Cut")
	menuCopy      = i18n.Define("desktop.menu_copy", "Copy")
	menuPaste     = i18n.Define("desktop.menu_paste", "Paste")
	menuSelectAll = i18n.Define("desktop.menu_select_all", "Select All")

	permissionAsks   = i18n.Define("desktop.permission_asks", "{{capability}}.{{method}} wants permission {{permission}}")
	permissionTitle  = i18n.Define("desktop.permission_title", "Permission required")
	permissionUse    = i18n.Define("desktop.permission_use", "Allow this app to use {{capability}}.")
	permissionChoose = i18n.Define("desktop.permission_choose", "Choose Allow to grant access, Allow once for this request, or Deny.")
	permissionYesNo  = i18n.Define("desktop.permission_yes_no", "Select Yes to Allow, No to Allow once, or Cancel to Deny.")
	permissionAllow  = i18n.Define("desktop.permission_allow", "Allow")
	permissionOnce   = i18n.Define("desktop.permission_once", "Allow once")
	permissionDeny   = i18n.Define("desktop.permission_deny", "Deny")

	aboutTitle = i18n.Define("desktop.about_title", "About")
	aboutBody  = i18n.Define("desktop.about_body", "A desktop application built with gofastr.")

	acceleratorCtrl  = i18n.Define("desktop.accelerator_ctrl", "Ctrl")
	acceleratorAlt   = i18n.Define("desktop.accelerator_alt", "Alt")
	acceleratorShift = i18n.Define("desktop.accelerator_shift", "Shift")
)
