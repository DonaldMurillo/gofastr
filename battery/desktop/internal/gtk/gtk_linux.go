//go:build linux && (amd64 || arm64)

package gtk

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

// The shared libraries the Linux host binds to, and the distro packages
// that carry them. GTK 3 and WebKitGTK 4.1 are the targets: 4.1 is what
// Ubuntu 22.04 onward, Debian 12 and Tauri ship against, and
// webkitgtk-6.0 (GTK 4) is a later probe.
//
// Optional libraries are the ones a host can do without: notifications
// degrade to nothing without libnotify, and a snapshot needs gdk-pixbuf
// only to turn the cairo surface into PNG bytes. A missing optional
// library leaves its symbols zero and Load still succeeds.
var (
	libGLib    = &library{soname: "libglib-2.0.so.0", packages: "libglib2.0-0 (Debian/Ubuntu), glib2 (Fedora)"}
	libGObject = &library{soname: "libgobject-2.0.so.0", packages: "libglib2.0-0 (Debian/Ubuntu), glib2 (Fedora)"}
	libGIO     = &library{soname: "libgio-2.0.so.0", packages: "libglib2.0-0 (Debian/Ubuntu), glib2 (Fedora)"}
	libGdk     = &library{soname: "libgdk-3.so.0", packages: "libgtk-3-0 (Debian/Ubuntu), gtk3 (Fedora)"}
	libGtk     = &library{soname: "libgtk-3.so.0", packages: "libgtk-3-0 (Debian/Ubuntu), gtk3 (Fedora)"}
	libWebKit  = &library{soname: "libwebkit2gtk-4.1.so.0", packages: "libwebkit2gtk-4.1-0 (Debian/Ubuntu), webkit2gtk4.1 (Fedora)"}
	libCairo   = &library{soname: "libcairo.so.2", packages: "libcairo2 (Debian/Ubuntu), cairo (Fedora)"}
	libJSC     = &library{soname: "libjavascriptcoregtk-4.1.so.0", packages: "libjavascriptcoregtk-4.1-0 (Debian/Ubuntu), webkit2gtk4.1 (Fedora)"}

	libPixbuf = &library{soname: "libgdk_pixbuf-2.0.so.0", packages: "libgdk-pixbuf-2.0-0 (Debian/Ubuntu), gdk-pixbuf2 (Fedora)", optional: true}
	libNotify = &library{soname: "libnotify.so.4", packages: "libnotify4 (Debian/Ubuntu), libnotify (Fedora)", optional: true}
)

// libraries is the load order. GLib and GObject come first so the
// symbols WebKitGTK resolves lazily are already global in the process.
var libraries = []*library{libGLib, libGObject, libGIO, libGdk, libGtk, libWebKit, libJSC, libCairo, libPixbuf, libNotify}

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
	GtkMainLevel                             uintptr // gtk_main_level
	GtkDialogResponse                        uintptr // gtk_dialog_response
	GtkClipboardRequestText                  uintptr // gtk_clipboard_request_text
	GtkNativeDialogShow                      uintptr // gtk_native_dialog_show
	GtkNativeDialogHide                      uintptr // gtk_native_dialog_hide
	GtkNativeDialogSetModal                  uintptr // gtk_native_dialog_set_modal
	GtkMessageDialogNew                      uintptr // gtk_message_dialog_new
	GtkDialogAddButton                       uintptr // gtk_dialog_add_button
	GtkDialogSetDefaultResponse              uintptr // gtk_dialog_set_default_response
	GtkFileChooserSetSelectMultiple          uintptr // gtk_file_chooser_set_select_multiple
	GtkFileChooserSetCurrentName             uintptr // gtk_file_chooser_set_current_name
	GtkFileChooserSetDoOverwriteConfirmation uintptr // gtk_file_chooser_set_do_overwrite_confirmation
	GtkFileChooserSetLocalOnly               uintptr // gtk_file_chooser_set_local_only
	GtkFileChooserGetFilenames               uintptr // gtk_file_chooser_get_filenames
	GtkFileChooserAddFilter                  uintptr // gtk_file_chooser_add_filter
	GtkFileFilterNew                         uintptr // gtk_file_filter_new
	GtkFileFilterSetName                     uintptr // gtk_file_filter_set_name
	GtkFileFilterAddPattern                  uintptr // gtk_file_filter_add_pattern
	GSListFree                               uintptr // g_slist_free
	GListFreeFull                            uintptr // g_list_free_full
	GMainContextIteration                    uintptr // g_main_context_iteration
	GSourceRemove                            uintptr // g_source_remove
	GtkWindowPresent                         uintptr // gtk_window_present
	GtkWindowGetPosition                     uintptr // gtk_window_get_position
	GtkWindowGetSize                         uintptr // gtk_window_get_size
	GtkWindowMove                            uintptr // gtk_window_move
	GtkWindowResize                          uintptr // gtk_window_resize
	GtkWindowSetPosition                     uintptr // gtk_window_set_position
	GtkWindowSetResizable                    uintptr // gtk_window_set_resizable
	GtkWindowSetDecorated                    uintptr // gtk_window_set_decorated
	GtkWindowSetKeepAbove                    uintptr // gtk_window_set_keep_above
	GtkWindowSetAcceptFocus                  uintptr // gtk_window_set_accept_focus
	GtkWindowStick                           uintptr // gtk_window_stick
	GtkWidgetGetVisible                      uintptr // gtk_widget_get_visible
	GtkWindowIsActive                        uintptr // gtk_window_is_active
	GtkWidgetSetSensitive                    uintptr // gtk_widget_set_sensitive
	GtkAccelGroupNew                         uintptr // gtk_accel_group_new
	GtkWindowAddAccelGroup                   uintptr // gtk_window_add_accel_group
	GtkWidgetAddAccelerator                  uintptr // gtk_widget_add_accelerator
	GdkKeyvalFromName                        uintptr // gdk_keyval_from_name
	WebkitUserScriptUnref                    uintptr // webkit_user_script_unref
	WebkitWebViewNewWithContext              uintptr // webkit_web_view_new_with_context
	WebkitWebContextNewEphemeral             uintptr // webkit_web_context_new_ephemeral
	WebkitWebViewCallAsyncJS                 uintptr // webkit_web_view_call_async_javascript_function
	WebkitWebViewCallAsyncJSFinish           uintptr // webkit_web_view_call_async_javascript_function_finish
	WebkitWebViewEvalJSFinish                uintptr // webkit_web_view_evaluate_javascript_finish
	WebkitWebViewExecuteEditingCommand       uintptr // webkit_web_view_execute_editing_command
	JscValueToJSON                           uintptr // jsc_value_to_json
	CairoSurfaceDestroy                      uintptr // cairo_surface_destroy
	CairoSurfaceWriteToPNGStream             uintptr // cairo_surface_write_to_png_stream

	// Persistent browser profiles (WebKitGTK2.40+).
	WebkitWebsiteDataManagerNew               uintptr // webkit_website_data_manager_new
	WebkitWebContextNewWithDataManager        uintptr // webkit_web_context_new_with_website_data_manager
	WebkitDataManagerGetCookieManager         uintptr // webkit_website_data_manager_get_cookie_manager
	WebkitCookieManagerSetPersistentStorage   uintptr // webkit_cookie_manager_set_persistent_storage
	WebkitDataManagerSetPersistentCredentials uintptr // webkit_website_data_manager_set_persistent_credential_storage_enabled
	WebkitDataManagerClear                    uintptr // webkit_website_data_manager_clear
	WebkitDataManagerClearFinish              uintptr // webkit_website_data_manager_clear_finish
	GCancellableNew                           uintptr // g_cancellable_new
	GCancellableCancel                        uintptr // g_cancellable_cancel

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
	GtkEventsPending        uintptr // gtk_events_pending
	GtkMainIterationDo      uintptr // gtk_main_iteration_do
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

	// WebKitGTK navigation policy (2.40+, the same floor as evaluate_javascript).
	WebkitPolicyDecisionIgnore                          uintptr // webkit_policy_decision_ignore
	WebkitResponsePolicyDecisionGetResponse             uintptr // webkit_response_policy_decision_get_response
	WebkitResponsePolicyDecisionIsMainFrameMainResource uintptr // webkit_response_policy_decision_is_main_frame_main_resource
	WebkitURIResponseGetURI                             uintptr // webkit_uri_response_get_uri

	// JavaScriptCore (the JSCValue side of an evaluate result).
	JscValueIsString uintptr // jsc_value_is_string
	JscValueToString uintptr // jsc_value_to_string

	// Window effects (#605): transparency, materials and page drags.
	GtkWidgetGetScreen              uintptr // gtk_widget_get_screen
	GtkWidgetSetVisual              uintptr // gtk_widget_set_visual
	GtkWidgetGetWindow              uintptr // gtk_widget_get_window
	GtkWidgetGetStyleContext        uintptr // gtk_widget_get_style_context
	GtkStyleContextAddProvider      uintptr // gtk_style_context_add_provider
	GtkCSSProviderNew               uintptr // gtk_css_provider_new
	GtkCSSProviderLoadFromData      uintptr // gtk_css_provider_load_from_data
	GtkWindowBeginMoveDrag          uintptr // gtk_window_begin_move_drag
	GdkScreenGetRGBAVisual          uintptr // gdk_screen_get_rgba_visual
	GdkScreenIsComposited           uintptr // gdk_screen_is_composited
	GdkEventGetEventType            uintptr // gdk_event_get_event_type
	GdkEventGetButton               uintptr // gdk_event_get_button
	GdkEventGetRootCoords           uintptr // gdk_event_get_root_coords
	GdkEventGetTime                 uintptr // gdk_event_get_time
	GdkAtomInternStaticString       uintptr // gdk_atom_intern_static_string
	GdkPropertyChange               uintptr // gdk_property_change
	WebkitWebViewSetBackgroundColor uintptr // webkit_web_view_set_background_color

	// Optional per symbol: the X11 backend's entry points. A GDK built
	// without X11 lacks them, and they must never be called on a Wayland
	// display, so the shell checks the display type first.
	GdkX11DisplayGetType          uintptr // gdk_x11_display_get_type
	GdkX11ScreenSupportsNetWMHint uintptr // gdk_x11_screen_supports_net_wm_hint

	// Optional: gdk-pixbuf turns a snapshot's cairo surface into PNG
	// bytes; libnotify carries notifications.
	GdkPixbufSaveToBufferv uintptr // gdk_pixbuf_save_to_bufferv
	NotifyInit             uintptr // notify_init
	NotifySetAppName       uintptr // notify_set_app_name
	NotifyGetServerCaps    uintptr // notify_get_server_caps
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
		{&Sym.WebkitWebsiteDataManagerNew, libWebKit, "webkit_website_data_manager_new"},
		{&Sym.WebkitWebContextNewWithDataManager, libWebKit, "webkit_web_context_new_with_website_data_manager"},
		{&Sym.WebkitDataManagerGetCookieManager, libWebKit, "webkit_website_data_manager_get_cookie_manager"},
		{&Sym.WebkitCookieManagerSetPersistentStorage, libWebKit, "webkit_cookie_manager_set_persistent_storage"},
		{&Sym.WebkitDataManagerSetPersistentCredentials, libWebKit, "webkit_website_data_manager_set_persistent_credential_storage_enabled"},
		{&Sym.WebkitDataManagerClear, libWebKit, "webkit_website_data_manager_clear"},
		{&Sym.WebkitDataManagerClearFinish, libWebKit, "webkit_website_data_manager_clear_finish"},
		{&Sym.GCancellableNew, libGIO, "g_cancellable_new"},
		{&Sym.GCancellableCancel, libGIO, "g_cancellable_cancel"},

		{&Sym.GtkMainLevel, libGtk, "gtk_main_level"},
		{&Sym.GtkDialogResponse, libGtk, "gtk_dialog_response"},
		{&Sym.GtkClipboardRequestText, libGtk, "gtk_clipboard_request_text"},
		{&Sym.GtkNativeDialogShow, libGtk, "gtk_native_dialog_show"},
		{&Sym.GtkNativeDialogHide, libGtk, "gtk_native_dialog_hide"},
		{&Sym.GtkNativeDialogSetModal, libGtk, "gtk_native_dialog_set_modal"},
		{&Sym.GtkMessageDialogNew, libGtk, "gtk_message_dialog_new"},
		{&Sym.GtkDialogAddButton, libGtk, "gtk_dialog_add_button"},
		{&Sym.GtkDialogSetDefaultResponse, libGtk, "gtk_dialog_set_default_response"},
		{&Sym.GtkFileChooserSetSelectMultiple, libGtk, "gtk_file_chooser_set_select_multiple"},
		{&Sym.GtkFileChooserSetCurrentName, libGtk, "gtk_file_chooser_set_current_name"},
		{&Sym.GtkFileChooserSetDoOverwriteConfirmation, libGtk, "gtk_file_chooser_set_do_overwrite_confirmation"},
		{&Sym.GtkFileChooserSetLocalOnly, libGtk, "gtk_file_chooser_set_local_only"},
		{&Sym.GtkFileChooserGetFilenames, libGtk, "gtk_file_chooser_get_filenames"},
		{&Sym.GtkFileChooserAddFilter, libGtk, "gtk_file_chooser_add_filter"},
		{&Sym.GtkFileFilterNew, libGtk, "gtk_file_filter_new"},
		{&Sym.GtkFileFilterSetName, libGtk, "gtk_file_filter_set_name"},
		{&Sym.GtkFileFilterAddPattern, libGtk, "gtk_file_filter_add_pattern"},
		{&Sym.GSListFree, libGLib, "g_slist_free"},
		{&Sym.GListFreeFull, libGLib, "g_list_free_full"},
		{&Sym.GMainContextIteration, libGLib, "g_main_context_iteration"},
		{&Sym.GSourceRemove, libGLib, "g_source_remove"},
		{&Sym.GtkWindowPresent, libGtk, "gtk_window_present"},
		{&Sym.GtkWindowGetPosition, libGtk, "gtk_window_get_position"},
		{&Sym.GtkWindowGetSize, libGtk, "gtk_window_get_size"},
		{&Sym.GtkWindowMove, libGtk, "gtk_window_move"},
		{&Sym.GtkWindowResize, libGtk, "gtk_window_resize"},
		{&Sym.GtkWindowSetPosition, libGtk, "gtk_window_set_position"},
		{&Sym.GtkWindowSetResizable, libGtk, "gtk_window_set_resizable"},
		{&Sym.GtkWindowSetDecorated, libGtk, "gtk_window_set_decorated"},
		{&Sym.GtkWindowSetKeepAbove, libGtk, "gtk_window_set_keep_above"},
		{&Sym.GtkWindowSetAcceptFocus, libGtk, "gtk_window_set_accept_focus"},
		{&Sym.GtkWindowStick, libGtk, "gtk_window_stick"},
		{&Sym.GtkWidgetGetVisible, libGtk, "gtk_widget_get_visible"},
		{&Sym.GtkWindowIsActive, libGtk, "gtk_window_is_active"},
		{&Sym.GtkWidgetSetSensitive, libGtk, "gtk_widget_set_sensitive"},
		{&Sym.GtkAccelGroupNew, libGtk, "gtk_accel_group_new"},
		{&Sym.GtkWindowAddAccelGroup, libGtk, "gtk_window_add_accel_group"},
		{&Sym.GtkWidgetAddAccelerator, libGtk, "gtk_widget_add_accelerator"},
		{&Sym.GdkKeyvalFromName, libGdk, "gdk_keyval_from_name"},
		{&Sym.WebkitUserScriptUnref, libWebKit, "webkit_user_script_unref"},
		{&Sym.WebkitWebViewNewWithContext, libWebKit, "webkit_web_view_new_with_context"},
		{&Sym.WebkitWebContextNewEphemeral, libWebKit, "webkit_web_context_new_ephemeral"},
		{&Sym.WebkitWebViewCallAsyncJS, libWebKit, "webkit_web_view_call_async_javascript_function"},
		{&Sym.WebkitWebViewCallAsyncJSFinish, libWebKit, "webkit_web_view_call_async_javascript_function_finish"},
		{&Sym.WebkitWebViewEvalJSFinish, libWebKit, "webkit_web_view_evaluate_javascript_finish"},
		{&Sym.WebkitWebViewExecuteEditingCommand, libWebKit, "webkit_web_view_execute_editing_command"},
		{&Sym.JscValueToJSON, libJSC, "jsc_value_to_json"},
		{&Sym.CairoSurfaceDestroy, libCairo, "cairo_surface_destroy"},
		{&Sym.CairoSurfaceWriteToPNGStream, libCairo, "cairo_surface_write_to_png_stream"},
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
		{&Sym.GtkEventsPending, libGtk, "gtk_events_pending"},
		{&Sym.GtkMainIterationDo, libGtk, "gtk_main_iteration_do"},
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
		{&Sym.WebkitPolicyDecisionIgnore, libWebKit, "webkit_policy_decision_ignore"},
		{&Sym.WebkitResponsePolicyDecisionGetResponse, libWebKit, "webkit_response_policy_decision_get_response"},
		{&Sym.WebkitResponsePolicyDecisionIsMainFrameMainResource, libWebKit, "webkit_response_policy_decision_is_main_frame_main_resource"},
		{&Sym.WebkitURIResponseGetURI, libWebKit, "webkit_uri_response_get_uri"},

		{&Sym.JscValueIsString, libJSC, "jsc_value_is_string"},
		{&Sym.JscValueToString, libJSC, "jsc_value_to_string"},

		{&Sym.GtkWidgetGetScreen, libGtk, "gtk_widget_get_screen"},
		{&Sym.GtkWidgetSetVisual, libGtk, "gtk_widget_set_visual"},
		{&Sym.GtkWidgetGetWindow, libGtk, "gtk_widget_get_window"},
		{&Sym.GtkWidgetGetStyleContext, libGtk, "gtk_widget_get_style_context"},
		{&Sym.GtkStyleContextAddProvider, libGtk, "gtk_style_context_add_provider"},
		{&Sym.GtkCSSProviderNew, libGtk, "gtk_css_provider_new"},
		{&Sym.GtkCSSProviderLoadFromData, libGtk, "gtk_css_provider_load_from_data"},
		{&Sym.GtkWindowBeginMoveDrag, libGtk, "gtk_window_begin_move_drag"},
		{&Sym.GdkScreenGetRGBAVisual, libGdk, "gdk_screen_get_rgba_visual"},
		{&Sym.GdkScreenIsComposited, libGdk, "gdk_screen_is_composited"},
		{&Sym.GdkEventGetEventType, libGdk, "gdk_event_get_event_type"},
		{&Sym.GdkEventGetButton, libGdk, "gdk_event_get_button"},
		{&Sym.GdkEventGetRootCoords, libGdk, "gdk_event_get_root_coords"},
		{&Sym.GdkEventGetTime, libGdk, "gdk_event_get_time"},
		{&Sym.GdkAtomInternStaticString, libGdk, "gdk_atom_intern_static_string"},
		{&Sym.GdkPropertyChange, libGdk, "gdk_property_change"},
		{&Sym.WebkitWebViewSetBackgroundColor, libWebKit, "webkit_web_view_set_background_color"},

		{&Sym.GdkPixbufSaveToBufferv, libPixbuf, "gdk_pixbuf_save_to_bufferv"},
		{&Sym.NotifyInit, libNotify, "notify_init"},
		{&Sym.NotifySetAppName, libNotify, "notify_set_app_name"},
		{&Sym.NotifyGetServerCaps, libNotify, "notify_get_server_caps"},
		{&Sym.NotifyNotificationNew, libNotify, "notify_notification_new"},
		{&Sym.NotifyNotificationShow, libNotify, "notify_notification_show"},
	}
}

// optionalSymbols may be missing from a library that is present: the GDK
// X11 backend's entry points, absent from a GDK built without X11. Load
// leaves a missing one zero instead of failing.
func optionalSymbols() []symbol {
	return []symbol{
		{&Sym.GdkX11DisplayGetType, libGdk, "gdk_x11_display_get_type"},
		{&Sym.GdkX11ScreenSupportsNetWMHint, libGdk, "gdk_x11_screen_supports_net_wm_hint"},
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
// whose text names the distro packages to install, so Run tells the user
// what to install instead of failing on a missing symbol.
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
	for _, s := range optionalSymbols() {
		if s.lib.handle == 0 {
			continue
		}
		if addr, err := Dlsym(s.lib.handle, s.name); err == nil {
			*s.target = addr
		}
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
