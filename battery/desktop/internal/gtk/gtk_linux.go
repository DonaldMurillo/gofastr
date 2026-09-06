//go:build linux && (amd64 || arm64)

package gtk

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// The shared libraries the Linux host binds to, and the distro packages
// that carry them. GTK 3 and WebKitGTK 4.1 are the targets named in
// docs/desktop-plan.md: 4.1 is what Ubuntu 22.04 onward, Debian 12 and
// Tauri ship against, and webkitgtk-6.0 (GTK 4) is a later probe.
//
// Optional libraries are the ones a host can do without: notifications
// degrade to nothing without libnotify, and a snapshot needs gdk-pixbuf
// only to turn the cairo surface into PNG bytes. A missing optional
// library leaves its symbols zero and Load still succeeds.
var (
	libGLib    = &library{soname: "libglib-2.0.so.0", packages: "libglib2.0-0 (Debian/Ubuntu), glib2 (Fedora)"}
	libGObject = &library{soname: "libgobject-2.0.so.0", packages: "libglib2.0-0 (Debian/Ubuntu), glib2 (Fedora)"}
	libGdk     = &library{soname: "libgdk-3.so.0", packages: "libgtk-3-0 (Debian/Ubuntu), gtk3 (Fedora)"}
	libGtk     = &library{soname: "libgtk-3.so.0", packages: "libgtk-3-0 (Debian/Ubuntu), gtk3 (Fedora)"}
	libWebKit  = &library{soname: "libwebkit2gtk-4.1.so.0", packages: "libwebkit2gtk-4.1-0 (Debian/Ubuntu), webkit2gtk4.1 (Fedora)"}
	libJSC     = &library{soname: "libjavascriptcoregtk-4.1.so.0", packages: "libjavascriptcoregtk-4.1-0 (Debian/Ubuntu), webkit2gtk4.1 (Fedora)"}

	libPixbuf = &library{soname: "libgdk_pixbuf-2.0.so.0", packages: "libgdk-pixbuf-2.0-0 (Debian/Ubuntu), gdk-pixbuf2 (Fedora)", optional: true}
	libNotify = &library{soname: "libnotify.so.4", packages: "libnotify4 (Debian/Ubuntu), libnotify (Fedora)", optional: true}
)

// libraries is the load order. GLib and GObject come first so the
// symbols WebKitGTK resolves lazily are already global in the process.
var libraries = []*library{libGLib, libGObject, libGdk, libGtk, libWebKit, libJSC, libPixbuf, libNotify}

// installLine is the one instruction a user can act on. Everything else
// in the list above arrives as a dependency of these two.
const installLine = "libgtk-3-0 and libwebkit2gtk-4.1-0 on Debian/Ubuntu, gtk3 and webkit2gtk4.1 on Fedora"

type library struct {
	soname   string
	packages string
	optional bool
	handle   uintptr
}

// Symbols holds the C entry points Load resolves. Each is a raw function
// address for internal/ffi.Call; the shell wraps them, this package does
// not, so that the table stays a flat, greppable list of what the host
// actually binds to.
//
// Fields whose library is optional are zero when that library is absent;
// every other field is non-zero once Load returns nil.
type Symbols struct {
	// GLib.
	GFree       uintptr // g_free
	GIdleAdd    uintptr // g_idle_add — the mainThread hop
	GErrorFree  uintptr // g_error_free
	GBytesUnref uintptr // g_bytes_unref

	// GObject.
	GSignalConnectData    uintptr // g_signal_connect_data
	GObjectRefSink        uintptr // g_object_ref_sink
	GObjectUnref          uintptr // g_object_unref
	GTypeCheckInstanceIsA uintptr // g_type_check_instance_is_a

	// GDK.
	GdkDisplayGetDefault    uintptr // gdk_display_get_default
	GdkPixbufGetFromSurface uintptr // gdk_pixbuf_get_from_surface

	// GTK: lifecycle and window.
	GtkInitCheck            uintptr // gtk_init_check
	GtkMain                 uintptr // gtk_main
	GtkMainQuit             uintptr // gtk_main_quit
	GtkWindowNew            uintptr // gtk_window_new
	GtkWindowSetTitle       uintptr // gtk_window_set_title
	GtkWindowSetDefaultSize uintptr // gtk_window_set_default_size
	GtkWindowClose          uintptr // gtk_window_close
	GtkContainerAdd         uintptr // gtk_container_add
	GtkWidgetShowAll        uintptr // gtk_widget_show_all
	GtkWidgetDestroy        uintptr // gtk_widget_destroy
	GtkBoxNew               uintptr // gtk_box_new
	GtkBoxPackStart         uintptr // gtk_box_pack_start

	// GTK: menus.
	GtkMenuBarNew           uintptr // gtk_menu_bar_new
	GtkMenuNew              uintptr // gtk_menu_new
	GtkMenuItemNewWithLabel uintptr // gtk_menu_item_new_with_label
	GtkMenuItemSetSubmenu   uintptr // gtk_menu_item_set_submenu
	GtkMenuShellAppend      uintptr // gtk_menu_shell_append
	GtkSeparatorMenuItemNew uintptr // gtk_separator_menu_item_new

	// GTK: clipboard and dialogs.
	GtkClipboardGetDefault    uintptr // gtk_clipboard_get_default
	GtkClipboardSetText       uintptr // gtk_clipboard_set_text
	GtkClipboardWaitForText   uintptr // gtk_clipboard_wait_for_text
	GtkFileChooserNativeNew   uintptr // gtk_file_chooser_native_new
	GtkNativeDialogRun        uintptr // gtk_native_dialog_run
	GtkNativeDialogDestroy    uintptr // gtk_native_dialog_destroy
	GtkFileChooserGetFilename uintptr // gtk_file_chooser_get_filename
	GtkDialogRun              uintptr // gtk_dialog_run

	// WebKitGTK.
	WebkitWebViewGetType                   uintptr // webkit_web_view_get_type
	WebkitWebViewNew                       uintptr // webkit_web_view_new
	WebkitWebViewLoadURI                   uintptr // webkit_web_view_load_uri
	WebkitWebViewGetSettings               uintptr // webkit_web_view_get_settings
	WebkitWebViewGetUCM                    uintptr // webkit_web_view_get_user_content_manager
	WebkitWebViewEvalJS                    uintptr // webkit_web_view_evaluate_javascript
	WebkitWebViewGetSnapshot               uintptr // webkit_web_view_get_snapshot
	WebkitWebViewGetSnapshotFinish         uintptr // webkit_web_view_get_snapshot_finish
	WebkitUCMRegisterScriptMessageHandler  uintptr // webkit_user_content_manager_register_script_message_handler
	WebkitUCMAddScript                     uintptr // webkit_user_content_manager_add_script
	WebkitUserScriptNew                    uintptr // webkit_user_script_new
	WebkitSettingsSetEnableDeveloperExtras uintptr // webkit_settings_set_enable_developer_extras
	WebkitJavascriptResultGetJSValue       uintptr // webkit_javascript_result_get_js_value

	// JavaScriptCore (the JSCValue side of an evaluate result).
	JscValueIsString uintptr // jsc_value_is_string
	JscValueToString uintptr // jsc_value_to_string

	// Optional: gdk-pixbuf turns a snapshot's cairo surface into PNG
	// bytes; libnotify carries notifications.
	GdkPixbufSaveToBufferv uintptr // gdk_pixbuf_save_to_bufferv
	NotifyInit             uintptr // notify_init
	NotifyNotificationNew  uintptr // notify_notification_new
	NotifyNotificationShow uintptr // notify_notification_show
}

// Sym is the resolved table. Reading a field before a successful Load
// yields zero.
var Sym Symbols

type symbol struct {
	target *uintptr
	lib    *library
	name   string
}

// table is every entry point the Linux host binds, in one flat list so
// that "what does this depend on" is a single read. The smoke test
// asserts that every non-optional entry resolves on a machine with the
// libraries installed, which is what keeps the list honest as WebKitGTK
// moves.
func table() []symbol {
	return []symbol{
		{&Sym.GFree, libGLib, "g_free"},
		{&Sym.GIdleAdd, libGLib, "g_idle_add"},
		{&Sym.GErrorFree, libGLib, "g_error_free"},
		{&Sym.GBytesUnref, libGLib, "g_bytes_unref"},

		{&Sym.GSignalConnectData, libGObject, "g_signal_connect_data"},
		{&Sym.GObjectRefSink, libGObject, "g_object_ref_sink"},
		{&Sym.GObjectUnref, libGObject, "g_object_unref"},
		{&Sym.GTypeCheckInstanceIsA, libGObject, "g_type_check_instance_is_a"},

		{&Sym.GdkDisplayGetDefault, libGdk, "gdk_display_get_default"},
		{&Sym.GdkPixbufGetFromSurface, libGdk, "gdk_pixbuf_get_from_surface"},

		{&Sym.GtkInitCheck, libGtk, "gtk_init_check"},
		{&Sym.GtkMain, libGtk, "gtk_main"},
		{&Sym.GtkMainQuit, libGtk, "gtk_main_quit"},
		{&Sym.GtkWindowNew, libGtk, "gtk_window_new"},
		{&Sym.GtkWindowSetTitle, libGtk, "gtk_window_set_title"},
		{&Sym.GtkWindowSetDefaultSize, libGtk, "gtk_window_set_default_size"},
		{&Sym.GtkWindowClose, libGtk, "gtk_window_close"},
		{&Sym.GtkContainerAdd, libGtk, "gtk_container_add"},
		{&Sym.GtkWidgetShowAll, libGtk, "gtk_widget_show_all"},
		{&Sym.GtkWidgetDestroy, libGtk, "gtk_widget_destroy"},
		{&Sym.GtkBoxNew, libGtk, "gtk_box_new"},
		{&Sym.GtkBoxPackStart, libGtk, "gtk_box_pack_start"},

		{&Sym.GtkMenuBarNew, libGtk, "gtk_menu_bar_new"},
		{&Sym.GtkMenuNew, libGtk, "gtk_menu_new"},
		{&Sym.GtkMenuItemNewWithLabel, libGtk, "gtk_menu_item_new_with_label"},
		{&Sym.GtkMenuItemSetSubmenu, libGtk, "gtk_menu_item_set_submenu"},
		{&Sym.GtkMenuShellAppend, libGtk, "gtk_menu_shell_append"},
		{&Sym.GtkSeparatorMenuItemNew, libGtk, "gtk_separator_menu_item_new"},

		{&Sym.GtkClipboardGetDefault, libGtk, "gtk_clipboard_get_default"},
		{&Sym.GtkClipboardSetText, libGtk, "gtk_clipboard_set_text"},
		{&Sym.GtkClipboardWaitForText, libGtk, "gtk_clipboard_wait_for_text"},
		{&Sym.GtkFileChooserNativeNew, libGtk, "gtk_file_chooser_native_new"},
		{&Sym.GtkNativeDialogRun, libGtk, "gtk_native_dialog_run"},
		{&Sym.GtkNativeDialogDestroy, libGtk, "gtk_native_dialog_destroy"},
		{&Sym.GtkFileChooserGetFilename, libGtk, "gtk_file_chooser_get_filename"},
		{&Sym.GtkDialogRun, libGtk, "gtk_dialog_run"},

		{&Sym.WebkitWebViewGetType, libWebKit, "webkit_web_view_get_type"},
		{&Sym.WebkitWebViewNew, libWebKit, "webkit_web_view_new"},
		{&Sym.WebkitWebViewLoadURI, libWebKit, "webkit_web_view_load_uri"},
		{&Sym.WebkitWebViewGetSettings, libWebKit, "webkit_web_view_get_settings"},
		{&Sym.WebkitWebViewGetUCM, libWebKit, "webkit_web_view_get_user_content_manager"},
		{&Sym.WebkitWebViewEvalJS, libWebKit, "webkit_web_view_evaluate_javascript"},
		{&Sym.WebkitWebViewGetSnapshot, libWebKit, "webkit_web_view_get_snapshot"},
		{&Sym.WebkitWebViewGetSnapshotFinish, libWebKit, "webkit_web_view_get_snapshot_finish"},
		{&Sym.WebkitUCMRegisterScriptMessageHandler, libWebKit, "webkit_user_content_manager_register_script_message_handler"},
		{&Sym.WebkitUCMAddScript, libWebKit, "webkit_user_content_manager_add_script"},
		{&Sym.WebkitUserScriptNew, libWebKit, "webkit_user_script_new"},
		{&Sym.WebkitSettingsSetEnableDeveloperExtras, libWebKit, "webkit_settings_set_enable_developer_extras"},
		{&Sym.WebkitJavascriptResultGetJSValue, libWebKit, "webkit_javascript_result_get_js_value"},

		{&Sym.JscValueIsString, libJSC, "jsc_value_is_string"},
		{&Sym.JscValueToString, libJSC, "jsc_value_to_string"},

		{&Sym.GdkPixbufSaveToBufferv, libPixbuf, "gdk_pixbuf_save_to_bufferv"},
		{&Sym.NotifyInit, libNotify, "notify_init"},
		{&Sym.NotifyNotificationNew, libNotify, "notify_notification_new"},
		{&Sym.NotifyNotificationShow, libNotify, "notify_notification_show"},
	}
}

var (
	loadOnce sync.Once
	loadErr  error
)

// Load dlopens the GTK and WebKitGTK stack and resolves Sym. It runs at
// most once; every later call returns the first result.
//
// It must be called from the thread that will own the GTK main loop:
// dlopen runs a library's constructors on the calling thread, and GTK
// wants those on the same thread as gtk_init and gtk_main. Nothing here
// calls gtk_init — that is the shell's job in phase 6 — so Load is safe
// to call from a test.
//
// A missing required library produces an error wrapping ErrUnavailable
// whose text names the distro packages to install, which is the message
// docs/desktop-plan.md promises the user ("Run returns a clear error
// naming the package to install").
func Load() error {
	loadOnce.Do(load)
	return loadErr
}

func load() {
	for _, lib := range libraries {
		h, err := DlopenGlobal(lib.soname)
		if err != nil {
			if lib.optional {
				continue
			}
			// Name the headline packages, not just the transitive
			// dependency that happened to fail first: a user told to
			// install libglib2.0-0 because that is what dlopen tripped
			// on still has no WebKitGTK afterwards.
			loadErr = fmt.Errorf("desktop: the Linux window needs GTK 3 and WebKitGTK 4.1, and %s could not be loaded; install %s (%s): %w",
				lib.soname, installLine, lib.packages, err)
			return
		}
		lib.handle = h
	}

	var missing []string
	for _, s := range table() {
		if s.lib.handle == 0 {
			continue // an optional library that is not installed
		}
		addr, err := Dlsym(s.lib.handle, s.name)
		if err != nil {
			if s.lib.optional {
				continue
			}
			missing = append(missing, s.name+" ("+s.lib.soname+")")
			continue
		}
		*s.target = addr
	}
	if len(missing) > 0 {
		loadErr = fmt.Errorf("desktop: %d entry point(s) missing from the installed GTK/WebKitGTK: %s",
			len(missing), strings.Join(missing, ", "))
	}
}

// Available reports whether the required libraries are present. It is
// Load without the error text, for a caller deciding between the GTK
// shell and a clear "not supported here" message.
func Available() bool {
	return Load() == nil
}

// Missing reports whether err is the "library not installed" case rather
// than a broken installation, so a caller can tell the user to install a
// package instead of filing a bug.
func Missing(err error) bool { return errors.Is(err, ErrUnavailable) }
