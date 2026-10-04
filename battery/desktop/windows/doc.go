// Package windows implements the Windows/amd64 desktop host with a
// Win32 message loop and WebView2. It also provides native menus,
// file/folder dialogs, clipboard access, tray notifications, and
// secondary windows. WebView2Loader.dll must be beside the executable
// (gofastr desktop build places it there); the Evergreen WebView2
// Runtime must be installed on the machine.
//
// On Windows 11 build 22000 or newer, the host asks DWM for dark-mode
// frame colors and rounded corners. On build 22621 or newer it also
// supports Mica (MaterialWindow), Mica Alt (MaterialSidebar), and
// Desktop Acrylic (MaterialGlass). DWM applies materials to the whole
// window, so MaterialSidebar is a whole-window Mica Alt surface. The
// WebView background is transparent so the page can show the backdrop.
// ChromeHiddenTitle and ChromeUnified use a custom frame while DWM keeps
// the system caption controls. Without a native menu, the page can fill
// the title-bar area. With a native menu, the shell preserves the native
// caption and menu bands above the page so the WebView cannot cover them.
// The shell opts into per-monitor DPI awareness so WebView2 and native
// controls render at display scale.
// WebView2 drag regions and the desktop drag handle can move the window.
// Windows has no system
// Reduce Transparency setting exposed by this host, so Appearance
// answers false and no reduce_transparency event fires. Other targets
// return the unsupported shell.
package windows
