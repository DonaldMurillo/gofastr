//go:build linux && (amd64 || arm64)

package linux

import "github.com/DonaldMurillo/gofastr/core/i18n"

var (
	dialogOpenFile   = i18n.Define("desktop.linux_open_file", "Open file")
	dialogSaveFile   = i18n.Define("desktop.linux_save_file", "Save file")
	dialogOpenFolder = i18n.Define("desktop.linux_open_folder", "Open folder")
	dialogOpen       = i18n.Define("desktop.linux_open", "Open")
	dialogSave       = i18n.Define("desktop.linux_save", "Save")
	dialogSelect     = i18n.Define("desktop.linux_select", "Select")
	dialogCancel     = i18n.Define("desktop.linux_cancel", "Cancel")
)
