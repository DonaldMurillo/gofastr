// Package linux provides the GTK 3 / WebKitGTK 4.1 desktop shell on
// Linux amd64 and arm64. It uses Go assembly and runtime-loaded system
// libraries; applications build with CGO_ENABLED=0 and need no C compiler
// or GTK development headers. Run loads the libraries lazily and reports
// missing runtime packages. Other architectures return an unsupported shell.
//
// The shell supports native windows, menus, clipboard, permission and file
// dialogs, shared-session secondary windows, JavaScript evaluation and PNG
// snapshots. Tray icons, compositor materials and OS deep-link registration
// are not implemented. GTK/window-manager restrictions apply to placement
// and always-on-top requests, especially under Wayland.
package linux
