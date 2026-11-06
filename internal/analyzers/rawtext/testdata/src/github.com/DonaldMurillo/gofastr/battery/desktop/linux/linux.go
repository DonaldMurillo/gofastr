// Package linux mirrors the Linux shell's withString/capsSetString
// wrappers (shell_linux.go / caps_linux.go): the two package-local
// helpers every OS-visible string goes through on Linux — menu rows via
// gtk_menu_item_new_with_label and window titles via
// gtk_window_set_title.
package linux

func withString(s string, fn func(uintptr)) {
	_ = s
	fn(0)
}

func capsSetString(fn, object uintptr, value string) {
	_ = fn
	_ = object
	_ = value
}

func menus(planTitle, localizedTitle string) {
	withString(planTitle, func(p uintptr) {}) // the caller localized it
	withString(localizedTitle, func(p uintptr) {})
	withString("Recent plans", func(p uintptr) {}) // want `withString\("Recent plans"\) hands English to the OS`
	capsSetString(0, 0, localizedTitle)
	capsSetString(0, 0, "Settings") // want `capsSetString\("Settings"\) hands English to the OS`
	// Atom and script-message names, not words for a person.
	withString("CARDINAL", func(p uintptr) {})
	withString("gofastr", func(p uintptr) {})
}
