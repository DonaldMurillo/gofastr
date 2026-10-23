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

// The preferences screen's heading and its save button.
var (
	pageSettingsTitle = i18n.Define("desktop.settings_title", "Settings")
	pageSettingsSave  = i18n.Define("desktop.settings_save", "Save")
)
