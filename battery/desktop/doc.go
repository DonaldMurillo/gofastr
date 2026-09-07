// Package desktop runs a GoFastr app as a local-first desktop
// application inside the operating system's own WebView, with a typed
// JavaScript bridge to native capabilities (window, dialogs, clipboard,
// notifications, files) that plugins extend in plain Go.
//
// EXPERIMENTAL. The desktop host ships as a proof of concept: the
// capability surface, the Shell/Window contracts, and the generated
// bridge of this package may change or be removed without a major
// version bump. An app that depends on it should pin a version. This
// package is marked Experimental in the stability manifest.
//
// This package is the contract and the OS-neutral half: the Shell,
// Window, and NativeDriver interfaces, the capabilities, the grants
// store, the boot handshake, app state, preferences, deep links, and
// the updater. The native layer lives in one package per platform, each
// compiling on every GOOS so a host cross-compiles without build tags
// of its own:
//
//   - battery/desktop/macos: the real shell (AppKit + WKWebView) on
//     darwin/arm64, the unsupported shell elsewhere.
//   - battery/desktop/windows, battery/desktop/linux: the unsupported
//     shell today (WebView2 and WebKitGTK when their phases land).
//   - battery/desktop/native: Shell() picks the platform package by
//     GOOS; New(cfg) is desktop.New with that shell as the default.
//
// desktop.New with a nil Config.Shell answers the unsupported shell on
// every platform; a host that wants the real shell writes native.New
// (or desktop.Config{Shell: native.Shell()}). Nothing registers itself
// through init.
//
// The battery serves the app's existing http.Handler on loopback,
// authenticating the single native window with a one-shot boot token
// minted at construction; every other local process that finds the
// port is refused. Capabilities are registered before Run freezes the
// registry; the page reaches them through
// window.__gofastr.desktop.<capability>.<method>, served by one
// permission-gated POST chokepoint.
//
// Usage:
//
//	opts, err := desktop.AppOptions("dev.gofastr.notes")
//	if err != nil { log.Fatal(err) }
//	app := framework.NewApp(append(opts,
//	    framework.WithConfig(framework.AppConfig{Name: "notes"}))...)
//	app.Mount(uihost.New(site))
//	d := native.New(desktop.Config{ID: "dev.gofastr.notes", Title: "Notes"})
//	app.RegisterBattery(d)
//	if err := d.Run(app); err != nil { log.Fatal(err) }
//
// See framework/docs/content/desktop.md for the full guide.
package desktop
